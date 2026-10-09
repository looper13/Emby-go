package server

import (
	"database/sql"
	"emby-go/internal/librarywatch"
	"emby-go/internal/store"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *App) cacheScope(libraryID int64) string {
	version := a.db.Version("g:version")
	if libraryID > 0 {
		version = a.db.Version("lib:" + strconv.FormatInt(libraryID, 10) + ":version")
	}
	var epoch uint64
	if libraryID == 0 {
		epoch = a.allScopeVersion.Load()
	} else if v, ok := a.scopeVersions.Load(libraryID); ok {
		epoch = v.(*atomic.Uint64).Load()
	}
	return strconv.FormatInt(libraryID, 10) + ":" + version + ":" + a.db.ActorVersion() + ":" + strconv.FormatUint(epoch, 10)
}
func responseKey(kind string, values ...any) string {
	data, _ := json.Marshal(values)
	return kind + ":" + string(data)
}
func (a *App) cachedResponse(c *gin.Context, key string, ttl time.Duration, load func() ([]byte, error)) {
	body, err := a.cache.Load(c.Request.Context(), key, ttl, load)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.Data(http.StatusOK, "application/json", body)
}
func withinCacheDirectory(directory, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// Invalidate only matching disk dependencies; thumbnail keys contain both the
// path and tag, so other movies' decoded thumbnails remain reusable.
func (a *App) invalidateDiskPaths(paths []string, recursive bool) {
	files := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path != "" {
			files[cachePathKey(path)] = true
		}
	}
	matches := func(path string) bool {
		if files[cachePathKey(path)] {
			return true
		}
		if recursive {
			for _, changed := range paths {
				if changed != "" && withinCacheDirectory(changed, path) {
					return true
				}
			}
		}
		return false
	}
	a.tagMu.Lock()
	if a.diskVersions == nil {
		a.diskVersions = make(map[string]diskVersionRecord)
	}
	// 同一批变化共享一个版本；目录记录作用于所有后代，包括正在加载、
	// 尚未进入缓存的路径，不依赖读请求事先登记。
	a.diskVersionSeq++
	for _, path := range paths {
		if path != "" {
			key := cachePathKey(path)
			record := a.diskVersions[key]
			record.version, record.ts = a.diskVersionSeq, time.Now()
			if recursive {
				record.subtreeVersion = a.diskVersionSeq
			}
			a.diskVersions[key] = record
		}
	}
	a.pruneDiskVersionsLocked(time.Now())
	for path := range a.tags {
		if matches(path) {
			delete(a.tags, path)
		}
	}
	a.tagMu.Unlock()
	a.nfoMu.Lock()
	a.nfos.deleteMatching(matches)
	a.nfoMu.Unlock()
	a.imgThumb.DeleteMatching(func(key string) bool {
		if !strings.HasPrefix(key, "t:") {
			return false
		}
		// Current thumbnail keys end with version, tag, size and quality fields.
		// Decode the path from the right, preserving colons in Windows drive names.
		prefix := key
		var version string
		valid := true
		for index := 0; index < 4; index++ {
			cut := strings.LastIndex(prefix, ":")
			if cut < 0 {
				valid = false
				break
			}
			version = prefix[cut+1:]
			prefix = prefix[:cut]
		}
		if valid && strings.HasPrefix(version, "v") {
			if _, err := strconv.ParseUint(strings.TrimPrefix(version, "v"), 10, 64); err == nil {
				return matches(strings.TrimPrefix(prefix, "t:"))
			}
		}
		return false
	})
}
func (a *App) invalidateMovieDisk(movieID int64) {
	movie, err := a.db.Movie(movieID)
	if err != nil {
		return
	}
	a.invalidateDiskPaths(movieDiskPaths(movie), false)
}

func movieDiskPaths(movie store.Movie) []string {
	return append([]string{movie.SourcePath, movie.NFOPath, movie.PosterPath, movie.LandscapePath}, movie.Backdrops()...)
}

// diskVersionRecord 保存路径自身及整棵子树最近的失效版本。
// 有效版本取基线、自身版本和祖先子树版本的最大值。
type diskVersionRecord struct {
	version        uint64
	subtreeVersion uint64
	ts             time.Time
}

const (
	// diskVersionMaxPaths 路径失效记录的软上限。超过后才做清理：正常情况下
	// 记录数只跟「启动后被改动过的不同路径数」有关，不会随读请求增长。
	diskVersionMaxPaths = 50000
	// diskVersionRetention 失效记录的保留时长；回收时同时抬高全局基线，
	// 客户端与正在加载的请求所持的历史版本也会失配。
	diskVersionRetention = 24 * time.Hour
)

// bumpDiskVersionLocked 记一次路径失效。调用方需持有 tagMu。
func (a *App) bumpDiskVersionLocked(path string) {
	a.diskVersionSeq++
	record := a.diskVersions[path]
	record.version = a.diskVersionSeq
	record.ts = time.Now()
	a.diskVersions[path] = record
}

// pruneDiskVersionsLocked 在失效记录过多时清理：先淘汰超过保留期的记录，
// 仍然超限（短时间内改了太多路径）才整表重置。任何记录回收都抬高基线。
//
// 分配序号不随记录删除回退，基线严格大于曾发出的版本，避免复用客户端旧 ETag。
func (a *App) pruneDiskVersionsLocked(now time.Time) {
	if len(a.diskVersions) <= diskVersionMaxPaths {
		return
	}
	cutoff := now.Add(-diskVersionRetention)
	removed := 0
	for path, record := range a.diskVersions {
		if record.ts.Before(cutoff) {
			delete(a.diskVersions, path)
			removed++
		}
	}
	if removed > 0 {
		a.diskVersionSeq++
		a.diskVersionBase = a.diskVersionSeq
	}
	if len(a.diskVersions) <= diskVersionMaxPaths {
		slog.Info("清理过期的磁盘失效记录", "removed", removed, "remaining", len(a.diskVersions))
		return
	}
	// 仍然超限：整表重置，基线抬到所有已发出的版本之上。
	total := len(a.diskVersions)
	clear(a.diskVersions)
	a.diskVersionSeq++
	a.diskVersionBase = a.diskVersionSeq
	slog.Info("重置磁盘失效记录", "dropped", total, "baseline", a.diskVersionBase)
}

func (a *App) diskVersion(path string) uint64 {
	path = cachePathKey(path)
	a.tagMu.Lock()
	defer a.tagMu.Unlock()
	return a.diskVersionLocked(path)
}

// diskVersionLocked 调用方需持有 tagMu。逐层查祖先，保持目录边界与局部失效范围。
func (a *App) diskVersionLocked(path string) uint64 {
	version := a.diskVersionBase
	if record := a.diskVersions[path]; record.version > version {
		version = record.version
	}
	for parent := filepath.Dir(path); parent != path; parent = filepath.Dir(path) {
		if record := a.diskVersions[parent]; record.subtreeVersion > version {
			version = record.subtreeVersion
		}
		path = parent
	}
	return version
}

func (a *App) finishLibraryCacheRefresh(libraryID int64) {
	version, _ := a.scopeVersions.LoadOrStore(libraryID, &atomic.Uint64{})
	version.(*atomic.Uint64).Add(1)
	a.allScopeVersion.Add(1)
}

func cachePathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
func (a *App) invalidateLibraryChanges(changes []librarywatch.Change) {
	files, directories := []string{}, []string{}
	for _, change := range changes {
		if change.Directory {
			directories = append(directories, change.Path)
		} else {
			files = append(files, change.Path)
		}
	}
	if len(files) > 0 {
		a.invalidateDiskPaths(files, false)
	}
	if len(directories) > 0 {
		a.invalidateDiskPaths(directories, true)
	}
}

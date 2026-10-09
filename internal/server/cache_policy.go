package server

import (
	"database/sql"
	"emby-go/internal/librarywatch"
	"emby-go/internal/store"
	"encoding/json"
	"errors"
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
		a.diskVersions = make(map[string]uint64)
	}
	for path := range a.diskVersions {
		if matches(path) {
			a.diskVersions[path]++
		}
	}
	for _, path := range paths {
		if path != "" && !recursive {
			key := cachePathKey(path)
			if _, exists := a.diskVersions[key]; !exists {
				a.diskVersions[key] = 1
			}
		}
	}
	for path := range a.tags {
		if matches(path) {
			delete(a.tags, path)
		}
	}
	a.tagMu.Unlock()
	a.nfoMu.Lock()
	for key := range a.nfos {
		index := strings.LastIndex(key, "|")
		if index < 0 {
			continue
		}
		path := key[:index]
		if matches(path) {
			delete(a.nfos, key)
		}
	}
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

func (a *App) diskVersion(path string) uint64 {
	path = cachePathKey(path)
	a.tagMu.Lock()
	defer a.tagMu.Unlock()
	if a.diskVersions == nil {
		a.diskVersions = make(map[string]uint64)
	}
	version := a.diskVersions[path]
	a.diskVersions[path] = version
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

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"emby-go/internal/alist"
	"emby-go/internal/imageutil"
	"emby-go/internal/mediaassets"
	"emby-go/internal/store"
	"emby-go/internal/subtitle"
	"github.com/gin-gonic/gin"
)

var cdPartPattern = regexp.MustCompile(`(?i)^(.*?)[ ._-]+(?:CD|part|disc|disk)([1-9][0-9]*)$`)

var knownAssetSuffixes = map[string]bool{
	"poster":    true,
	"cover":     true,
	"default":   true,
	"movie":     true,
	"fanart":    true,
	"thumb":     true,
	"landscape": true,
	"banner":    true,
	"clearart":  true,
	"logo":      true,
	"disc":      true,
	"trailer":   true,
}

type movieDeleteOutcome struct {
	RemoteDeleted []string
	RemoteFailed  []string
	LocalDeleted  []string
	DirRemoved    bool
}

func isPathInsideLibrary(libraryRoot, targetPath string) bool {
	root, err := filepath.Abs(libraryRoot)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	inside := func(base, item string) bool {
		rel, err := filepath.Rel(base, item)
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if !inside(root, target) {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if errors.Is(err, os.ErrNotExist) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target))
		if parentErr != nil {
			return false
		}
		resolvedTarget = filepath.Join(parent, filepath.Base(target))
	} else if err != nil {
		return false
	}
	return inside(resolvedRoot, resolvedTarget)
}

func isAlistURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return false
	}
	p := u.EscapedPath()
	for _, route := range []string{"/d/", "/p/", "/dav/"} {
		if strings.HasPrefix(p, route) || strings.Contains(p, route) {
			return true
		}
	}
	return false
}

func isMatchingAsset(entryName string, stems map[string]struct{}) bool {
	ext := filepath.Ext(entryName)
	stem := strings.TrimSuffix(entryName, ext)
	lowerStem := strings.ToLower(stem)
	lowerName := strings.ToLower(entryName)

	for s := range stems {
		if lowerStem == s {
			return true
		}
		if strings.HasPrefix(lowerName, s+".") {
			return true
		}
		if strings.HasPrefix(lowerStem, s+"-") {
			suffix := lowerStem[len(s)+1:]
			if knownAssetSuffixes[suffix] {
				return true
			}
			if strings.HasPrefix(suffix, "fanart") {
				num := strings.TrimPrefix(suffix, "fanart")
				if num != "" && strings.Trim(num, "0123456789") == "" {
					return true
				}
			}
			if match := cdPartPattern.FindStringSubmatch(stem); match != nil {
				return true
			}
		}
	}
	return false
}

func (a *App) collectMovieLocalFiles(movie store.Movie, lib store.Library) ([]string, string, bool) {
	movieDir := strings.TrimSpace(movie.OutputDir)
	if movieDir == "" {
		movieDir = filepath.Dir(movie.SourcePath)
	}
	movieDir = filepath.Clean(movieDir)
	libRoot := filepath.Clean(lib.Path)

	filesMap := make(map[string]struct{})
	addFile := func(path string) {
		p := strings.TrimSpace(path)
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if isPathInsideLibrary(libRoot, clean) {
			filesMap[clean] = struct{}{}
		}
	}

	sources := append([]string{movie.SourcePath}, movie.AdditionalParts...)
	sourceCleanSet := make(map[string]bool, len(sources))
	for _, s := range sources {
		addFile(s)
		sourceCleanSet[filepath.Clean(s)] = true
	}

	stems := make(map[string]struct{})
	for _, s := range sources {
		base := filepath.Base(s)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" {
			stems[strings.ToLower(stem)] = struct{}{}
			if match := cdPartPattern.FindStringSubmatch(stem); match != nil && match[1] != "" {
				stems[strings.ToLower(match[1])] = struct{}{}
			}
		}
	}

	// 1. Explicit metadata & image paths
	addFile(movie.NFOPath)
	addFile(movie.PosterPath)
	addFile(movie.LandscapePath)
	addFile(movie.BackdropPath)
	for _, b := range movie.BackdropPaths {
		addFile(b)
	}

	// 2. Dynamically resolved artwork
	artwork := a.resolveMovieArtwork(movie, true)
	addFile(artwork.PosterPath)
	addFile(artwork.LandscapePath)
	addFile(artwork.BackdropPath)
	for _, b := range artwork.BackdropPaths {
		addFile(b)
	}

	// 3. Trailers
	for _, s := range sources {
		addFile(mediaassets.TrailerPath(s, movieDir))
	}
	for stem := range stems {
		addFile(filepath.Join(movieDir, stem+"-trailer.mp4"))
	}

	// 4. Subtitles and audio sidecars
	for _, s := range sources {
		for _, f := range subtitle.Discover(s) {
			addFile(f.Path)
		}
		for _, f := range subtitle.DiscoverAudio(s) {
			addFile(f.Path)
		}
	}

	// 5. Check if movieDir is a dedicated directory
	isDedicatedDir := false
	entries, err := os.ReadDir(movieDir)
	if err == nil {
		hasOtherMovies := false
		if movie.Television != nil {
			hasOtherMovies = true
		} else {
			for _, entry := range entries {
				if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
					full := filepath.Join(movieDir, entry.Name())
					if !sourceCleanSet[full] {
						hasOtherMovies = true
						break
					}
				}
			}
		}

		isDedicatedDir = !hasOtherMovies && isPathInsideLibrary(libRoot, movieDir)
		if isDedicatedDir {
			for _, entry := range entries {
				full := filepath.Join(movieDir, entry.Name())
				if !entry.IsDir() {
					addFile(full)
				} else if strings.EqualFold(entry.Name(), "extrafanart") {
					if extraEntries, err := os.ReadDir(full); err == nil {
						for _, ee := range extraEntries {
							if !ee.IsDir() {
								addFile(filepath.Join(full, ee.Name()))
							}
						}
					}
				}
			}
		} else {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				ext := filepath.Ext(name)
				full := filepath.Join(movieDir, name)

				if strings.EqualFold(ext, ".strm") && !sourceCleanSet[full] {
					continue
				}

				if isMatchingAsset(name, stems) {
					addFile(full)
				} else if imageSource, ok := imageutil.MovieImageSource(full); ok {
					if _, ok := stems[strings.ToLower(filepath.Base(imageSource))]; ok {
						addFile(full)
					}
				}
			}
		}
	}

	files := make([]string, 0, len(filesMap))
	for f := range filesMap {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, movieDir, isDedicatedDir
}

func (a *App) deleteMovieWithFiles(ctx context.Context, movie store.Movie, lib store.Library) (movieDeleteOutcome, error) {
	sources := append([]string{movie.SourcePath}, movie.AdditionalParts...)
	cfg := a.configSnapshot()
	var alistClient *alist.Client
	if cfg.AListURL != "" && cfg.AListToken != "" {
		var clientErr error
		alistClient, clientErr = alist.New(cfg.AListURL, cfg.AListToken)
		if clientErr != nil {
			slog.Warn("初始化 AList 客户端失败，跳过网盘文件删除", "error", clientErr)
		}
	}

	remotePaths := make([]string, 0)
	seenRemote := make(map[string]bool)

	for _, source := range sources {
		if !strings.EqualFold(filepath.Ext(source), ".strm") {
			continue
		}
		data, err := readDeletionSTRM(source)
		if err != nil {
			continue
		}
		link := strmLink(data)
		if link == "" {
			continue
		}

		if alistClient != nil {
			remotePath, err := alistClient.Resolve(link)
			if err == nil && remotePath != "" {
				if !seenRemote[remotePath] {
					seenRemote[remotePath] = true
					remotePaths = append(remotePaths, remotePath)
				}
			} else if isAlistURL(link) {
				slog.Warn("STRM 中的 AList 链接与当前系统配置的 AList 服务地址不一致，跳过网盘文件删除", "link", link, "source", source)
			}
		} else if isAlistURL(link) {
			slog.Warn("该影片 STRM 指向 AList 地址，但系统未配置 AList 服务地址或 Token，跳过网盘文件删除", "link", link, "source", source)
		}
	}

	outcome := movieDeleteOutcome{}
	if alistClient != nil && len(remotePaths) > 0 {
		for _, remotePath := range remotePaths {
			if _, err := alistClient.Stat(ctx, remotePath); err != nil {
				if errors.Is(err, alist.ErrNotFound) {
					continue
				}
				slog.Warn("检查网盘原始文件失败，跳过该远端文件删除", "path", remotePath, "error", err)
				outcome.RemoteFailed = append(outcome.RemoteFailed, remotePath)
				continue
			}
			if err := alistClient.Remove(ctx, remotePath); err != nil {
				if _, verifyErr := alistClient.Stat(ctx, remotePath); errors.Is(verifyErr, alist.ErrNotFound) {
					outcome.RemoteDeleted = append(outcome.RemoteDeleted, remotePath)
					continue
				}
				slog.Warn("删除网盘原始文件失败，跳过该远端文件删除", "path", remotePath, "error", err)
				outcome.RemoteFailed = append(outcome.RemoteFailed, remotePath)
				continue
			}
			outcome.RemoteDeleted = append(outcome.RemoteDeleted, remotePath)
		}
	}

	filesToDelete, movieDir, isDedicatedDir := a.collectMovieLocalFiles(movie, lib)

	deletedSet := make(map[string]bool)
	if strings.TrimSpace(movie.SourcePath) != "" {
		if err := os.Remove(movie.SourcePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return outcome, fmt.Errorf("删除本地源文件失败 (%s)：%w", movie.SourcePath, err)
		}
		deletedSet[movie.SourcePath] = true
		outcome.LocalDeleted = append(outcome.LocalDeleted, movie.SourcePath)
	}

	for _, f := range filesToDelete {
		if deletedSet[f] {
			continue
		}
		deletedSet[f] = true
		if err := os.Remove(f); err == nil || errors.Is(err, os.ErrNotExist) {
			outcome.LocalDeleted = append(outcome.LocalDeleted, f)
		} else {
			slog.Warn("删除关联文件失败", "path", f, "error", err)
		}
	}

	if isDedicatedDir {
		_ = os.Remove(filepath.Join(movieDir, "extrafanart"))
		if err := os.Remove(movieDir); err == nil {
			outcome.DirRemoved = true
		}
	}

	a.invalidateDiskPaths(outcome.LocalDeleted, false)
	a.invalidateDiskPaths([]string{movieDir}, true)

	if err := a.db.DeleteMovie(movie.ID); err != nil {
		return outcome, fmt.Errorf("清理数据库索引失败：%w", err)
	}
	_ = a.db.BumpVersion(movie.LibraryID)

	return outcome, nil
}

func (a *App) embyDeleteItem(c *gin.Context) {
	if c.Param("uid") != "" && !a.validUser(c) {
		return
	}
	value, ok := c.Get("accountUser")
	if !ok || !value.(store.User).Administrator {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}
	a.adminDelete(c)
}


package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type r18LocalSource struct {
	ID          string `json:"id"`
	ItemID      string `json:"item_id"`
	Name        string `json:"name"`
	LibraryName string `json:"library_name"`
	Container   string `json:"container"`
}

type r18LocalMatch struct {
	Source r18LocalSource
	Movie  store.Movie
	Path   string
	URL    string
}

type r18LocalSnapshot struct {
	Sources map[string][]r18LocalMatch
}

// 只检查已授权目录库中的真实 STRM，不接受 API 或浏览器提供的文件路径。
func readR18LocalSTRM(root, source string) (string, bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	source, err = filepath.Abs(source)
	if err != nil || !strings.EqualFold(filepath.Ext(source), ".strm") {
		return "", false
	}
	inside := func(base, target string) bool {
		rel, err := filepath.Rel(base, target)
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if !inside(root, source) {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || !inside(resolvedRoot, resolved) {
		return "", false
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", false
	}
	line, err := bufio.NewReader(io.LimitReader(file, (64<<10)+1)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false
	}
	raw := strings.TrimSpace(line)
	u, err := url.Parse(raw)
	if !scanner.ValidHTTP(raw) || err != nil || u.User != nil || u.Hostname() == "" || strings.ContainsAny(raw, "\r\n\\\x00") {
		return "", false
	}
	return raw, true
}

func (a *App) prepareR18LocalSources(c *gin.Context, videos []r18.Video) error {
	numbers := make([]string, 0, len(videos))
	local := r18LocalSnapshot{Sources: map[string][]r18LocalMatch{}}
	for _, v := range videos {
		key := store.NormalizeDuplicateNumber(v.Number)
		if key != "" {
			if _, exists := local.Sources[key]; !exists {
				local.Sources[key] = []r18LocalMatch{}
				numbers = append(numbers, v.Number)
			}
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	movies, err := a.db.LocalSTRMMovies(ctx, numbers)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("本地 STRM 索引查询失败，请重试")
	}
	libraries := map[int64]store.Library{}
	for _, movie := range movies {
		if err := ctx.Err(); err != nil {
			return err
		}
		library, known := libraries[movie.LibraryID]
		if !known {
			library, err = a.db.Library(movie.LibraryID)
			if err != nil || !library.IsDirectory() || !a.db.CanAccessLibrary(library.ID) {
				continue
			}
			libraries[library.ID] = library
		}
		key := store.NormalizeDuplicateNumber(movie.Number)
		paths := append([]string{movie.SourcePath}, movie.AdditionalParts...)
		seen := map[string]bool{}
		for index, file := range paths {
			if err := ctx.Err(); err != nil {
				return err
			}
			if seen[file] {
				continue
			}
			seen[file] = true
			raw, valid := readR18LocalSTRM(library.Path, file)
			if !valid {
				continue
			}
			itemID := strconv.FormatInt(movie.ID, 10)
			sourceID := "strm-" + itemID
			if index > 0 {
				itemID = "part-" + itemID + "-" + strconv.Itoa(index+1)
				sourceID += "-p" + strconv.Itoa(index+1)
			}
			u, _ := url.Parse(raw)
			local.Sources[key] = append(local.Sources[key], r18LocalMatch{Source: r18LocalSource{
				ID: sourceID, ItemID: itemID, Name: filepath.Base(file), LibraryName: library.Name,
				Container: strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), "."),
			}, Movie: movie, Path: file, URL: raw})
		}
	}
	// 快照只属于当前授权请求；列表批量查询，详情与播放复查重新读取。
	c.Set("r18LocalSources", local)
	return nil
}

func (a *App) r18LocalMatches(c *gin.Context, v r18.Video) ([]r18LocalMatch, error) {
	key := store.NormalizeDuplicateNumber(v.Number)
	if snapshot, exists := c.Get("r18LocalSources"); exists {
		if sources, found := snapshot.(r18LocalSnapshot).Sources[key]; found {
			return sources, nil
		}
	}
	if err := a.prepareR18LocalSources(c, []r18.Video{v}); err != nil {
		return nil, err
	}
	snapshot, _ := c.Get("r18LocalSources")
	return snapshot.(r18LocalSnapshot).Sources[key], nil
}

func localR18Presentation(matches []r18LocalMatch) []r18LocalSource {
	sources := make([]r18LocalSource, 0, len(matches))
	for _, match := range matches {
		sources = append(sources, match.Source)
	}
	return sources
}

func (a *App) r18LocalMediaSources(c *gin.Context, matches []r18LocalMatch) []gin.H {
	sources := make([]gin.H, 0, len(matches))
	for index, match := range matches {
		source := a.mediaSourceWithURL(match.Movie, match.Source.ItemID, match.Path, match.URL, c)
		source["Name"] = match.Source.LibraryName + " · " + match.Source.Name
		source["Path"] = source["DirectStreamUrl"]
		source["SourceType"], source["LocalSourceId"] = "strm", match.Source.ID
		source["Default"] = index == 0
		sources = append(sources, source)
	}
	return sources
}

func (a *App) r18LocalPlayback(c *gin.Context) {
	library, ok := a.r18Library(c)
	if !ok {
		return
	}
	sourceID := c.Query("source_id")
	if _, _, err := r18.DecodeID(c.Param("video")); err != nil || !validR18LocalSourceID(sourceID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的本地播放源"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	// 复用短期影片身份缓存，通常无需再次等待 JavDB；本地文件和权限仍实时核验。
	video, err := a.r18ImageVideo(ctx, library, c.Param("video"))
	if err != nil {
		a.r18Failure(c, "详情查询", err)
		return
	}
	matches, err := a.r18LocalMatches(c, video)
	if err != nil {
		a.r18Failure(c, "本地播放源查询", err)
		return
	}
	for _, match := range matches {
		if match.Source.ID == sourceID {
			c.JSON(http.StatusOK, gin.H{"source": match.Source})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "所选本地 STRM 已失效或无权访问，请刷新后自行选择其它播放方式"})
}

func validR18LocalSourceID(raw string) bool {
	if len(raw) > 128 || !strings.HasPrefix(raw, "strm-") {
		return false
	}
	id, part, split := strings.Cut(strings.TrimPrefix(raw, "strm-"), "-p")
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != id {
		return false
	}
	if split {
		index, err := strconv.Atoi(part)
		return err == nil && index >= 2 && strconv.Itoa(index) == part
	}
	return true
}

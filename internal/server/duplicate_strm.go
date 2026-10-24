package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type duplicateSTRMFile struct {
	LocalPath string `json:"local_path"`
	Content   string `json:"content"`
	Error     string `json:"error"`
}

type duplicateSTRMContents struct {
	MovieID int64               `json:"movie_id"`
	Files   []duplicateSTRMFile `json:"files"`
}

func readDuplicateSTRM(root, source string) duplicateSTRMFile {
	file := duplicateSTRMFile{LocalPath: source}
	if !strings.EqualFold(filepath.Ext(source), ".strm") {
		file.Error = "不是 STRM 文件，无法读取内容"
		return file
	}
	// Reuse the local file boundaries without invoking AList or the STRM URL.
	local, err := deletionLocalPath(root, source, false)
	if err != nil {
		file.Error = "STRM 不存在、无法访问或不在媒体库目录内"
		return file
	}
	info, err := os.Stat(local)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		file.Error = "无法读取 STRM：仅支持不超过 64 KiB 的普通文件"
		return file
	}
	data, err := readDeletionSTRM(local)
	if errors.Is(err, os.ErrNotExist) {
		file.Error = "STRM 文件不存在"
	} else if err != nil || len(data) > 64<<10 {
		file.Error = "无法读取 STRM：仅支持不超过 64 KiB 的普通文件"
	} else {
		file.Content = strings.TrimPrefix(string(data), "\ufeff")
	}
	return file
}

func (a *App) adminDuplicateSTRM(c *gin.Context) {
	// Contents can contain signed URLs; do not retain them in HTTP caches.
	c.Header("Cache-Control", "no-store")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	libraryID, libErr := strconv.ParseInt(c.DefaultQuery("library_id", "0"), 10, 64)
	key := c.Query("key")
	if err != nil || id <= 0 || libErr != nil || libraryID < 0 || strings.TrimSpace(key) == "" || len(key) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的影片、媒体库或番号分组参数"})
		return
	}
	movie, err := a.db.Movie(id)
	if err != nil || !a.db.CanAccessLibrary(movie.LibraryID) || libraryID != 0 && libraryID != movie.LibraryID {
		c.JSON(http.StatusNotFound, gin.H{"error": "影片不存在或不可访问"})
		return
	}
	if movie.Television != nil || store.NormalizeDuplicateNumber(movie.Number) != key {
		c.JSON(http.StatusConflict, gin.H{"error": "影片已不属于该番号分组，请重新查重"})
		return
	}
	lib, err := a.db.Library(movie.LibraryID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体库不存在或不可访问"})
		return
	}
	sources := append([]string{movie.SourcePath}, movie.AdditionalParts...)
	if len(sources) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "影片分段超过 64 个，无法读取 STRM 内容"})
		return
	}
	out := duplicateSTRMContents{MovieID: id, Files: make([]duplicateSTRMFile, 0, len(sources))}
	for _, source := range sources {
		if c.Request.Context().Err() != nil {
			return
		}
		out.Files = append(out.Files, readDuplicateSTRM(lib.Path, source))
	}
	c.JSON(http.StatusOK, out)
}

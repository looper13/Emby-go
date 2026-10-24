package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) adminDuplicates(c *gin.Context) {
	param := func(name string, fallback, max int64) (int64, error) {
		raw := c.Query(name)
		if raw == "" {
			return fallback, nil
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 || value > max || name == "limit" && value == 0 {
			return 0, strconv.ErrSyntax
		}
		return value, nil
	}
	libraryID, libErr := param("library_id", 0, 1<<63-1)
	limit, limitErr := param("limit", 20, 100)
	offset, offsetErr := param("offset", 0, 1<<31-1)
	if libErr != nil || limitErr != nil || offsetErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的媒体库或分页参数"})
		return
	}
	if libraryID > 0 && !a.db.CanAccessLibrary(libraryID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "媒体库不存在或不可访问"})
		return
	}
	if strings.HasSuffix(c.FullPath(), "/items") {
		key := c.Query("key")
		if strings.TrimSpace(key) == "" || len(key) > 256 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "番号分组不能为空或过长"})
			return
		}
		result, err := a.db.DuplicateMovieMembers(c.Request.Context(), libraryID, key, int(limit), int(offset))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取重复影片失败"})
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}
	result, err := a.db.DuplicateMovieGroups(c.Request.Context(), libraryID, int(limit), int(offset))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "影片查重失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

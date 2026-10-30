package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 只转派显式本地来源；默认来源和 115 票据继续沿用原分支。
func (a *App) r18LocalStream(c *gin.Context) bool {
	const localPrefix = "mediasource_"
	values := []string{}
	local := false
	for key, sources := range c.Request.URL.Query() {
		if !strings.EqualFold(key, "MediaSourceId") {
			continue
		}
		for _, source := range sources {
			values = append(values, source)
			// 这里只识别来源类型，完整 ID 仍逐字匹配，非规范值不能回退 115。
			candidate := strings.TrimSpace(source)
			local = local || len(candidate) >= len(localPrefix) && strings.EqualFold(candidate[:len(localPrefix)], localPrefix)
		}
	}
	if !local {
		return false
	}
	if _, authenticated := c.Get("accountUser"); !authenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "本地虚拟条目播放需要登录"})
		return true
	}
	requested := values[0]
	for _, source := range values[1:] {
		if source != requested {
			c.JSON(http.StatusBadRequest, gin.H{"error": "播放来源参数冲突"})
			return true
		}
	}
	if len(requested) > 128 {
		c.JSON(http.StatusNotFound, gin.H{"error": "本地播放源不存在"})
		return true
	}
	libraryID, videoID, valid := parseR18ItemID(c.Param("id"))
	if !valid {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return true
	}
	library, valid := a.r18CompatLibrary(c, libraryID)
	if !valid {
		return true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	video, err := a.r18ImageVideo(ctx, library, videoID)
	if err != nil {
		a.r18Failure(c, "详情查询", err)
		return true
	}
	matches, err := a.r18LocalMatches(c, video)
	if err != nil {
		a.r18Failure(c, "本地播放源查询", err)
		return true
	}
	for _, match := range matches {
		if requested != "mediasource_"+match.Source.ItemID {
			continue
		}
		// 精确白名单匹配后复用目录流；不改查询，避免污染 Gin 的查询缓存。
		original := c.Params
		c.Params = append(gin.Params(nil), original...)
		defer func() { c.Params = original }()
		for index := range c.Params {
			if c.Params[index].Key == "id" {
				c.Params[index].Value = match.Source.ItemID
				break
			}
		}
		a.stream(c)
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "所选本地 STRM 已失效、番号不匹配或无权访问"})
	return true
}

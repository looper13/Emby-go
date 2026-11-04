package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emby-go/internal/r18"
	"github.com/gin-gonic/gin"
)

// 只转派显式本地来源；默认网盘版本继续由网盘播放入口处理。
func (a *App) r18LocalStream(c *gin.Context) bool {
	const localPrefix = "mediasource_"
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "播放查询参数编码无效"})
		return true
	}
	values := []string{}
	local := false
	for key, sources := range query {
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
	return a.r18StreamLocalMatches(c, requested, matches)
}

// 默认网盘无可用来源时，复用本次请求已验证的影片和本地匹配，避免重复读取上游。
func (a *App) r18DefaultLocalStream(c *gin.Context, video r18.Video, sourceID string) bool {
	if _, authenticated := c.Get("accountUser"); !authenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "本地虚拟条目播放需要登录"})
		return true
	}
	libraryID, videoID, valid := parseR18ItemID(c.Param("id"))
	if !valid || videoID != video.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return true
	}
	if _, valid := a.r18CompatLibrary(c, libraryID); !valid {
		return true
	}
	matches, err := a.r18LocalMatches(c, video)
	if err != nil {
		a.r18Failure(c, "本地播放源查询", err)
		return true
	}
	return a.r18StreamLocalMatches(c, sourceID, matches)
}

func (a *App) r18StreamLocalMatches(c *gin.Context, requested string, matches []r18LocalMatch) bool {
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

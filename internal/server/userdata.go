package server

import (
	"net/http"
	"strconv"
	"strings"

	"emby-go/internal/store"

	"github.com/gin-gonic/gin"
)

// userDataJSON 组装 UserItemDataDto 形态的响应。
func (a *App) userDataJSON(id int64) (gin.H, bool) {
	m, err := a.db.Movie(id)
	if err != nil || !m.IsVisible() {
		return nil, false
	}
	d, _ := a.db.Data(id)
	percentage := float64(0)
	if m.RuntimeSeconds > 0 && d.PositionTicks > 0 {
		percentage = float64(d.PositionTicks) / float64(m.RuntimeSeconds*10000000) * 100
	}
	return gin.H{
		"Key":                   strconv.FormatInt(id, 10),
		"ItemId":                strconv.FormatInt(id, 10),
		"ServerId":              a.serverID,
		"PlaybackPositionTicks": d.PositionTicks,
		"PlayCount":             d.PlayCount,
		"Played":                d.Played,
		"IsFavorite":            d.IsFavorite,
		"PlayedPercentage":      percentage,
		"LastPlayedDate":        d.LastPlayedAt,
	}, true
}

// userDataWrite 在用户数据写后统一 bump 版本：列表/详情缓存 key 均含库版本号，据此自然失效。
func (a *App) userDataWrite(libraryID int64) {
	_ = a.db.BumpVersion(libraryID)
}

func (a *App) favoriteItem(c *gin.Context, favorite bool) {
	if !a.validUser(c) {
		return
	}
	canonical, movie, item, err := a.favoriteTarget(c, c.Param("id"))
	if err != nil {
		if store.NotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	if movie == nil {
		if err := a.db.SetItemFavorite(canonical, favorite); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		data := userDataOf(item)
		data["Key"], data["ItemId"], data["ServerId"], data["IsFavorite"] = canonical, c.Param("id"), a.serverID, favorite
		c.JSON(http.StatusOK, data)
		return
	}
	id := movie.ID
	if err := a.db.SetFavorite(id, favorite); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.userDataWrite(movie.LibraryID)
	resp, ok := a.userDataJSON(id)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	resp["ItemId"] = c.Param("id")
	c.JSON(http.StatusOK, resp)
}

func (a *App) favorite(c *gin.Context)   { a.favoriteItem(c, true) }
func (a *App) unfavorite(c *gin.Context) { a.favoriteItem(c, false) }

func (a *App) rateItem(c *gin.Context, likes int) {
	if !a.validUser(c) {
		return
	}
	movie, err := a.resolveUserMedia(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	id := movie.ID
	if err := a.db.SetLikes(id, likes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.userDataWrite(movie.LibraryID)
	resp, ok := a.userDataJSON(id)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	resp["ItemId"] = c.Param("id")
	c.JSON(http.StatusOK, resp)
}

// rating 处理 POST /Users/{uid}/Items/{id}/Rating?Likes=true|false。
func (a *App) rating(c *gin.Context) {
	switch strings.ToLower(c.Query("Likes")) {
	case "true":
		a.rateItem(c, 1)
	case "false":
		a.rateItem(c, -1)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Likes required (true|false)"})
	}
}

func (a *App) unrate(c *gin.Context) { a.rateItem(c, 0) }

func (a *App) hideFromResume(c *gin.Context) {
	if !a.validUser(c) {
		return
	}
	movie, err := a.resolveUserMedia(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	id := movie.ID
	hide := strings.EqualFold(c.Query("Hide"), "true")
	if err := a.db.SetHideFromResume(id, hide); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	a.userDataWrite(movie.LibraryID)
	resp, ok := a.userDataJSON(id)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	resp["ItemId"] = c.Param("id")
	c.JSON(http.StatusOK, resp)
}

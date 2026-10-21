package server

import (
	"errors"
	"strconv"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) browseLibraries(c *gin.Context) {
	libraries, err := a.db.UserLibraries()
	if err != nil {
		c.JSON(500, gin.H{"error": "读取媒体库失败"})
		return
	}
	items := make([]gin.H, 0, len(libraries))
	for _, lib := range libraries {
		items = append(items, gin.H{"Id": lib.ID, "Name": lib.Name, "Path": "", "Type": lib.Type, "SourceType": lib.SourceType, "Enabled": lib.Enabled})
	}
	c.JSON(200, gin.H{"items": items, "total": len(items)})
}

// Preserve read endpoints used by the media wall, without exposing maintenance
// inventory, import, configuration or scraping to ordinary accounts.
func (a *App) adminBoundary(c *gin.Context) {
	if value, ok := c.Get("accountUser"); ok && value.(store.User).Administrator {
		c.Next()
		return
	}
	p := strings.TrimPrefix(c.FullPath(), "/api/admin")
	allowed := p == "/account" && (c.Request.Method == "GET" || c.Request.Method == "PUT")
	if c.Request.Method == "GET" {
		switch p {
		case "/items", "/items/:id/detail", "/shows", "/anime/bangumi", "/anime/subjects/:id", "/anime/catalog", "/anime/entries/:id", "/anime/entries/:id/episodes", "/anime/entries/:id/releases":
			allowed = true
		}
	}
	if !allowed {
		c.AbortWithStatusJSON(403, gin.H{"error": "需要管理员权限"})
		return
	}
	c.Next()
}

func (a *App) adminUsers(c *gin.Context) {
	users, err := a.db.Users()
	if err != nil {
		c.Status(500)
		return
	}
	out := []gin.H{}
	for _, user := range users {
		out = append(out, (&App{appState: a.appState, db: a.db.ForUser(user.ID)}).userDto())
	}
	libs, err := a.db.Libraries()
	if err != nil {
		c.Status(500)
		return
	}
	folders := []gin.H{}
	for _, lib := range libs {
		folders = append(folders, gin.H{"Id": externalLibraryID(lib.ID), "Name": lib.Name, "Type": lib.Type})
	}
	c.JSON(200, gin.H{"Users": out, "Libraries": folders})
}
func userFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrUserConflict):
		c.JSON(409, gin.H{"error": "用户名已存在"})
	case errors.Is(err, store.ErrProtectedUser):
		c.JSON(400, gin.H{"error": "不能禁用或删除原管理员"})
	case store.NotFound(err):
		c.Status(404)
	default:
		c.JSON(500, gin.H{"error": "保存用户失败"})
	}
}
func (a *App) createUser(c *gin.Context) {
	var req struct{ Username, Pw string }
	if c.ShouldBindJSON(&req) != nil || !validCredentials(req.Username, req.Pw) {
		c.JSON(400, gin.H{"error": "账号须为 3-64 个字符，密码至少 9 个字符且不超过 72 字节"})
		return
	}
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	user, err := a.db.CreateUser(req.Username, req.Pw)
	if err != nil {
		userFailure(c, err)
		return
	}
	c.JSON(201, (&App{appState: a.appState, db: a.db.ForUser(user.ID)}).userDto())
}
func (a *App) editUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(404)
		return
	}
	var req struct {
		Username, NewPassword string
		IsDisabled            bool
	}
	if c.ShouldBindJSON(&req) != nil || len([]rune(strings.TrimSpace(req.Username))) < 3 || len([]rune(strings.TrimSpace(req.Username))) > 64 || (req.NewPassword != "" && !validCredentials(req.Username, req.NewPassword)) {
		c.Status(400)
		return
	}
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	if err = a.db.EditUser(id, req.Username, req.NewPassword, req.IsDisabled); err != nil {
		userFailure(c, err)
		return
	}
	if id == 1 {
		a.setAdminName(req.Username)
	}
	c.Status(204)
}
func (a *App) deleteUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err != nil || id <= 0 {
		c.Status(404)
		return
	}
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	if err = a.db.DeleteUser(id); err != nil {
		userFailure(c, err)
		return
	}
	c.Status(204)
}

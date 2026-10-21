package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) logout(c *gin.Context) {
	if err := a.db.DeleteAccessToken(requestToken(c)); err != nil {
		c.JSON(500, gin.H{"error": "退出登录失败"})
		return
	}
	setSessionCookie(c, "", -1)
	c.Status(http.StatusNoContent)
}

func (a *App) account(c *gin.Context) {
	libs, err := a.db.UserLibraries()
	if user, _ := a.db.User(a.db.UserID()); user.Administrator {
		libs, err = a.db.Libraries()
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取媒体库失败"})
		return
	}
	if _, err := a.db.LibraryAccessPolicy(); err != nil {
		c.JSON(500, gin.H{"error": "读取权限失败"})
		return
	}
	folders := []gin.H{}
	for _, lib := range libs {
		folders = append(folders, gin.H{"Id": externalLibraryID(lib.ID), "Name": lib.Name, "Type": lib.Type})
	}
	c.JSON(200, gin.H{"User": a.userDto(), "Libraries": folders})
}

func (a *App) updateAccount(c *gin.Context) {
	var req struct {
		Username        string
		CurrentPassword string
		NewPassword     string
	}
	if c.ShouldBindJSON(&req) != nil || len([]rune(strings.TrimSpace(req.Username))) < 3 || len([]rune(strings.TrimSpace(req.Username))) > 64 || (req.NewPassword != "" && !validCredentials(req.Username, req.NewPassword)) {
		c.JSON(400, gin.H{"error": "账号须为 3-64 个字符；新密码至少 9 个字符且不超过 72 字节"})
		return
	}
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	if err := a.db.UpdateAdministrator(req.Username, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, store.ErrInvalidPassword) {
			c.JSON(403, gin.H{"error": "当前密码不正确"})
			return
		}
		c.JSON(500, gin.H{"error": "保存账号失败"})
		return
	}
	if a.db.UserID() == 1 {
		a.setAdminName(strings.TrimSpace(req.Username))
	}
	setSessionCookie(c, "", -1)
	c.Status(http.StatusNoContent)
}

func (a *App) updateUserPolicy(c *gin.Context) {
	if value, ok := c.Get("accountUser"); !ok || !value.(store.User).Administrator {
		c.JSON(403, gin.H{"error": "需要管理员权限"})
		return
	}
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	id, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	if _, err = a.db.User(id); err != nil || id <= 0 {
		c.Status(404)
		return
	}
	a = &App{appState: a.appState, db: a.db.ForUser(id)}
	var req struct {
		EnableAllFolders *bool
		EnabledFolders   *[]string
	}
	if c.ShouldBindJSON(&req) != nil || req.EnableAllFolders == nil || req.EnabledFolders == nil {
		c.JSON(400, gin.H{"error": "须提供 EnableAllFolders 和 EnabledFolders"})
		return
	}
	p := store.LibraryAccessPolicy{EnableAllFolders: *req.EnableAllFolders}
	for _, raw := range *req.EnabledFolders {
		id, err := strconv.ParseInt(raw, 10, 64)
		lib, ok := parseLibraryExternal(id)
		if err != nil || !ok || lib <= 0 {
			c.JSON(400, gin.H{"error": "媒体库 ID 无效"})
			return
		}
		p.LibraryIDs = append(p.LibraryIDs, lib)
	}
	if err := a.db.SaveLibraryAccessPolicy(p); err != nil {
		if errors.Is(err, store.ErrInvalidLibrary) {
			c.JSON(400, gin.H{"error": "媒体库已删除或不可用，请刷新后重试"})
			return
		}
		c.JSON(500, gin.H{"error": "保存权限失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type r18Source interface {
	Check(context.Context) error
	List(context.Context, r18.Query) (r18.Page, error)
	Get(context.Context, string) (r18.Video, error)
	Resources(context.Context, r18.Video, int) (r18.ResourcePage, error)
	Estimate(context.Context) (int64, error)
	CatalogCount(context.Context, r18.CatalogQuery) (int64, error)
	CatalogList(context.Context, r18.CatalogQuery) ([]r18.Video, error)
	Close() error
}
type r18ClientEntry struct {
	revision int64
	client   r18Source
}

func (a *App) registerR18Routes(r *gin.Engine) {
	browse := r.Group("/api/r18", a.requireAuth, noStore)
	browse.GET("/libraries", a.bind((*App).r18Libraries))
	browse.GET("/libraries/:lib/videos", a.bind((*App).r18Videos))
	browse.GET("/libraries/:lib/videos/:video", a.bind((*App).r18Video))
	browse.GET("/libraries/:lib/videos/:video/resources", a.bind((*App).r18Resources))
	browse.GET("/libraries/:lib/videos/:video/images/:image", a.bind((*App).r18Image))
	browse.HEAD("/libraries/:lib/videos/:video/images/:image", a.bind((*App).r18Image))
	admin := r.Group("/api/admin/r18", a.requireAuth, a.requireAdministrator, noStore)
	admin.POST("/libraries", a.bind((*App).createR18Library))
	admin.GET("/libraries/:id", a.bind((*App).r18LibraryConfig))
	admin.PUT("/libraries/:id", a.bind((*App).updateR18Library))
}

func (a *App) newR18Client(cfg r18.Config) (r18Source, error) {
	if a.r18Open != nil {
		return a.r18Open(cfg)
	}
	return r18.Open(cfg)
}
func (a *App) r18Client(l store.Library) (r18Source, error) {
	a.r18Mu.Lock()
	defer a.r18Mu.Unlock()
	if old, ok := a.r18Clients[l.ID]; ok && old.revision == l.Revision {
		return old.client, nil
	}
	cfg, e := a.db.R18Connection(l.ID)
	if e != nil {
		return nil, e
	}
	client, e := a.newR18Client(cfg)
	if e != nil {
		return nil, e
	}
	if a.r18Clients == nil {
		a.r18Clients = map[int64]r18ClientEntry{}
	}
	if old, ok := a.r18Clients[l.ID]; ok {
		_ = old.client.Close()
	}
	a.r18Clients[l.ID] = r18ClientEntry{l.Revision, client}
	return client, nil
}
func (a *App) closeR18Clients() {
	a.r18Mu.Lock()
	clients := a.r18Clients
	a.r18Clients = nil
	a.r18Mu.Unlock()
	for _, entry := range clients {
		_ = entry.client.Close()
	}
}
func (a *App) invalidateR18Client(id int64) {
	a.r18Mu.Lock()
	entry, ok := a.r18Clients[id]
	delete(a.r18Clients, id)
	a.r18Mu.Unlock()
	if ok {
		_ = entry.client.Close()
	}
}
func (a *App) r18Library(c *gin.Context) (store.Library, bool) {
	id, e := strconv.ParseInt(c.Param("lib"), 10, 64)
	if e != nil || id <= 0 || !a.db.CanAccessLibrary(id) {
		c.JSON(404, gin.H{"error": "资料库不存在或无权访问"})
		return store.Library{}, false
	}
	l, e := a.db.Library(id)
	if e != nil || l.SourceType != store.SourceR18 {
		c.JSON(404, gin.H{"error": "R18 资料库不存在"})
		return l, false
	}
	return l, true
}
func (a *App) r18Failure(c *gin.Context, stage string, e error) {
	if errors.Is(e, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": "R18 条目不存在"})
		return
	}
	// pgx connection errors may include a DSN; ordinary users never receive it.
	message := e.Error()
	id, _ := strconv.ParseInt(c.Param("lib"), 10, 64)
	if id == 0 {
		if value, ok := c.Get("r18LibraryID"); ok {
			id = value.(int64)
		}
	}
	if cfg, err := a.db.R18Connection(id); err == nil && cfg.Password != "" {
		message = redactR18Error(message, cfg.Password)
	}
	slog.Warn("R18 catalog request failed", "library_id", id, "stage", stage, "error", message)
	c.JSON(http.StatusBadGateway, gin.H{"error": "R18 " + stage + "失败：数据库不可用或查询超时，请重试；标题搜索较慢时可改用番号精确搜索。"})
}
func (a *App) r18Libraries(c *gin.Context) {
	libs, e := a.db.UserLibraries()
	if e != nil {
		c.JSON(500, gin.H{"error": "读取媒体库失败"})
		return
	}
	items := []store.Library{}
	for _, l := range libs {
		if l.SourceType == store.SourceR18 {
			l.Path = ""
			items = append(items, l)
		}
	}
	c.JSON(200, gin.H{"items": items, "total": len(items)})
}
func (a *App) r18Videos(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	q := r18.Query{Search: c.Query("search"), SearchMode: c.Query("search_mode"), Service: c.Query("service"), Cursor: c.Query("cursor"), Order: c.Query("order"), Limit: 40}
	q.Before = c.Query("direction") == "prev"
	if d := c.Query("direction"); d != "" && d != "prev" && d != "next" {
		c.JSON(400, gin.H{"error": "无效的分页方向"})
		return
	}
	if q.SearchMode != "" && q.SearchMode != "number" && q.SearchMode != "title" {
		c.JSON(400, gin.H{"error": "无效的搜索模式"})
		return
	}
	if q.Order != "" && q.Order != "asc" && q.Order != "desc" {
		c.JSON(400, gin.H{"error": "无效的排序方向"})
		return
	}
	if len([]rune(q.Search)) > 120 || len(q.Service) > 64 {
		c.JSON(400, gin.H{"error": "搜索条件过长"})
		return
	}
	if q.Cursor != "" {
		if _, _, e := r18.DecodeID(q.Cursor); e != nil {
			c.JSON(400, gin.H{"error": "无效的分页游标"})
			return
		}
	}
	client, e := a.r18Client(l)
	if e != nil {
		a.r18Failure(c, "连接", e)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	page, e := client.List(ctx, q)
	if e != nil {
		a.r18Failure(c, "列表查询", e)
		return
	}
	estimate, _ := client.Estimate(ctx)
	for i, v := range page.Items {
		page.Items[i] = a.r18Presentation(l, v)
	}
	l.Path = ""
	c.JSON(200, gin.H{"items": page.Items, "next_cursor": page.NextCursor, "previous_cursor": page.PreviousCursor, "has_more": page.HasMore, "has_previous": page.HasPrevious, "estimated_total": estimate, "library": l})
}
func (a *App) r18Video(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	if _, _, e := r18.DecodeID(c.Param("video")); e != nil {
		c.JSON(400, gin.H{"error": "无效的 R18 条目 ID"})
		return
	}
	client, e := a.r18Client(l)
	if e != nil {
		a.r18Failure(c, "连接", e)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	v, e := client.Get(ctx, c.Param("video"))
	if e != nil {
		a.r18Failure(c, "详情查询", e)
		return
	}
	c.JSON(200, gin.H{"video": a.r18Presentation(l, v), "library_name": l.Name, "playable": len(a.r18ReadySources(c, l, v.ID)) > 0})
}
func (a *App) r18Resources(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	if _, _, e := r18.DecodeID(c.Param("video")); e != nil {
		c.JSON(400, gin.H{"error": "无效的 R18 条目 ID"})
		return
	}
	offset, e := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if e != nil || offset < 0 || offset > 10000 {
		c.JSON(400, gin.H{"error": "无效的资源分页位置"})
		return
	}
	client, e := a.r18Client(l)
	if e != nil {
		a.r18Failure(c, "连接", e)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	v, e := client.Get(ctx, c.Param("video"))
	if e != nil {
		a.r18Failure(c, "详情查询", e)
		return
	}
	page, e := client.Resources(ctx, v, offset)
	if e != nil {
		a.r18Failure(c, "磁力资源查询", e)
		return
	}
	c.JSON(200, page)
}

type r18ConnectionRequest struct {
	Name     string  `json:"name"`
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Database string  `json:"database"`
	Username string  `json:"username"`
	Password *string `json:"password"`
	SSLMode  string  `json:"ssl_mode"`
	Revision int64   `json:"revision"`
}

func (r r18ConnectionRequest) config(password string) r18.Config {
	return r18.Config{Host: r.Host, Port: r.Port, Database: r.Database, Username: r.Username, Password: password, SSLMode: r.SSLMode}
}
func (a *App) checkR18Connection(c *gin.Context, cfg r18.Config) bool {
	if e := cfg.Validate(); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return false
	}
	client, e := a.newR18Client(cfg)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效的 PostgreSQL 连接配置"})
		return false
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	if e = client.Check(ctx); e != nil {
		message := e.Error()
		if cfg.Password != "" {
			message = redactR18Error(message, cfg.Password)
		}
		c.JSON(400, gin.H{"error": "连接校验失败（需要只读访问 r18_video 和 article）：" + message})
		return false
	}
	return true
}
func (a *App) createR18Library(c *gin.Context) {
	var req r18ConnectionRequest
	if c.ShouldBindJSON(&req) != nil || req.Password == nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(400, gin.H{"error": "请填写媒体库名称和数据库连接信息"})
		return
	}
	cfg := req.config(*req.Password)
	if !a.checkR18Connection(c, cfg) {
		return
	}
	l, e := a.db.CreateR18Library(req.Name, cfg)
	if e != nil {
		c.JSON(400, gin.H{"error": "保存 R18 媒体库失败"})
		return
	}
	c.JSON(201, l)
}
func (a *App) r18LibraryConfig(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	l, e := a.db.Library(id)
	if e != nil || l.SourceType != store.SourceR18 {
		c.JSON(404, gin.H{"error": "R18 媒体库不存在"})
		return
	}
	cfg, e := a.db.R18Connection(id)
	if e != nil {
		c.JSON(500, gin.H{"error": "读取连接配置失败"})
		return
	}
	c.JSON(200, gin.H{"library": l, "connection": store.MarshalR18Connection(cfg)})
}
func (a *App) updateR18Library(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	l, e := a.db.Library(id)
	if e != nil || l.SourceType != store.SourceR18 {
		c.JSON(404, gin.H{"error": "R18 媒体库不存在"})
		return
	}
	var req r18ConnectionRequest
	if c.ShouldBindJSON(&req) != nil || req.Revision <= 0 {
		c.JSON(400, gin.H{"error": "无效的连接配置"})
		return
	}
	old, e := a.db.R18Connection(id)
	if e != nil {
		c.JSON(500, gin.H{"error": "读取连接配置失败"})
		return
	}
	password := old.Password
	if req.Password != nil {
		password = *req.Password
	}
	cfg := req.config(password)
	if !a.checkR18Connection(c, cfg) {
		return
	}
	l, e = a.db.UpdateR18Connection(id, req.Revision, req.Name, cfg)
	if errors.Is(e, store.ErrConflict) {
		c.JSON(409, gin.H{"error": "媒体库已被修改，请刷新后重试"})
		return
	}
	if e != nil {
		c.JSON(500, gin.H{"error": "保存连接配置失败"})
		return
	}
	a.invalidateR18Client(id)
	c.JSON(200, l)
}

func redactR18Error(message, password string) string {
	if password == "" {
		return message
	}
	for _, secret := range []string{url.QueryEscape(password), url.PathEscape(password), strings.TrimPrefix(url.UserPassword("", password).String(), ":"), password} {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}

// TODO: Add playable sources separately; magnets are not HTTP media streams.

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
	Resource(context.Context, r18.Video, string) (r18.Resource, error)
	CatalogPage(context.Context, r18.CatalogQuery) (r18.CatalogPage, error)
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
	browse.GET("/libraries/:lib/videos/:video/local-playback", a.bind((*App).r18LocalPlayback))
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
		if errors.Is(e, sql.ErrNoRows) {
			return nil, errors.New("尚未配置 JavDB API 连接")
		}
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
	// 上游错误仅用于脱敏日志，响应不附带原始正文或 API Key。
	message := e.Error()
	id, _ := strconv.ParseInt(c.Param("lib"), 10, 64)
	if id == 0 {
		if value, ok := c.Get("r18LibraryID"); ok {
			id = value.(int64)
		}
	}
	if cfg, err := a.db.R18Connection(id); err == nil && cfg.APIKey != "" {
		message = redactR18Error(message, cfg.APIKey)
	}
	slog.Warn("JavDB catalog request failed", "library_id", id, "stage", stage, "error", message)
	c.JSON(http.StatusBadGateway, gin.H{"error": "JavDB " + stage + "失败：API 不可用、未配置或请求超时，请检查连接配置后重试。"})
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
func r18BrowseQuery(c *gin.Context) (r18.Query, error) {
	search := strings.TrimSpace(c.Query("search"))
	defaultSort := "release"
	if search != "" {
		defaultSort = "relevance"
	}
	q := r18.Query{Search: search, Type: c.DefaultQuery("type", "all"), Filter: c.DefaultQuery("filter", "magnets"),
		Sort: c.DefaultQuery("sort", defaultSort), Order: c.DefaultQuery("order", "desc"), Year: strings.TrimSpace(c.Query("year"))}
	var err error
	if q.Page, err = strconv.Atoi(c.DefaultQuery("page", "1")); err != nil || q.Page < 1 || q.Page > 9999 {
		return q, errors.New("无效的页码")
	}
	if q.Limit, err = strconv.Atoi(c.DefaultQuery("limit", "24")); err != nil || q.Limit < 1 || q.Limit > 50 {
		return q, errors.New("无效的每页条数")
	}
	return q, r18.ValidateQuery(q)
}

func (a *App) r18Videos(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	q, e := r18BrowseQuery(c)
	if e != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": e.Error()})
		return
	}
	client, e := a.r18Client(l)
	if e != nil {
		a.r18Failure(c, "连接", e)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	page, e := client.List(ctx, q)
	if e != nil {
		a.r18Failure(c, "列表查询", e)
		return
	}
	if e = a.prepareR18LocalSources(c, page.Items); e != nil {
		a.r18Failure(c, "本地播放源查询", e)
		return
	}
	for i, v := range page.Items {
		matches, _ := a.r18LocalMatches(c, v)
		v.HasLocal = len(matches) > 0
		page.Items[i] = a.r18Presentation(l, v)
	}
	l.Path = ""
	c.JSON(200, gin.H{"items": page.Items, "page": page.Page, "page_size": page.PageSize, "has_more": page.HasMore, "has_previous": page.HasPrevious, "library": l})
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	v, e := client.Get(ctx, c.Param("video"))
	if e != nil {
		a.r18Failure(c, "详情查询", e)
		return
	}
	matches, e := a.r18LocalMatches(c, v)
	if e != nil {
		a.r18Failure(c, "本地播放源查询", e)
		return
	}
	v.HasLocal = len(matches) > 0
	playable := v.HasLocal
	if !playable {
		playable = len(a.r18ReadySources(c, l, v.ID)) > 0
	}
	c.JSON(200, gin.H{"video": a.r18DetailPresentation(l, v), "library_name": l.Name, "playable": playable, "local_sources": localR18Presentation(matches)})
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
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
	BaseURL  string  `json:"base_url"`
	APIKey   *string `json:"api_key"`
	Revision int64   `json:"revision"`
}

func (r r18ConnectionRequest) config(apiKey string) r18.Config {
	return r18.Config{BaseURL: r.BaseURL, APIKey: apiKey}
}
func (a *App) checkR18Connection(c *gin.Context, cfg r18.Config) bool {
	if e := cfg.Validate(); e != nil {
		c.JSON(400, gin.H{"error": "请填写有效的 API 地址和 API Key"})
		return false
	}
	client, e := a.newR18Client(cfg)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效的 JavDB API 连接配置"})
		return false
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if e = client.Check(ctx); e != nil {
		slog.Warn("JavDB API connection check failed", "error", redactR18Error(e.Error(), cfg.APIKey))
		c.JSON(400, gin.H{"error": "JavDB API 连接校验失败，请检查服务地址、API Key 和服务状态"})
		return false
	}
	return true
}
func (a *App) createR18Library(c *gin.Context) {
	var req r18ConnectionRequest
	if c.ShouldBindJSON(&req) != nil || req.APIKey == nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(400, gin.H{"error": "请填写媒体库名称、API 地址和 API Key"})
		return
	}
	cfg := req.config(strings.TrimSpace(*req.APIKey))
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
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
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
	if c.ShouldBindJSON(&req) != nil || req.Revision <= 0 || strings.TrimSpace(req.Name) == "" {
		c.JSON(400, gin.H{"error": "无效的连接配置"})
		return
	}
	if req.Revision != l.Revision {
		c.JSON(409, gin.H{"error": "媒体库已被修改，请刷新后重试"})
		return
	}
	old, e := a.db.R18Connection(id)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		c.JSON(500, gin.H{"error": "读取连接配置失败"})
		return
	}
	apiKey := old.APIKey
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		apiKey = strings.TrimSpace(*req.APIKey)
	}
	cfg := req.config(apiKey)
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

func redactR18Error(message, apiKey string) string {
	if apiKey == "" {
		return message
	}
	for _, secret := range []string{url.QueryEscape(apiKey), url.PathEscape(apiKey), strings.TrimPrefix(url.UserPassword("", apiKey).String(), ":"), apiKey} {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}

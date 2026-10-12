package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/scheduler"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) registerAnime(g *gin.RouterGroup) {
	g.GET("/anime/catalog", a.animeCatalog)
	g.GET("/anime/entries/:id", a.animeEntry)
	g.GET("/anime/entries/:id/episodes", a.animeEntryEpisodes)
	g.GET("/anime/entries/:id/releases", a.animeEntryReleases)
	g.GET("/anime/entries/:id/mapping-candidates", a.animeMappingCandidates)
	g.PUT("/anime/entries/:id/mapping", a.animeMapping)
	g.GET("/anime/media-candidates", a.animeMediaCandidates)
	g.PUT("/anime/bindings/:target", a.animeBind)
	g.DELETE("/anime/bindings/:target", a.animeBind)
	g.POST("/anime/bindings/:target/check", a.animeCheck)
	g.POST("/anime/refresh", a.animeRefresh)
	g.GET("/anime/progress", a.animeProgress)
	g.POST("/anime/cancel", a.animeCancel)
	g.GET("/anime/settings", a.animeSettings)
	g.PUT("/anime/settings", a.animeSaveSettings)
	g.POST("/anime/test", a.animeTestSource)
	g.GET("/anime/search", a.animeSearch)
}

func animeError(c *gin.Context, err error) {
	code := http.StatusBadGateway
	switch {
	case errors.Is(err, sql.ErrNoRows):
		code = 404
	case errors.Is(err, store.ErrConflict), errors.Is(err, scheduler.ErrBusy):
		code = 409
	case errors.Is(err, context.DeadlineExceeded):
		code = 504
	case errors.Is(err, context.Canceled):
		code = 499
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
func (a *App) animeEnabled(c *gin.Context) bool {
	l, err := a.db.AnimeLibrary()
	if err != nil {
		animeError(c, err)
		return false
	}
	if !l.Enabled {
		c.JSON(409, gin.H{"error": "动漫库已关闭，可在动漫设置中重新启用"})
		return false
	}
	return true
}
func animeEntryID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 || id > 1e9 {
		c.JSON(400, gin.H{"error": "条目 ID 无效"})
		return 0, false
	}
	return id, true
}
func animePage(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
func animeSeasonQuery(c *gin.Context) (int, int, bool) {
	now := time.Now()
	year, err := strconv.Atoi(c.DefaultQuery("year", strconv.Itoa(now.Year())))
	season, e := strconv.Atoi(c.DefaultQuery("season", strconv.Itoa((int(now.Month())-1)/3+1)))
	if err != nil || e != nil || year < 1900 || year > now.Year()+1 || season < 1 || season > 4 {
		c.JSON(400, gin.H{"error": "年份或季度无效"})
		return 0, 0, false
	}
	return year, season, true
}

func (a *App) animeCatalog(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	year, season, ok := animeSeasonQuery(c)
	if !ok {
		return
	}
	day, err := strconv.Atoi(c.DefaultQuery("weekday", "-1"))
	if err != nil || day < -1 || day > 8 {
		c.JSON(400, gin.H{"error": "星期筛选无效"})
		return
	}
	limit, offset := animePage(c)
	filter := store.AnimeCatalogFilter{Source: c.Query("source"), OnlyPlayable: c.Query("playable") == "true"}
	if filter.Source != "" && filter.Source != "mikan" && filter.Source != "bangumi" {
		c.JSON(400, gin.H{"error": "资料来源筛选无效"})
		return
	}
	if filter.OnlyPlayable {
		for _, p := range a.animeProjection() {
			filter.PlayableSubjects = append(filter.PlayableSubjects, p.SubjectID)
		}
	}
	scope := fmt.Sprintf("catalog:%d:%d", year, season)
	var refreshErr error
	ttl := 6 * time.Hour
	now := time.Now()
	if year < now.Year() || year == now.Year() && season < (int(now.Month())-1)/3+1 {
		ttl = 7 * 24 * time.Hour
	}
	if a.animeNeedsRefresh(scope, ttl) {
		_, refreshErr = a.syncAnimeSeason(year, season)
	}
	items, total, err := a.db.AnimeCatalog(year, season, day, c.Query("search"), limit, offset, filter)
	if err != nil {
		animeError(c, err)
		return
	}
	state, err := a.db.AnimeSync(scope)
	if err != nil {
		animeError(c, err)
		return
	}
	status := 200
	if errors.Is(refreshErr, scheduler.ErrBusy) {
		state.Status = "queued"
	}
	if total == 0 && (state.Status == "running" || state.Status == "queued") {
		status = 202
	}
	if total == 0 && state.Status == "failed" {
		c.JSON(502, gin.H{"error": state.Error, "state": state})
		return
	}
	c.JSON(status, gin.H{"items": items, "total": total, "state": state, "stale": state.Status != "success"})
}

func (a *App) animeEntry(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	entry, err := a.db.AnimeEntry(id)
	if err != nil {
		animeError(c, err)
		return
	}
	if err = a.db.TouchAnimeEntry(id); err != nil {
		animeError(c, err)
		return
	}
	scope := fmt.Sprintf("entry:%d", id)
	var refreshErr error
	if a.animeNeedsRefresh(scope, 30*time.Minute) {
		_, refreshErr = a.syncAnimeEntryAuto(id)
	}
	var subject *anime.Subject
	var binding *anime.Binding
	if entry.SubjectID > 0 {
		sub, e := a.db.AnimeSubject(entry.SubjectID)
		if e != nil {
			animeError(c, e)
			return
		}
		subject = &sub
		entry.BangumiID = sub.BangumiID
		if sub.IsMovie() {
			b := a.animeBindingView(sub.ID, 0)
			binding = &b
		}
	}
	state, _ := a.db.AnimeSync(scope)
	if errors.Is(refreshErr, scheduler.ErrBusy) {
		state.Status = "queued"
	}
	c.JSON(200, gin.H{"entry": entry, "subject": subject, "binding": binding, "state": state, "stale": state.Status != "success"})
}

func (a *App) animeBindingView(sid, eid int64) anime.Binding {
	b, err := a.db.AnimeBinding(sid, eid)
	if err != nil {
		return anime.Binding{SubjectID: sid, EpisodeID: eid, Status: "unavailable", Error: "关联读取失败"}
	}
	if b.MovieID == 0 {
		return b
	}
	raw := anime.ID("movie", sid)
	if eid > 0 {
		raw = anime.ID("episode", eid)
	}
	a.anime.mu.Lock()
	checking := a.anime.checks[raw] != nil
	a.anime.mu.Unlock()
	if checking {
		b.Status = "checking"
		return b
	}
	_, _, source, err := a.animeMedia(raw)
	if err != nil {
		b.Status, b.Error = "unavailable", "原媒体不可见、库已关闭或 STRM 不可用"
	} else if b.SourceVersion != "" && b.SourceVersion != anime.SourceVersion(source) {
		b.Status = "unchecked"
	}
	return b
}

func (a *App) animeEntryEpisodes(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	entry, err := a.db.AnimeEntry(id)
	if err != nil {
		animeError(c, err)
		return
	}
	out := []gin.H{}
	eps, err := a.db.AnimeEpisodes(entry.SubjectID)
	if err != nil {
		animeError(c, err)
		return
	}
	limit, offset := animePage(c)
	end := min(len(eps), offset+limit)
	start := min(offset, len(eps))
	for _, ep := range eps[start:end] {
		b := a.animeBindingView(entry.SubjectID, ep.ID)
		d := store.UserData{}
		if b.MovieID > 0 {
			d, _ = a.db.Data(b.MovieID)
		}
		out = append(out, gin.H{"episode": ep, "binding": b, "item_id": anime.ID("episode", ep.ID), "userdata": d})
	}
	c.JSON(200, gin.H{"items": out, "total": len(eps)})
}
func (a *App) animeEntryReleases(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	if _, err := a.db.AnimeEntry(id); err != nil {
		animeError(c, err)
		return
	}
	limit, offset := animePage(c)
	items, total, err := a.db.AnimeReleases(id, limit, offset, c.Query("filter"))
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total})
}

func (a *App) animeRefresh(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	var req struct {
		ID     int64 `json:"id"`
		Year   int   `json:"year"`
		Season int   `json:"season"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "刷新参数无效"})
		return
	}
	var job *animeJob
	var err error
	if req.ID > 0 && req.ID <= 1e9 {
		job, err = a.syncAnimeEntry(req.ID)
	} else if req.ID == 0 && req.Year >= 1900 && req.Year <= time.Now().Year()+1 && req.Season >= 1 && req.Season <= 4 {
		job, err = a.syncAnimeSeason(req.Year, req.Season)
	} else {
		c.JSON(400, gin.H{"error": "刷新范围无效"})
		return
	}
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(202, gin.H{"task_id": job.ID})
}
func (a *App) animeProgress(c *gin.Context) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	if a.anime.job == nil {
		c.JSON(200, gin.H{"id": 0, "scope": "", "running": false, "error": ""})
		return
	}
	c.JSON(200, a.anime.job)
}
func (a *App) animeCancel(c *gin.Context) {
	a.anime.mu.Lock()
	if j := a.anime.job; j != nil && j.Running {
		j.cancel()
	}
	a.anime.mu.Unlock()
	c.Status(204)
}

func (a *App) animeSettings(c *gin.Context) {
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		animeError(c, err)
		return
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"source": cfg, "library": lib})
}
func (a *App) animeSaveSettings(c *gin.Context) {
	var req struct {
		Source  anime.Settings `json:"source"`
		Library store.Library  `json:"library"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "设置格式无效"})
		return
	}
	if err := anime.ValidateBaseURL(req.Source.MikanURL); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if name := strings.TrimSpace(req.Library.Name); name == "" || len([]rune(name)) > 100 {
		c.JSON(400, gin.H{"error": "库名称需要 1 至 100 个字符"})
		return
	}
	a.anime.mu.Lock()
	_, err := a.db.SaveAnimeSettings(req.Source, req.Library.Name, req.Library.Enabled, req.Library.Revision)
	if err == nil && a.anime.job != nil && a.anime.job.Running {
		a.anime.job.cancel()
	}
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	a.animeSettings(c)
}
func (a *App) animeTestSource(c *gin.Context) {
	var req anime.Settings
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "设置格式无效"})
		return
	}
	if err := anime.ValidateBaseURL(req.MikanURL); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	client := anime.NewClient(req.MikanURL)
	now := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	client.Context = ctx
	entries, err := client.Season(ctx, now.Year(), (int(now.Month())-1)/3+1)
	if err != nil {
		animeError(c, err)
		return
	}
	_, _, err = client.SearchSubjects(ctx, "动画", 0)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true, "entries": len(entries), "bangumi": true})
}

func (a *App) animeMappingCandidates(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	entry, err := a.db.AnimeEntry(id)
	if err != nil {
		animeError(c, err)
		return
	}
	keyword := c.DefaultQuery("search", entry.Name)
	if strings.TrimSpace(keyword) == "" || len([]rune(keyword)) > 100 {
		c.JSON(400, gin.H{"error": "搜索词需要 1 至 100 个字符"})
		return
	}
	a.anime.mu.Lock()
	cfg, err := a.db.AnimeSettings()
	client := a.animeSourceLocked(cfg)
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	_, offset := animePage(c)
	items, total, err := client.SearchSubjects(c.Request.Context(), keyword, offset)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total})
}
func (a *App) animeMapping(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	var req struct {
		BangumiID int64 `json:"bangumi_id"`
		Revision  int64 `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || req.BangumiID <= 0 || req.Revision <= 0 {
		c.JSON(400, gin.H{"error": "映射参数无效"})
		return
	}
	current, err := a.db.AnimeEntry(id)
	if err != nil {
		animeError(c, err)
		return
	}
	if current.Revision != req.Revision {
		animeError(c, store.ErrConflict)
		return
	}
	job, err := a.startAnimeSync(fmt.Sprintf("mapping:%d:%d:%d", id, req.BangumiID, req.Revision), func(ctx context.Context, client *anime.Client, commit func(func() error) error) error {
		sub, eps, err := client.Subject(ctx, req.BangumiID)
		if err != nil {
			return err
		}
		return commit(func() error {
			sid, e := a.db.SaveAnimeSubject(ctx, sub, eps)
			if e != nil {
				return e
			}
			return a.db.MapAnimeEntry(id, sid, req.Revision, true)
		})
	})
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(202, gin.H{"task_id": job.ID})
}

func (a *App) animeMediaCandidates(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	limit, offset := animePage(c)
	lib, _ := strconv.ParseInt(c.Query("library_id"), 10, 64)
	items, total, err := a.db.SearchAdmin(store.AdminQuery{LibraryID: lib, Term: c.Query("search"), Limit: limit, Offset: offset, SortBy: "title"})
	if err != nil {
		animeError(c, err)
		return
	}
	out := []gin.H{}
	for _, m := range items {
		reason := ""
		if !m.IsVisible() || len(m.AdditionalParts) != 0 || !strings.EqualFold(filepath.Ext(m.SourcePath), ".strm") || a.db.RequireDirectory(m.LibraryID) != nil {
			reason = "仅支持启用目录库中的可见单文件 STRM"
		}
		if reason == "" {
			if _, sourceErr := readAnimeSource(m.SourcePath); sourceErr != nil {
				reason = "STRM 文件缺失或内容不是单个 HTTP/HTTPS 地址"
			}
		}
		occupied, e := a.db.AnimeBindingOccupied(m.ID)
		if e != nil {
			animeError(c, e)
			return
		}
		library, _ := a.db.Library(m.LibraryID)
		out = append(out, gin.H{"id": m.ID, "name": m.Title, "library_id": m.LibraryID, "library_name": library.Name, "path": m.SourcePath, "eligible": reason == "", "reason": reason, "occupied": occupied})
	}
	c.JSON(200, gin.H{"items": out, "total": total})
}
func (a *App) animeBind(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	raw := c.Param("target")
	sub, ep, _, err := a.db.AnimeTarget(raw)
	if err != nil {
		animeError(c, err)
		return
	}
	var req struct {
		MovieID  int64 `json:"movie_id"`
		Revision int64 `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Revision < 0 || req.MovieID < 0 {
		c.JSON(400, gin.H{"error": "关联参数无效"})
		return
	}
	if c.Request.Method == "DELETE" {
		req.MovieID = 0
	}
	if req.MovieID > 0 {
		m, e := a.db.Movie(req.MovieID)
		if e != nil {
			animeError(c, e)
			return
		}
		_, sourceErr := readAnimeSource(m.SourcePath)
		if sourceErr != nil || !m.IsVisible() || len(m.AdditionalParts) > 0 || !strings.EqualFold(filepath.Ext(m.SourcePath), ".strm") || a.db.RequireDirectory(m.LibraryID) != nil {
			c.JSON(400, gin.H{"error": "媒体源需要有效的 HTTP/HTTPS STRM"})
			return
		}
	}
	b, err := a.db.BindAnime(sub.ID, ep.ID, req.MovieID, req.Revision)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, b)
}
func (a *App) animeCheck(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	raw := c.Param("target")
	if err := a.checkAnimeMedia(c.Request.Context(), raw, true); err != nil {
		animeError(c, err)
		return
	}
	sub, ep, _, err := a.db.AnimeTarget(raw)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, a.animeBindingView(sub.ID, ep.ID))
}
func (a *App) animeSearch(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	keyword := strings.TrimSpace(c.Query("q"))
	if keyword == "" || len([]rune(keyword)) > 100 {
		c.JSON(400, gin.H{"error": "搜索词需要 1 至 100 个字符"})
		return
	}
	a.anime.mu.Lock()
	cfg, err := a.db.AnimeSettings()
	client := a.animeSourceLocked(cfg)
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	items, err := client.SearchMikan(c.Request.Context(), keyword)
	if err != nil {
		animeError(c, err)
		return
	}
	limit, offset := animePage(c)
	start := min(offset, len(items))
	end := min(start+limit, len(items))
	c.JSON(200, gin.H{"items": items[start:end], "total": len(items)})
}

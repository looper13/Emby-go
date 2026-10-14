package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/anime/artwork"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) tmdbClient() (*anime.TMDBClient, store.AnimeTMDBConfig, error) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	cfg, err := a.db.AnimeTMDBConfig()
	if err != nil {
		return nil, cfg, err
	}
	if !cfg.Configured {
		return nil, cfg, errors.New("tmdb_not_configured")
	}
	if a.anime.tmdb == nil || a.anime.tmdb.APIKey != cfg.APIKey {
		a.anime.tmdb = anime.NewTMDBClient(cfg.APIKey)
	}
	return a.anime.tmdb, cfg, nil
}
func (a *App) animeTMDBSettings(c *gin.Context) {
	v, err := a.db.AnimeTMDBConfig()
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, v)
}
func (a *App) animeTMDBTest(c *gin.Context) {
	client, _, err := a.tmdbClient()
	if err != nil {
		animeError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err = client.Validate(ctx); err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (a *App) animeTMDBSaveSettings(c *gin.Context) {
	var req struct {
		APIKey   string `json:"api_key"`
		Clear    bool   `json:"clear"`
		Revision int64  `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.APIKey) > 256 || strings.ContainsAny(strings.TrimSpace(req.APIKey), "\r\n\t ") || req.Clear && strings.TrimSpace(req.APIKey) != "" {
		c.JSON(400, gin.H{"error": "TMDB 凭据格式无效"})
		return
	}
	a.anime.mu.Lock()
	err := a.db.SaveAnimeTMDBConfig(req.APIKey, req.Clear, req.Revision)
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	a.animeTMDBSettings(c)
}
func (a *App) animeTMDBSearch(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" || len([]rune(q)) > 100 {
		c.JSON(400, gin.H{"error": "查询词需要 1 至 100 个字符"})
		return
	}
	client, _, err := a.tmdbClient()
	if err != nil {
		animeError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	v, err := client.Search(ctx, q)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"items": v})
}
func (a *App) animeTMDBSeriesDetail(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	client, _, err := a.tmdbClient()
	if err != nil {
		animeError(c, err)
		return
	}
	v, err := client.Series(c.Request.Context(), id)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, v)
}
func (a *App) animeTMDBSeason(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	season, err := strconv.Atoi(c.Param("season"))
	if err != nil || season < 0 || season > 1000 {
		c.Status(400)
		return
	}
	client, _, err := a.tmdbClient()
	if err != nil {
		animeError(c, err)
		return
	}
	v, err := client.Season(c.Request.Context(), id, season)
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, v)
}
func (a *App) animeTMDBMapSeries(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	var req struct {
		SeriesID int64 `json:"series_id"`
		Revision int64 `json:"revision"`
	}
	if c.ShouldBindJSON(&req) != nil || req.SeriesID < 0 || req.SeriesID > 1e9 {
		c.Status(400)
		return
	}
	sub, err := a.loadBangumiSubject(c.Request.Context(), id)
	if err != nil {
		animeError(c, err)
		return
	}
	v := store.AnimeTMDBSeries{SubjectID: sub.ID, SeriesID: req.SeriesID, Revision: req.Revision}
	cfg, err := a.db.AnimeTMDBConfig()
	if err != nil {
		animeError(c, err)
		return
	}
	if req.SeriesID > 0 {
		client, config, err := a.tmdbClient()
		if err != nil {
			animeError(c, err)
			return
		}
		cfg = config
		remote, err := client.Series(c.Request.Context(), req.SeriesID)
		if err != nil {
			animeError(c, err)
			return
		}
		v.Name = remote.Name
	}
	a.anime.mu.Lock()
	lib, err := a.db.AnimeLibrary()
	if err == nil && !lib.Enabled {
		err = store.ErrConflict
	}
	if err == nil {
		err = a.db.SaveAnimeTMDBSeries(c.Request.Context(), v, cfg.Revision)
	}
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	v.Revision++
	c.JSON(200, v)
}
func (a *App) animeTMDBMapEpisode(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	var req struct {
		Season         int   `json:"season"`
		Number         int   `json:"number"`
		Revision       int64 `json:"revision"`
		SeriesRevision int64 `json:"series_revision"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Season < 0 || req.Season > 1000 || req.Number < 0 || req.Number > 10000 {
		c.Status(400)
		return
	}
	ep, err := a.db.AnimeEpisode(id)
	if err != nil {
		animeError(c, err)
		return
	}
	series, err := a.db.AnimeTMDBSeries(ep.SubjectID)
	if err != nil {
		animeError(c, err)
		return
	}
	if series.SeriesID == 0 || series.Revision != req.SeriesRevision {
		animeError(c, store.ErrConflict)
		return
	}
	v := store.AnimeTMDBEpisode{EpisodeID: id, SubjectID: ep.SubjectID, Revision: req.Revision, Stills: []string{}}
	cfg, err := a.db.AnimeTMDBConfig()
	if err != nil {
		animeError(c, err)
		return
	}
	if c.Request.Method != "DELETE" {
		client, config, err := a.tmdbClient()
		if err != nil {
			animeError(c, err)
			return
		}
		cfg = config
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		remote, err := client.Season(ctx, series.SeriesID, req.Season)
		if err != nil {
			animeError(c, err)
			return
		}
		var selected anime.TMDBEpisode
		for _, e := range remote.Episodes {
			if e.Number == req.Number && e.Season == req.Season {
				selected = e
				break
			}
		}
		if selected.ID <= 0 {
			c.JSON(400, gin.H{"error": "所选 TMDB 章节不存在"})
			return
		}
		stills, err := client.Stills(ctx, series.SeriesID, req.Season, req.Number, selected.ID)
		if err != nil {
			animeError(c, err)
			return
		}
		v.SeriesID = series.SeriesID
		v.Season = req.Season
		v.Number = req.Number
		v.TMDBID = selected.ID
		v.Name = selected.Name
		v.Stills = stills
	}
	a.anime.mu.Lock()
	lib, err := a.db.AnimeLibrary()
	if err == nil && !lib.Enabled {
		err = store.ErrConflict
	}
	if err == nil {
		err = a.db.SaveAnimeTMDBEpisode(c.Request.Context(), v, req.SeriesRevision, cfg.Revision)
	}
	a.anime.mu.Unlock()
	if err != nil {
		animeError(c, err)
		return
	}
	v.Revision++
	c.JSON(200, v)
}
func (a *App) animeEpisodeStill(c *gin.Context) {
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil || !lib.Enabled || a.artwork == nil {
		c.Status(404)
		return
	}
	revision, e := strconv.ParseInt(c.Param("revision"), 10, 64)
	index, e2 := strconv.Atoi(c.Param("index"))
	m, err := a.db.AnimeTMDBEpisode(id)
	if err != nil || e != nil || e2 != nil || m.Revision != revision || m.SeriesID <= 0 || index < 0 || index >= len(m.Stills) {
		c.Status(404)
		return
	}
	raw := anime.TMDBImageURL(m.Stills[index])
	if raw == "" {
		c.Status(404)
		return
	}
	client, err := a.imageClient()
	if err != nil {
		c.Status(503)
		return
	}
	k := artwork.Key{Kind: "episode", ID: id, Variant: fmt.Sprintf("tmdb_%s", anime.SourceVersion(raw))}
	asset, err := a.artwork.Ensure(c.Request.Context(), k, false, func(ctx context.Context) ([]byte, string, error) {
		data, e := client.Artwork(ctx, raw)
		return data, raw, e
	})
	if err != nil || asset.Path == "" {
		c.Header("Cache-Control", "no-store")
		c.Status(404)
		return
	}
	current, e := a.db.AnimeTMDBEpisode(id)
	if e != nil || current.Revision != revision || current.SeriesID == 0 {
		c.Status(404)
		return
	}
	a.serveArtwork(c, asset)
}

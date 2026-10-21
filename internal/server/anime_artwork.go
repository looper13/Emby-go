package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/anime/artwork"
	"github.com/gin-gonic/gin"
)

func (a *App) artworkOwner(ctx context.Context, kind string, id int64) bool {
	if kind == "subject" {
		if a.archive.SubjectExists(ctx, id) {
			return true
		}
		_, err := a.db.AnimeSubjectByBangumiID(id)
		return err == nil
	}
	return (kind == "person" || kind == "character") && a.archive.PersonExists(ctx, kind, id)
}
func (a *App) imageClient() (*anime.Client, error) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		return nil, err
	}
	return a.animeSourceLocked(cfg), nil
}
func (a *App) bangumiArtwork(ctx context.Context, k artwork.Key, retry bool) (artwork.Asset, error) {
	if a.artwork == nil {
		return artwork.Asset{}, errors.New("artwork_store_unavailable")
	}
	client, err := a.imageClient()
	if err != nil {
		return artwork.Asset{}, err
	}
	return a.artwork.Ensure(ctx, k, retry, func(ctx context.Context) ([]byte, string, error) {
		return client.BangumiImage(ctx, k.Kind, k.ID, k.Variant)
	})
}
func (a *App) serveBangumiArtwork(c *gin.Context, k artwork.Key) {
	if k.Variant != "common" && k.Variant != "large" && k.Variant != "medium" && k.Variant != "small" && k.Variant != "grid" {
		c.Status(400)
		return
	}
	asset, err := a.bangumiArtwork(c.Request.Context(), k, false)
	if asset.Path == "" {
		c.Header("Cache-Control", "no-store")
		if errors.Is(err, artwork.ErrBusy) {
			c.Header("Retry-After", "3")
			c.Status(503)
		} else {
			c.Status(404)
		}
		return
	}
	a.serveArtwork(c, asset)
}
func (a *App) serveArtwork(c *gin.Context, asset artwork.Asset) {
	lib, err := a.db.AnimeLibrary()
	if err != nil || !lib.Enabled || !a.db.CanAccessLibrary(lib.ID) {
		c.Status(404)
		return
	}
	setPrivateImageCache(c)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", asset.MIME)
	tag := `"` + asset.Hash + `"`
	c.Header("ETag", tag)
	if c.GetHeader("If-None-Match") == tag {
		c.Status(304)
		return
	}
	c.File(asset.Path)
}
func (a *App) animePersonImage(c *gin.Context) {
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	lib, err := a.db.AnimeLibrary()
	kind := c.Param("kind")
	if err != nil || !lib.Enabled || !a.artworkOwner(c.Request.Context(), kind, id) {
		c.Status(404)
		return
	}
	a.serveBangumiArtwork(c, artwork.Key{Kind: kind, ID: id, Variant: c.DefaultQuery("size", "medium")})
}

type artworkJob struct {
	ID        int64  `json:"id"`
	Running   bool   `json:"running"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Missing   int    `json:"missing"`
	Failed    int    `json:"failed"`
	Error     string `json:"error"`
	cancel    context.CancelFunc
}

func (a *App) artworkProgress(c *gin.Context) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	c.JSON(200, gin.H{"job": a.artworkJob, "available": a.artwork != nil})
}
func (a *App) artworkCancel(c *gin.Context) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	if a.artworkJob != nil && a.artworkJob.Running {
		a.artworkJob.cancel()
	}
	c.Status(204)
}
func (a *App) artworkStart(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	var req struct {
		Subjects []int64 `json:"subject_ids"`
		People   bool    `json:"people"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Subjects) == 0 || len(req.Subjects) > 50 {
		c.JSON(400, gin.H{"error": "每批需要 1 至 50 个作品"})
		return
	}
	if a.artwork == nil {
		c.JSON(503, gin.H{"error": "图片归档不可用"})
		return
	}
	keys := []artwork.Key{}
	seen := map[string]bool{}
	add := func(k artwork.Key) {
		if !seen[k.String()] {
			keys = append(keys, k)
			seen[k.String()] = true
		}
	}
	for _, id := range req.Subjects {
		if !a.artworkOwner(c.Request.Context(), "subject", id) {
			c.Status(404)
			return
		}
		add(artwork.Key{Kind: "subject", ID: id, Variant: "common"})
		if req.People {
			people, _ := a.archive.People(c.Request.Context(), id)
			for _, p := range people {
				add(artwork.Key{Kind: p.Kind, ID: p.ID, Variant: "medium"})
			}
		}
	}
	if len(keys) > 500 {
		c.JSON(400, gin.H{"error": "每批最多 500 张图片，请减少作品数量"})
		return
	}
	a.anime.mu.Lock()
	if a.rootCtx.Err() != nil {
		a.anime.mu.Unlock()
		c.Status(503)
		return
	}
	if a.artworkJob != nil && a.artworkJob.Running {
		a.anime.mu.Unlock()
		c.Status(409)
		return
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil || !lib.Enabled {
		a.anime.mu.Unlock()
		c.Status(409)
		return
	}
	ctx, cancel := context.WithTimeout(a.rootCtx, 45*time.Minute)
	j := &artworkJob{ID: a.startTask("anime_artwork"), Running: true, Status: "running", Total: len(keys), cancel: cancel}
	a.artworkJob = j
	a.anime.wg.Add(1)
	a.anime.mu.Unlock()
	go func() {
		defer a.anime.wg.Done()
		defer cancel()
		for _, k := range keys {
			if ctx.Err() != nil {
				break
			}
			asset, err := a.bangumiArtwork(ctx, k, true)
			if ctx.Err() != nil {
				break
			}
			a.anime.mu.Lock()
			j.Completed++
			if errors.Is(err, artwork.ErrMissing) {
				j.Missing++
			} else if err != nil || asset.State == "failed" {
				j.Failed++
			}
			a.anime.mu.Unlock()
		}
		a.anime.mu.Lock()
		j.Running = false
		j.Status = "success"
		var finish error
		if ctx.Err() != nil {
			j.Status = "cancelled"
			finish = ctx.Err()
		} else if j.Failed > 0 {
			j.Status = "partial"
			finish = fmt.Errorf("artwork_failed: %d", j.Failed)
		}
		if finish != nil {
			j.Error = finish.Error()
		}
		a.anime.mu.Unlock()
		a.finishTask(j.ID, finish)
	}()
	c.JSON(202, gin.H{"task_id": j.ID})
}

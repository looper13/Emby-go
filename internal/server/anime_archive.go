package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/anime/artwork"
	"emby-go/internal/anime/catalog"
	"emby-go/internal/scheduler"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

type archiveJob struct {
	ID        int64  `json:"id"`
	Running   bool   `json:"running"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	StartedAt string `json:"started_at"`
	UpToDate  bool   `json:"up_to_date"`
	catalog.Progress
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

func (a *App) archiveProgress(c *gin.Context) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	state, err := a.db.AnimeSync("archive")
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"snapshot": a.archive.Snapshot(), "job": a.archiveJob, "state": state})
}
func (a *App) archiveStart(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	job, err := a.startArchive()
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(202, gin.H{"task_id": job.ID})
}
func (a *App) archiveCancel(c *gin.Context) {
	a.anime.mu.Lock()
	if j := a.archiveJob; j != nil && j.Running {
		j.cancel()
	}
	a.anime.mu.Unlock()
	c.Status(204)
}

func (a *App) startArchive() (*archiveJob, error) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	if err := a.rootCtx.Err(); err != nil {
		return nil, err
	}
	if a.archiveJob != nil && a.archiveJob.Running {
		return a.archiveJob, nil
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil {
		return nil, err
	}
	if !lib.Enabled {
		return nil, scheduler.ErrBusy
	}
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.rootCtx, 45*time.Minute)
	j := &archiveJob{ID: a.startTask("anime_archive"), Running: true, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339), Progress: catalog.Progress{Phase: "manifest"}, done: make(chan struct{}), cancel: cancel}
	if err = a.db.SetAnimeSyncState("archive", cfg.Revision, "running", ""); err != nil {
		cancel()
		a.finishTask(j.ID, err)
		return nil, err
	}
	a.archiveJob = j
	update := func(p catalog.Progress) { a.anime.mu.Lock(); j.Progress = p; a.anime.mu.Unlock() }
	commit := func(fn func() error) error {
		a.anime.mu.Lock()
		defer a.anime.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		current, e := a.db.AnimeSettings()
		if e != nil {
			return e
		}
		l, e := a.db.AnimeLibrary()
		if e != nil {
			return e
		}
		if current.Revision != cfg.Revision || l.Revision != lib.Revision || !l.Enabled {
			return store.ErrConflict
		}
		return fn()
	}
	a.anime.wg.Add(1)
	go func() {
		defer a.anime.wg.Done()
		defer cancel()
		upToDate := false
		err := func() error {
			client := a.archiveClient
			manifest, err := catalog.Latest(ctx, client)
			if err != nil {
				return err
			}
			upToDate = a.archive.Snapshot().Manifest.Digest == manifest.Digest
			if !upToDate {
				dir := filepath.Join(filepath.Dir(a.configSnapshot().DBPath), "anime-catalog")
				p, err := catalog.Download(ctx, client, dir, manifest, update)
				if err != nil {
					return err
				}
				defer os.Remove(p)
				// Activation is gated separately; its progress callback must not reenter
				// the application's configuration lock held by commit.
				_, err = a.archive.Import(ctx, p, manifest, func(p catalog.Progress) {
					if p.Phase != "activate" {
						update(p)
					}
				}, commit)
				if err != nil {
					return err
				}
			}
			if err = commit(a.db.AnimeArchiveSchedule); err != nil {
				return err
			}
			return a.sched.Reload()
		}()
		a.anime.mu.Lock()
		j.Running = false
		j.err = err
		j.Status = "success"
		j.UpToDate = err == nil && upToDate
		if err != nil {
			j.Error = err.Error()
			j.Status = "failed"
			if errors.Is(err, context.Canceled) {
				j.Status = "cancelled"
				j.Error = ""
			}
		} else {
			j.Phase = "done"
		}
		if current, e := a.db.AnimeSettings(); e == nil && current.Revision == cfg.Revision {
			_ = a.db.SetAnimeSyncState("archive", cfg.Revision, j.Status, j.Error)
		}
		close(j.done)
		a.anime.mu.Unlock()
		a.finishTask(j.ID, err)
	}()
	return j, nil
}

func (a *App) bangumiCatalog(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	year, e := strconv.Atoi(c.DefaultQuery("year", "0"))
	season, e2 := strconv.Atoi(c.DefaultQuery("season", "0"))
	if e != nil || e2 != nil || year < 0 || year > time.Now().Year()+1 || season < 0 || season > 4 {
		c.JSON(400, gin.H{"error": "年份或首播季度无效"})
		return
	}
	limit, offset := animePage(c)
	f := catalog.Filter{Search: c.Query("search"), Year: year, Season: season, Limit: limit, Offset: offset}
	if len([]rune(f.Search)) > 100 {
		c.JSON(400, gin.H{"error": "搜索词过长"})
		return
	}
	items, total, err := a.archive.Search(c.Request.Context(), f)
	if errors.Is(err, catalog.ErrNotReady) {
		items = []catalog.Record{}
		total = 0
		err = nil
	}
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "snapshot": a.archive.Snapshot()})
}

func (a *App) loadBangumiSubject(ctx context.Context, id int64) (anime.Subject, error) {
	r, eps, snap, err := a.archive.ReadSubject(ctx, id)
	if err != nil {
		return a.db.AnimeSubjectByBangumiID(id)
	}
	sid, err := a.db.ImportAnimeSubject(ctx, r.Subject, eps, snap.Manifest.Digest, snap.Manifest.CreatedAt)
	if err != nil {
		return anime.Subject{}, err
	}
	return a.db.AnimeSubject(sid)
}

func (a *App) bangumiSubject(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	sub, err := a.loadBangumiSubject(c.Request.Context(), id)
	if err != nil {
		animeError(c, err)
		return
	}
	eps, err := a.db.AnimeEpisodes(sub.ID)
	if err != nil {
		animeError(c, err)
		return
	}
	items := []gin.H{}
	mappings, err := a.db.AnimeTMDBEpisodes(sub.ID)
	if err != nil {
		animeError(c, err)
		return
	}
	series, err := a.db.AnimeTMDBSeries(sub.ID)
	if err != nil {
		animeError(c, err)
		return
	}
	for _, ep := range eps {
		mapping := mappings[ep.ID]
		if mapping.Stills == nil {
			mapping.Stills = []string{}
		}
		mapping.EpisodeID = ep.ID
		items = append(items, gin.H{"episode": ep, "binding": a.animeBindingView(sub.ID, ep.ID), "item_id": anime.ID("episode", ep.ID), "tmdb": mapping})
	}
	entries, err := a.db.AnimeSubjectEntries(sub.ID)
	if err != nil {
		animeError(c, err)
		return
	}
	people, err := a.archive.People(c.Request.Context(), id)
	if err != nil {
		people = []catalog.Person{}
	}
	var binding *anime.Binding
	if sub.IsMovie() {
		b := a.animeBindingView(sub.ID, 0)
		binding = &b
	}
	c.JSON(200, gin.H{"subject": sub, "episodes": items, "entries": entries, "people": people, "binding": binding, "tmdb": series})
}

func (a *App) refreshBangumiSubject(c *gin.Context) {
	if !a.animeEnabled(c) {
		return
	}
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	if _, err := a.loadBangumiSubject(c.Request.Context(), id); err != nil {
		animeError(c, err)
		return
	}
	j, err := a.startAnimeSync(fmt.Sprintf("subject:%d", id), func(ctx context.Context, client *anime.Client, commit func(func() error) error) error {
		sub, eps, err := client.Subject(ctx, id)
		if err != nil {
			return err
		}
		return commit(func() error { _, err := a.db.SaveAnimeSubject(ctx, sub, eps); return err })
	})
	if err != nil {
		animeError(c, err)
		return
	}
	c.JSON(202, gin.H{"task_id": j.ID})
}

func (a *App) animeSubjectImage(c *gin.Context) {
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil || !lib.Enabled {
		c.Status(404)
		return
	}
	if !a.artworkOwner(c.Request.Context(), "subject", id) {
		c.Status(404)
		return
	}
	a.serveBangumiArtwork(c, artwork.Key{Kind: "subject", ID: id, Variant: c.DefaultQuery("size", "common")})
}

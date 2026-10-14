package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/scheduler"
	"emby-go/internal/store"
)

type animeJob struct {
	ID        int64              `json:"id"`
	Scope     string             `json:"scope"`
	Running   bool               `json:"running"`
	Error     string             `json:"error"`
	Commits   int                `json:"commits"`
	StartedAt string             `json:"started_at"`
	ElapsedMS int64              `json:"elapsed_ms"`
	Requests  anime.RequestStats `json:"requests"`
	done      chan struct{}
	cancel    context.CancelFunc
	err       error
}
type animeRuntime struct {
	checks       map[string]*animeCheckFlight
	covers       map[string]animeCover
	coverFlights map[string]*animeCoverFlight
	mu           sync.Mutex
	wg           sync.WaitGroup
	client       *anime.Client
	tmdb         *anime.TMDBClient
	job          *animeJob
}

func (a *App) animeSourceLocked(cfg anime.Settings) *anime.Client {
	if a.anime.client == nil || a.anime.client.MikanURL != cfg.MikanURL {
		a.anime.client = anime.NewClient(cfg.MikanURL)
		a.anime.client.Context = a.rootCtx
	}
	return a.anime.client
}

// The single bounded sync worker joins repeated requests for the same scope.
// Its context belongs to the application, so leaving a page cannot cancel other
// readers' shared refresh. Explicit cancel and shutdown still stop all I/O.
func (a *App) startAnimeSync(scope string, work func(context.Context, *anime.Client, func(func() error) error) error) (*animeJob, error) {
	a.anime.mu.Lock()
	defer a.anime.mu.Unlock()
	if err := a.rootCtx.Err(); err != nil {
		return nil, err
	}
	if job := a.anime.job; job != nil && job.Running {
		if job.Scope == scope {
			return job, nil
		}
		return nil, scheduler.ErrBusy
	}
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		return nil, err
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil {
		return nil, err
	}
	if !lib.Enabled {
		return nil, errors.New("动漫库已关闭")
	}
	client := a.animeSourceLocked(cfg)
	ctx, cancel := context.WithTimeout(a.rootCtx, 3*time.Minute)
	ctx, metrics := anime.MeasureRequests(ctx)
	started := time.Now()
	job := &animeJob{ID: a.startTask("anime_sync"), Scope: scope, Running: true, StartedAt: started.UTC().Format(time.RFC3339), done: make(chan struct{}), cancel: cancel}
	a.anime.job = job
	if err = a.db.SetAnimeSyncState(scope, cfg.Revision, "running", ""); err != nil {
		cancel()
		job.Running = false
		a.finishTask(job.ID, err)
		return nil, err
	}
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
		library, e := a.db.AnimeLibrary()
		if e != nil {
			return e
		}
		if current.Revision != cfg.Revision || library.Revision != lib.Revision || !library.Enabled {
			return store.ErrConflict
		}
		if err := fn(); err != nil {
			return err
		}
		job.Commits++
		return nil
	}
	a.anime.wg.Add(1)
	go func() {
		defer a.anime.wg.Done()
		defer cancel()
		err := work(ctx, client, commit)
		a.anime.mu.Lock()
		status, message := "success", ""
		if err != nil {
			status, message = "failed", err.Error()
			if job.Commits > 0 {
				status = "partial"
			}
			if errors.Is(err, context.Canceled) {
				status = "cancelled"
			}
		}
		// A changed configuration owns a different generation; never attach an old
		// response or error to the new source's status.
		if current, e := a.db.AnimeSettings(); e == nil && current.Revision == cfg.Revision {
			_ = a.db.SetAnimeSyncState(scope, cfg.Revision, status, message)
		}
		job.Running, job.Error, job.err = false, message, err
		job.ElapsedMS, job.Requests = time.Since(started).Milliseconds(), metrics.Snapshot()
		slog.Info("动漫资料同步结束", "task_id", job.ID, "scope", scope, "source", "mikan/bangumi", "status", status, "elapsed_ms", job.ElapsedMS, "commits", job.Commits, "requests", job.Requests.Requests, "successes", job.Requests.Successes, "failures", job.Requests.Failures, "cache_hits", job.Requests.CacheHits)
		close(job.done)
		a.anime.mu.Unlock()
		a.finishTask(job.ID, err)
	}()
	return job, nil
}

func (a *App) syncAnimeSeason(year, season int) (*animeJob, error) {
	return a.startAnimeSync(fmt.Sprintf("catalog:%d:%d", year, season), func(ctx context.Context, client *anime.Client, commit func(func() error) error) error {
		entries, err := client.Season(ctx, year, season)
		if err != nil {
			return err
		}
		if err = commit(func() error { return a.db.SaveAnimeCatalog(ctx, entries) }); err != nil {
			return err
		}
		if err = commit(a.db.EnsureAnimeSchedule); err != nil {
			return err
		}
		if a.sched != nil {
			return a.sched.Reload()
		}
		return nil
	})
}

func (a *App) syncAnimeEntry(id int64) (*animeJob, error) {
	return a.startAnimeEntry(id, true)
}
func (a *App) syncAnimeEntryAuto(id int64) (*animeJob, error) {
	return a.startAnimeEntry(id, false)
}
func (a *App) startAnimeEntry(id int64, force bool) (*animeJob, error) {
	return a.startAnimeSync(fmt.Sprintf("entry:%d", id), func(ctx context.Context, client *anime.Client, commit func(func() error) error) error {
		entry, htmlReleases, err := client.MikanDetail(ctx, id)
		if err != nil {
			return err
		}
		// Keep the successful Mikan snapshot even if RSS or Bangumi is down.
		if err = commit(func() error { return a.db.SaveAnimeEntry(ctx, entry, htmlReleases) }); err != nil {
			return err
		}
		var warnings []error
		rss, e := client.Releases(ctx, id)
		if e != nil {
			warnings = append(warnings, e)
		} else if e = commit(func() error { return a.db.SaveAnimeEntry(ctx, entry, rss) }); e != nil {
			return e
		}
		current, err := a.db.AnimeEntry(id)
		if err != nil {
			return err
		}
		bgmID := entry.BangumiID
		if current.Manual {
			sub, e := a.db.AnimeSubject(current.SubjectID)
			if e != nil {
				return e
			}
			bgmID = sub.BangumiID
		}
		if bgmID > 0 {
			if !force && current.SubjectID > 0 {
				cached, e := a.db.AnimeSubject(current.SubjectID)
				when, _ := time.Parse(time.RFC3339, cached.UpdatedAt)
				ttl := 24 * time.Hour
				now := time.Now()
				if current.Year < now.Year() || current.Year == now.Year() && current.Season < (int(now.Month())-1)/3+1 {
					ttl = 7 * 24 * time.Hour
				}
				if e == nil && cached.BangumiID == bgmID && time.Since(when) < ttl {
					anime.RecordCacheHit(ctx)
					return errors.Join(warnings...)
				}
			}
			sub, eps, e := client.Subject(ctx, bgmID)
			if e != nil {
				warnings = append(warnings, e)
			} else {
				e = commit(func() error {
					sid, e := a.db.SaveAnimeSubject(ctx, sub, eps)
					if e != nil {
						return e
					}
					latest, e := a.db.AnimeEntry(id)
					if e != nil {
						return e
					}
					if latest.Manual {
						return nil
					}
					return a.db.MapAnimeEntry(id, sid, latest.Revision, false)
				})
				if e != nil {
					return e
				}
			}
		}
		return errors.Join(warnings...)
	})
}

func (a *App) animeNeedsRefresh(scope string, ttl time.Duration) bool {
	state, err := a.db.AnimeSync(scope)
	if err != nil {
		return false
	}
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		return false
	}
	if state.Generation != cfg.Revision {
		return true
	}
	when, _ := time.Parse(time.RFC3339, state.UpdatedAt)
	if state.Status == "failed" || state.Status == "partial" || state.Status == "cancelled" {
		return time.Since(when) > 5*time.Minute
	}
	return state.Status != "running" && time.Since(when) > ttl
}

func (a *App) runScheduledAnime(ctx context.Context) error {
	lib, err := a.db.AnimeLibrary()
	if err != nil {
		return err
	}
	if !lib.Enabled {
		return scheduler.ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	wait := func(job *animeJob) error {
		select {
		case <-ctx.Done():
			job.cancel()
			return ctx.Err()
		case <-job.done:
			return job.err
		}
	}
	now := time.Now()
	job, err := a.syncAnimeSeason(now.Year(), (int(now.Month())-1)/3+1)
	if err != nil {
		return err
	}
	if err = wait(job); err != nil {
		return err
	}
	ids, err := a.db.AnimeRefreshEntries(now.Year(), (int(now.Month())-1)/3+1)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !a.animeNeedsRefresh(fmt.Sprintf("entry:%d", id), 30*time.Minute) {
			continue
		}
		job, err = a.syncAnimeEntryAuto(id)
		if err != nil {
			return err
		}
		if err = wait(job); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

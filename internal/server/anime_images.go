package server

import (
	"context"
	"fmt"
	"time"

	"emby-go/internal/anime"
	"github.com/gin-gonic/gin"
)

type animeCover struct {
	data                 []byte
	mime, url, failedURL string
	fetched, retryAt     time.Time
}
type animeCoverFlight struct {
	done   chan struct{}
	result animeCover
}

// Request coalescing, four active fetches and 32 cached images bound memory and
// network work. A failed replacement keeps the last good image for five minutes.
func (a *App) serveAnimeCover(c *gin.Context, key, rawURL string) bool {
	if rawURL == "" {
		return false
	}
	a.anime.mu.Lock()
	if a.rootCtx.Err() != nil {
		a.anime.mu.Unlock()
		return false
	}
	old := a.anime.covers[key]
	if (old.url == rawURL && time.Since(old.fetched) < 6*time.Hour) || (old.failedURL == rawURL && time.Now().Before(old.retryAt)) {
		a.anime.mu.Unlock()
		return writeAnimeCover(c, old)
	}
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		a.anime.mu.Unlock()
		return writeAnimeCover(c, old)
	}
	flightKey := fmt.Sprintf("%s:%d:%s", key, cfg.Revision, anime.SourceVersion(rawURL))
	flight := a.anime.coverFlights[flightKey]
	if flight == nil {
		if len(a.anime.coverFlights) >= 4 {
			a.anime.mu.Unlock()
			return writeAnimeCover(c, old)
		}
		if a.anime.coverFlights == nil {
			a.anime.coverFlights = map[string]*animeCoverFlight{}
		}
		flight = &animeCoverFlight{done: make(chan struct{})}
		a.anime.coverFlights[flightKey] = flight
		client := a.animeSourceLocked(cfg)
		a.anime.wg.Add(1)
		go func() {
			defer a.anime.wg.Done()
			ctx, cancel := context.WithTimeout(a.rootCtx, 15*time.Second)
			defer cancel()
			data, mime, err := client.Cover(ctx, rawURL)
			result := old
			if err == nil {
				result = animeCover{data: data, mime: mime, url: rawURL, fetched: time.Now()}
			} else {
				result.failedURL = rawURL
				result.retryAt = time.Now().Add(5 * time.Minute)
			}
			a.anime.mu.Lock()
			defer a.anime.mu.Unlock()
			current, e := a.db.AnimeSettings()
			lib, le := a.db.AnimeLibrary()
			if e == nil && le == nil && lib.Enabled && current.Revision == cfg.Revision && a.rootCtx.Err() == nil {
				if a.anime.covers == nil {
					a.anime.covers = map[string]animeCover{}
				}
				if _, exists := a.anime.covers[key]; !exists && len(a.anime.covers) >= 32 {
					oldestKey := ""
					oldest := time.Now()
					for k, v := range a.anime.covers {
						if v.fetched.Before(oldest) {
							oldest, oldestKey = v.fetched, k
						}
					}
					delete(a.anime.covers, oldestKey)
				}
				a.anime.covers[key] = result
				flight.result = result
			}
			delete(a.anime.coverFlights, flightKey)
			close(flight.done)
		}()
	}
	a.anime.mu.Unlock()
	select {
	case <-c.Request.Context().Done():
		c.Status(499)
		return true
	case <-flight.done:
		return writeAnimeCover(c, flight.result)
	}
}
func writeAnimeCover(c *gin.Context, cover animeCover) bool {
	if len(cover.data) == 0 {
		return false
	}
	setPrivateImageCache(c)
	c.Header("X-Content-Type-Options", "nosniff")
	tag := `"` + anime.SourceVersion(string(cover.data)) + `"`
	c.Header("ETag", tag)
	if c.GetHeader("If-None-Match") == tag {
		c.Status(304)
	} else {
		c.Data(200, cover.mime, cover.data)
	}
	return true
}

func (a *App) animeEntryImage(c *gin.Context) {
	id, ok := animeEntryID(c)
	if !ok {
		return
	}
	l, err := a.db.AnimeLibrary()
	if err != nil || !l.Enabled {
		c.Status(404)
		return
	}
	entry, err := a.db.AnimeEntry(id)
	if err != nil {
		c.Status(404)
		return
	}
	raw := entry.Image
	if entry.SubjectID > 0 {
		if sub, e := a.db.AnimeSubject(entry.SubjectID); e == nil && sub.Image != "" {
			raw = sub.Image
		}
	}
	if !a.serveAnimeCover(c, fmt.Sprintf("entry:%d", id), raw) {
		c.Status(404)
	}
}

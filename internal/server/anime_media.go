package server

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/scanner"
	"emby-go/internal/scheduler"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func (a *App) prepareAnimePlayback(c *gin.Context) bool {
	if !strings.HasPrefix(c.Param("id"), "anime-") {
		return true
	}
	if (c.Param("uid") != "" && c.Param("uid") != "1") || (c.Query("UserId") != "" && c.Query("UserId") != "1") {
		c.JSON(404, gin.H{"error": "user not found"})
		return false
	}
	if err := a.checkAnimeMedia(c.Request.Context(), c.Param("id"), false); err != nil {
		c.JSON(404, gin.H{"error": "动漫媒体尚未关联或资源不可用"})
		return false
	}
	return true
}

func (a *App) animeMedia(rawID string) (store.Movie, anime.Binding, string, error) {
	var m store.Movie
	l, err := a.db.AnimeLibrary()
	if err != nil || !l.Enabled {
		return m, anime.Binding{}, "", sql.ErrNoRows
	}
	_, _, b, err := a.db.AnimeTarget(rawID)
	if err != nil || b.MovieID == 0 {
		return m, b, "", sql.ErrNoRows
	}
	m, err = a.db.Movie(b.MovieID)
	if err != nil || !m.IsVisible() || len(m.AdditionalParts) != 0 || !strings.EqualFold(filepath.Ext(m.SourcePath), ".strm") {
		return m, b, "", sql.ErrNoRows
	}
	if err = a.db.RequireDirectory(m.LibraryID); err != nil {
		return m, b, "", err
	}
	source, err := readAnimeSource(m.SourcePath)
	if err != nil || !scanner.ValidHTTP(source) {
		return m, b, "", sql.ErrNoRows
	}
	return m, b, source, nil
}

// Listing uses the last successful check for the unchanged source. Playback
// repeats expired checks; expiration alone never clears a client's whole library.
func (a *App) animePlayable(rawID string) (store.Movie, bool) {
	m, b, source, err := a.animeMedia(rawID)
	return m, err == nil && b.Status == "playable" && b.SourceVersion == anime.SourceVersion(source)
}

type animeCheckFlight struct {
	done chan struct{}
	err  error
}

func readAnimeSource(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", errors.New("invalid_strm_size")
	}
	scan := bufio.NewScanner(file)
	var source string
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		if source != "" {
			return "", errors.New("multiple_strm_sources")
		}
		source = line
	}
	if err = scan.Err(); err != nil {
		return "", err
	}
	if !scanner.ValidHTTP(source) {
		return "", errors.New("invalid_strm_source")
	}
	return source, nil
}

func (a *App) checkAnimeMedia(ctx context.Context, rawID string, force bool) error {
	a.anime.mu.Lock()
	if err := a.rootCtx.Err(); err != nil {
		a.anime.mu.Unlock()
		return err
	}
	flight := a.anime.checks[rawID]
	if flight == nil {
		if len(a.anime.checks) >= 4 {
			a.anime.mu.Unlock()
			return scheduler.ErrBusy
		}
		flight = &animeCheckFlight{done: make(chan struct{})}
		if a.anime.checks == nil {
			a.anime.checks = map[string]*animeCheckFlight{}
		}
		a.anime.checks[rawID] = flight
		a.anime.wg.Add(1)
		go func() {
			defer a.anime.wg.Done()
			err := a.checkAnimeMediaNow(a.rootCtx, rawID, force)
			a.anime.mu.Lock()
			flight.err = err
			delete(a.anime.checks, rawID)
			close(flight.done)
			a.anime.mu.Unlock()
		}()
	}
	a.anime.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-flight.done:
		return flight.err
	}
}

func (a *App) checkAnimeMediaNow(ctx context.Context, rawID string, force bool) error {
	_, b, source, err := a.animeMedia(rawID)
	if err != nil {
		return err
	}
	version := anime.SourceVersion(source)
	checked, _ := time.Parse(time.RFC3339, b.CheckedAt)
	if !force && b.Status == "playable" && b.SourceVersion == version && time.Since(checked) < anime.AvailabilityTTL {
		return nil
	}
	err = anime.CheckMedia(ctx, source)
	// Cancellation should not invalidate a previously good source.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, message := "playable", ""
	if err != nil {
		status, message = "unavailable", err.Error()
	}
	// Re-read after I/O; both replacement and STRM edits invalidate this response.
	_, current, latest, readErr := a.animeMedia(rawID)
	if readErr != nil || current.Revision != b.Revision || current.MovieID != b.MovieID || latest != source {
		return store.ErrConflict
	}
	if saveErr := a.db.SetAnimeAvailability(b, status, version, message); saveErr != nil {
		return saveErr
	}
	return err
}

func (a *App) resolveUserMedia(raw string) (store.Movie, error) {
	if strings.HasPrefix(raw, "anime-") {
		m, _, _, err := a.animeMedia(raw)
		return m, err
	}
	m, _, _, ok := a.resolvePlaybackTarget(raw)
	if !ok || !m.IsVisible() {
		return store.Movie{}, sql.ErrNoRows
	}
	return m, nil
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/anime"
	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/store"
)

type animeTransport func(*http.Request) (*http.Response, error)

func (f animeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func animeFixtureApp(t *testing.T) (*App, *atomic.Int64, *atomic.Bool) {
	t.Helper()
	a, err := newApp(config.Config{DBPath: filepath.Join(t.TempDir(), "db")}, cache.NewMemory(64))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if err = a.db.InitializeAdministrator("admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	count := &atomic.Int64{}
	fail := &atomic.Bool{}
	client := anime.NewClient(anime.DefaultMikanURL)
	client.HTTP = &http.Client{Transport: animeTransport(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		path := ""
		status := 200
		switch {
		case strings.Contains(r.URL.Path, "BangumiCoverFlow"):
			path = "mikan/home-season.html"
		case r.URL.Path == "/Home/Bangumi/4063":
			path = "mikan/bangumi-page-tv-4063.html"
		case r.URL.Path == "/RSS/Bangumi":
			path = "mikan/rss-bangumi-4063.xml"
		case r.URL.Path == "/v0/subjects/418229":
			path = "bangumi/subject-tv-418229.json"
		case r.URL.Path == "/v0/episodes":
			path = "bangumi/episodes-tv-418229-p1.json"
			if r.URL.Query().Get("offset") != "0" {
				path = "bangumi/episodes-tv-418229-p2.json"
			}
		default:
			status = 404
		}
		if fail.Load() {
			status = 403
		}
		data := []byte("denied")
		if status == 200 {
			var e error
			data, e = os.ReadFile(filepath.Join("../anime/testdata", path))
			if e != nil {
				return nil, e
			}
			if r.URL.Path == "/v0/episodes" {
				// The captured pages contain four of twelve real episodes. This
				// synthetic envelope deliberately tests a complete four-item feed.
				var page map[string]any
				if e = json.Unmarshal(data, &page); e != nil {
					return nil, e
				}
				page["total"] = 4
				data, e = json.Marshal(page)
				if e != nil {
					return nil, e
				}
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header), Request: r}, nil
	})}
	a.anime.client = client
	return a, count, fail
}
func awaitAnimeJob(t *testing.T, j *animeJob) error {
	t.Helper()
	select {
	case <-j.done:
		return j.err
	case <-time.After(15 * time.Second):
		t.Fatal("anime job did not finish")
		return nil
	}
}

func TestAnimeSyncFixturesFailureAndManualPriority(t *testing.T) {
	a, count, fail := animeFixtureApp(t)
	j, err := a.syncAnimeSeason(2026, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		same, e := a.syncAnimeSeason(2026, 4)
		if e != nil || same.ID != j.ID {
			t.Fatal("refresh was not coalesced", e)
		}
	}
	if err = awaitAnimeJob(t, j); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 1 {
		t.Fatal("duplicate source calls", count.Load())
	}
	if j.Requests.Requests != 1 || j.Requests.Successes != 1 || j.Requests.Failures != 0 || j.Commits == 0 {
		t.Fatal("incorrect task request metrics", j)
	}
	entries, total, err := a.db.AnimeCatalog(2026, 4, -1, "", 50, 0)
	if err != nil || total < 10 || len(entries) != total {
		t.Fatal(total, err)
	}
	tasks, err := a.db.ScheduledTasks()
	if err != nil || len(tasks) != 1 || tasks[0].Type != "anime_sync" {
		t.Fatal(tasks, err)
	}
	if err = a.db.DeleteScheduledTask(tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	j, err = a.syncAnimeEntry(4063)
	if err != nil {
		t.Fatal(err)
	}
	if err = awaitAnimeJob(t, j); err != nil {
		t.Fatal(err)
	}
	entry, err := a.db.AnimeEntry(4063)
	if err != nil || entry.SubjectID == 0 {
		t.Fatal(entry, err)
	}
	eps, err := a.db.AnimeEpisodes(entry.SubjectID)
	if err != nil || len(eps) != 4 {
		t.Fatal(eps, err)
	}
	_, releaseCount, err := a.db.AnimeReleases(4063, 100, 0, "")
	if err != nil || releaseCount == 0 {
		t.Fatal(releaseCount, err)
	}
	other, err := a.db.SaveAnimeSubject(context.Background(), anime.Subject{BangumiID: 9999, Name: "Manual", Platform: "TV"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.MapAnimeEntry(4063, other, entry.Revision, true); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	j, err = a.syncAnimeSeason(2026, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = awaitAnimeJob(t, j); err == nil {
		t.Fatal("failure reported success")
	}
	_, still, _ := a.db.AnimeCatalog(2026, 4, -1, "", 50, 0)
	if still != total {
		t.Fatal("failure erased snapshot")
	}
	entry, err = a.db.AnimeEntry(4063)
	if err != nil || !entry.Manual || entry.SubjectID != other {
		t.Fatal("lost manual mapping", entry, err)
	}
	fail.Store(false)
	j, err = a.syncAnimeSeason(2026, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = awaitAnimeJob(t, j); err != nil {
		t.Fatal(err)
	}
	tasks, _ = a.db.ScheduledTasks()
	if len(tasks) != 0 {
		t.Fatal("deleted schedule recreated")
	}
	entry, _ = a.db.AnimeEntry(4063)
	if entry.SubjectID != other || !entry.Manual {
		t.Fatal("catalog refresh replaced mapping")
	}
}

func TestAnimeOldGenerationCannotCommit(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	committed := false
	j, err := a.startAnimeSync("generation-test", func(ctx context.Context, _ *anime.Client, commit func(func() error) error) error {
		close(entered)
		<-release
		return commit(func() error { committed = true; return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	lib, _ := a.db.AnimeLibrary()
	cfg, _ := a.db.AnimeSettings()
	cfg.MikanURL = "https://example.com"
	a.anime.mu.Lock()
	_, err = a.db.SaveAnimeSettings(cfg, lib.Name, lib.Enabled, lib.Revision)
	a.anime.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = awaitAnimeJob(t, j); !errors.Is(err, store.ErrConflict) || committed {
		t.Fatal("old generation committed", err)
	}
}

func TestAnimeIncompletePagesPreserveSubject(t *testing.T) {
	a, _, _ := animeFixtureApp(t)
	j, err := a.syncAnimeEntry(4063)
	if err != nil {
		t.Fatal(err)
	}
	if err = awaitAnimeJob(t, j); err != nil {
		t.Fatal(err)
	}
	entry, _ := a.db.AnimeEntry(4063)
	before, _ := a.db.AnimeEpisodes(entry.SubjectID)
	transport := a.anime.client.HTTP.Transport
	a.anime.client.HTTP.Transport = animeTransport(func(r *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(r)
		if err != nil || r.URL.Path != "/v0/episodes" {
			return resp, err
		}
		defer resp.Body.Close()
		var page map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, err
		}
		page["total"] = 12 // Captured pages end after episode four; subsequent offset repeats.
		data, _ := json.Marshal(page)
		resp.Body = io.NopCloser(strings.NewReader(string(data)))
		return resp, nil
	})
	j, err = a.syncAnimeEntry(4063)
	if err != nil {
		t.Fatal(err)
	}
	if err = awaitAnimeJob(t, j); err == nil {
		t.Fatal("incomplete pagination accepted")
	}
	after, _ := a.db.AnimeEpisodes(entry.SubjectID)
	if len(after) != len(before) || after[0].ID != before[0].ID {
		t.Fatal("partial list replaced committed episodes")
	}
	state, _ := a.db.AnimeSync("entry:4063")
	if state.Status != "partial" || state.LastSuccessAt == "" || j.Commits == 0 {
		t.Fatal("lost last successful snapshot time", state)
	}
}

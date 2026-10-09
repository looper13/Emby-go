package server

import (
	"context"
	"emby-go/internal/scraper"
	"emby-go/internal/store"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentScrapesDoNotReleaseGate(t *testing.T) {
	app, ts, token := contractEnv(t)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer backend.Close()
	app.db.SetSetting(settingMetatubeURL, backend.URL)
	app.db.SetSetting(settingMetatubeToken, "test")
	movies, _, err := app.db.SearchAll(0, "", "", "title", false, 10, 0)
	if err != nil || len(movies) == 0 {
		t.Fatalf("setup: %v", err)
	}
	if !app.beginScrape("scrape", 2) || !app.claimNFO("scrape") {
		t.Fatal("setup gate")
	}
	defer app.endScrape(nil)
	defer app.releaseNFO("scrape")
	response, _ := embyRaw(t, ts, "POST", "/api/admin/items/"+strconv.FormatInt(movies[0].ID, 10)+"/scrape", token, `{"provider":"fanza","id":"a"}`)
	probeEntered := app.claimNFO("probe")
	if probeEntered {
		app.releaseNFO("probe")
	}
	if response.StatusCode != 409 || probeEntered {
		t.Fatalf("single request status=%d; other task admitted while batch still running=%v", response.StatusCode, probeEntered)
	}
}
func TestInterruptedScrapeRefreshesSuccessfulMovies(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel", false: "circuit_breaker"}[cancel], func(t *testing.T) { testInterruptedScrape(t, cancel) })
	}
}

func testInterruptedScrape(t *testing.T, cancel bool) {
	t.Helper()
	app, _, _ := contractEnv(t)
	movies, _, err := app.db.SearchAll(0, "", "", "title", false, 10, 0)
	if err != nil || len(movies) == 0 {
		t.Fatal(err)
	}
	first := movies[0]
	count := 1
	if !cancel {
		count = scraper.MaxConsecutiveFailures + 1
	}
	for index := 0; index < count; index++ {
		number := fmt.Sprintf("ABF-%03d", 19+index)
		other := filepath.Join(filepath.Dir(first.SourcePath), number+".strm")
		writeFile(t, other, "http://media.test/other.mp4")
		if _, err := app.db.UpsertMovie(store.Movie{LibraryID: first.LibraryID, SourcePath: other, Status: "pending", Number: number, OutputDir: filepath.Dir(other)}, 30, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var searches atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/search") {
			if searches.Add(1) > 1 {
				if cancel {
					app.scrapeCancel()
				}
				w.WriteHeader(503)
				return
			}
			w.Write([]byte(`{"data":[{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated"}]}`))
			return
		}
		w.Write([]byte(`{"data":{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated"}}`))
	}))
	defer backend.Close()
	if !app.beginScrape("scrape", count+1) || !app.claimNFO("scrape") {
		t.Fatal("gate setup")
	}
	err = app.executeScrape(scraper.Config{BaseURL: backend.URL, Token: "test", Concurrency: 1}, scraper.RunOptions{Overwrite: true})
	if err == nil || (cancel && app.scrapeContext().Err() != context.Canceled) || (!cancel && !strings.Contains(err.Error(), "连续 20")) {
		t.Fatalf("expected interrupted task: %v", err)
	}
	raw, err := os.ReadFile(first.NFOPath)
	if err != nil || !strings.Contains(string(raw), "Updated") {
		t.Fatalf("first success missing on disk: %v", err)
	}
	indexed, err := app.db.Movie(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexed.Title, "Updated") {
		t.Fatalf("successful NFO written, DB title still %q after cancellation", indexed.Title)
	}
	if app.scrapeStatus.Cancelled != cancel || app.scraping() || app.nfoBusyOwner() != "" {
		t.Fatal("interrupted task did not finish and release the gate")
	}
}

func TestSingleMovieMutationsRespectNFOGate(t *testing.T) {
	app, ts, token := contractEnv(t)
	movies, _, err := app.db.SearchAll(0, "", "", "title", false, 10, 0)
	if err != nil || len(movies) != 1 {
		t.Fatalf("setup: %v", err)
	}
	id := strconv.FormatInt(movies[0].ID, 10)
	for _, request := range []struct{ method, path string }{
		{"PUT", "/api/admin/items/" + id},
		{"POST", "/api/admin/items/" + id + "/reread"},
		{"POST", "/api/admin/items/" + id + "/probe"},
		{"POST", "/api/admin/items/" + id + "/images/poster"},
		{"POST", "/api/admin/items/" + id + "/scrape"},
		{"POST", "/api/admin/items/manual"},
	} {
		if !app.claimNFO("scrape") {
			t.Fatal("gate setup")
		}
		resp, raw := embyRaw(t, ts, request.method, request.path, token, "{}")
		owner := app.nfoBusyOwner()
		app.releaseNFO("scrape")
		if resp.StatusCode != 409 || owner != "刮削" {
			t.Fatalf("%s: %d %s owner=%s", request.path, resp.StatusCode, raw, owner)
		}
	}
	// Invalid input must release its own claim as well.
	resp, _ := embyRaw(t, ts, "PUT", "/api/admin/items/"+id, token, "bad-json")
	if resp.StatusCode != 400 || app.nfoBusyOwner() != "" {
		t.Fatal("failed edit leaked the gate")
	}
}

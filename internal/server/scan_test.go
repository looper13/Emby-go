package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"emby-go/internal/scanner"
	"emby-go/internal/scheduler"
)

func TestIncrementalScanAPIAndScheduledModes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.strm"), "http://media.test/a.mp4\n")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>A</title></movie>")
	app, server, token := newProbeTestApp(t, root)
	libraries, err := app.db.Libraries()
	if err != nil || len(libraries) != 1 {
		t.Fatalf("libraries = %+v, %v", libraries, err)
	}
	library := libraries[0]
	otherRoot := t.TempDir()
	writeFile(t, filepath.Join(otherRoot, "b.strm"), "http://media.test/b.mp4\n")
	writeFile(t, filepath.Join(otherRoot, "b.nfo"), "<movie><title>B</title></movie>")
	other, err := app.db.AddLibrary("other", otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, want scanner.Result) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Emby-Token", token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result scanner.Result
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || result != want {
			t.Fatalf("%s: status=%d result=%+v want=%+v", path, response.StatusCode, result, want)
		}
	}
	version := app.db.Version("g:version")
	app.cache.Set("scan-test", []byte("keep"), 0)
	call("/api/admin/scan?library_id="+strconv.FormatInt(library.ID, 10), scanner.Result{Skipped: 1})
	if _, exists := app.cache.Get("scan-test"); !exists || app.db.Version("g:version") != version {
		t.Fatal("unchanged library scan invalidated cache")
	}
	call("/api/admin/scan", scanner.Result{Added: 1, Success: 1, Skipped: 1})
	if _, exists := app.cache.Get("scan-test"); !exists {
		t.Fatal("changed library scan discarded unrelated cache")
	}
	call("/api/admin/reindex", scanner.Result{Updated: 2, Success: 2})
	call("/api/admin/scan", scanner.Result{Skipped: 2})
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>Updated A</title></movie>")
	writeFile(t, filepath.Join(otherRoot, "b.nfo"), "<movie><title>Updated B</title></movie>")
	if err := app.runScheduledTask(context.Background(), scheduler.Task{Type: "scan", Params: map[string]any{"library_id": library.ID}}); err != nil {
		t.Fatal(err)
	}
	if app.scanStatus.Updated != 1 || app.scanStatus.Skipped != 0 {
		t.Fatalf("scheduled incremental update: %+v", app.scanStatus)
	}
	otherMovies, _, err := app.db.SearchAll(other.ID, "", "", "title", false, 10, 0)
	if err != nil || len(otherMovies) != 1 || otherMovies[0].Title != "B" {
		t.Fatalf("targeted scan touched other library: %+v %v", otherMovies, err)
	}
	if err := app.runScheduledTask(context.Background(), scheduler.Task{Type: "scan", Params: map[string]any{"library_id": library.ID}}); err != nil {
		t.Fatal(err)
	}
	if app.scanStatus.Updated != 0 || app.scanStatus.Skipped != 1 {
		t.Fatalf("scheduled unchanged scan: %+v", app.scanStatus)
	}
	if err := app.runScheduledTask(context.Background(), scheduler.Task{Type: "reindex"}); err != nil {
		t.Fatal(err)
	}
	if app.scanStatus.Libraries != 2 || app.scanStatus.Updated != 1 || app.scanStatus.Skipped != 0 {
		t.Fatalf("scheduled full rebuild: %+v", app.scanStatus)
	}
	call("/api/admin/scan", scanner.Result{Skipped: 2})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/scan/progress", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Emby-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var progress scanStatus
	if err := json.NewDecoder(response.Body).Decode(&progress); err != nil {
		t.Fatal(err)
	}
	if progress.Running || progress.Done != progress.Total || progress.Skipped != 1 || progress.Updated != 0 || progress.FinishedAt == "" {
		t.Fatalf("scan progress API: %+v", progress)
	}
}

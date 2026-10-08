package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/librarywatch"
	"emby-go/internal/store"
)

func awaitLibraryWatch(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for automatic library refresh")
}

func TestLibraryWatchEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.strm"), "http://media.test/a.mp4\n")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>A</title></movie>")
	app, server, token := newProbeTestApp(t, root)
	app.startLibraryMonitoring()
	awaitLibraryWatch(t, func() bool {
		app.taskMu.Lock()
		defer app.taskMu.Unlock()
		return len(app.tasks) > 0 && app.tasks[0].Type == "watch" && app.tasks[0].Status == "success"
	})
	find := func(title string) (store.Movie, bool) {
		movies, _, err := app.db.SearchAll(0, "", "", "title", false, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, movie := range movies {
			if movie.Title == title {
				return movie, true
			}
		}
		return store.Movie{}, false
	}
	version := app.db.Version("lib:1:version")
	for attempt := 0; attempt < 4; attempt++ {
		writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>Burst "+strconv.Itoa(attempt)+"</title></movie>")
	}
	awaitLibraryWatch(t, func() bool { _, found := find("Burst 3"); return found })
	awaitLibraryWatch(t, func() bool { return !app.scanning() })
	oldVersion, _ := strconv.Atoi(version)
	newVersion, _ := strconv.Atoi(app.db.Version("lib:1:version"))
	if newVersion != oldVersion+1 {
		t.Fatalf("burst should produce one index update: version %d -> %d", oldVersion, newVersion)
	}
	awaitLibraryWatch(t, func() bool { return app.claimNFO("watch-test") })
	defer app.releaseNFO("watch-test")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>After busy</title></movie>")
	time.Sleep(1300 * time.Millisecond)
	if _, found := find("After busy"); found {
		t.Fatal("watcher bypassed NFO write gate")
	}
	app.releaseNFO("watch-test")
	awaitLibraryWatch(t, func() bool { _, found := find("After busy"); return found })
	directory := filepath.Join(root, "new", "child")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(directory, "movie.strm"), "http://media.test/movie.mp4\n")
	writeFile(t, filepath.Join(directory, "movie.nfo"), "<movie><title>Nested</title></movie>")
	awaitLibraryWatch(t, func() bool { _, found := find("Nested"); return found })
	awaitLibraryWatch(t, func() bool { return !app.scanning() })
	renamed := filepath.Join(root, "renamed")
	if err := os.Rename(filepath.Join(root, "new"), renamed); err != nil {
		t.Fatal(err)
	}
	awaitLibraryWatch(t, func() bool {
		movie, found := find("Nested")
		return found && movie.SourcePath == filepath.Join(renamed, "child", "movie.strm")
	})
	writeFile(t, filepath.Join(renamed, "child", "movie.nfo"), "<movie><title>Moved edit</title></movie>")
	awaitLibraryWatch(t, func() bool { _, found := find("Moved edit"); return found })
	for _, name := range []string{"child/movie.strm", "child/movie.nfo", "child"} {
		if err := os.Remove(filepath.Join(renamed, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	awaitLibraryWatch(t, func() bool { _, found := find("Moved edit"); return !found })
	if _, found := find("After busy"); !found {
		t.Fatal("subtree deletion removed unrelated movie")
	}
	otherRoot := t.TempDir()
	writeFile(t, filepath.Join(otherRoot, "other.strm"), "http://media.test/other.mp4\n")
	writeFile(t, filepath.Join(otherRoot, "other.nfo"), "<movie><title>New library</title></movie>")
	body, err := json.Marshal(map[string]string{"Name": "other", "Path": otherRoot})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/libraries", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Emby-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("add library: %d", response.StatusCode)
	}
	awaitLibraryWatch(t, func() bool { _, found := find("New library"); return found })
	request, err = http.NewRequest(http.MethodDelete, server.URL+"/api/admin/libraries/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Emby-Token", token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete library: %d", response.StatusCode)
	}
	app.watchMu.Lock()
	stopped := app.monitors[1] == nil
	app.watchMu.Unlock()
	if !stopped {
		t.Fatal("deleted library is still monitored")
	}
}

func TestLibraryWatchCanBeDisabled(test *testing.T) {
	for _, mode := range []string{librarywatch.ModeRealtime, librarywatch.ModePolling} {
		test.Run(mode, func(test *testing.T) {
			app, _, _ := newProbeTestApp(test, test.TempDir())
			app.cfg.DisableLibraryMonitor = true
			app.cfg.LibraryMonitorMode = mode
			app.startLibraryMonitoring()
			app.watchMu.Lock()
			defer app.watchMu.Unlock()
			if len(app.monitors) != 0 {
				test.Fatal("disabled library monitoring started watchers")
			}
		})
	}
}

func TestLibraryPollingEndToEnd(test *testing.T) {
	root := test.TempDir()
	if err := os.Mkdir(filepath.Join(root, "old"), 0755); err != nil {
		test.Fatal(err)
	}
	for name, title := range map[string]string{"a": "A", "b": "B", "deleted": "Deleted", "old/movie": "Moved"} {
		writeFile(test, filepath.Join(root, name+".strm"), "http://media.test/movie.mp4\n")
		writeFile(test, filepath.Join(root, name+".nfo"), "<movie><title>"+title+"</title></movie>")
	}
	app, server, token := newProbeTestApp(test, root)
	app.cfg.LibraryMonitorMode = librarywatch.ModePolling
	app.startLibraryMonitoring()
	awaitLibraryWatch(test, func() bool {
		app.taskMu.Lock()
		defer app.taskMu.Unlock()
		return len(app.tasks) > 0 && app.tasks[0].Type == "poll" && app.tasks[0].Status == "success"
	})
	response, raw := embyRaw(test, server, http.MethodGet, "/api/admin/settings", token, "")
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil || response.StatusCode != http.StatusOK || settings["library_monitor_mode"] != "polling" || settings["disable_library_monitor"] != false {
		test.Fatalf("monitor settings = %s, err=%v", raw, err)
	}
	find := func(title string) (store.Movie, bool) {
		movies, _, err := app.db.SearchAll(0, "", "", "title", false, 100, 0)
		if err != nil {
			test.Fatal(err)
		}
		for _, movie := range movies {
			if movie.Title == title {
				return movie, true
			}
		}
		return store.Movie{}, false
	}
	unchanged, found := find("B")
	if !found {
		test.Fatal("initial movie missing")
	}
	for attempt := 0; attempt < 4; attempt++ {
		writeFile(test, filepath.Join(root, "a.nfo"), "<movie><title>Polling "+strconv.Itoa(attempt)+"</title></movie>")
	}
	for _, name := range []string{"deleted.strm", "deleted.nfo"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			test.Fatal(err)
		}
	}
	renamed := filepath.Join(root, "renamed")
	if err := os.Rename(filepath.Join(root, "old"), renamed); err != nil {
		test.Fatal(err)
	}
	nested := filepath.Join(root, "new", "child")
	if err := os.MkdirAll(nested, 0755); err != nil {
		test.Fatal(err)
	}
	writeFile(test, filepath.Join(nested, "movie.strm"), "http://media.test/new.mp4\n")
	writeFile(test, filepath.Join(nested, "movie.nfo"), "<movie><title>New</title></movie>")
	time.Sleep(1500 * time.Millisecond)
	if _, found := find("Polling 3"); found {
		test.Fatal("polling mode unexpectedly used immediate filesystem notifications")
	}
	deadline := time.Now().Add(librarywatch.DefaultPollInterval + 5*time.Second)
	for {
		_, updated := find("Polling 3")
		_, added := find("New")
		_, deleted := find("Deleted")
		moved, exists := find("Moved")
		if updated && added && !deleted && exists && moved.SourcePath == filepath.Join(renamed, "movie.strm") && !app.scanning() {
			break
		}
		if time.Now().After(deadline) {
			test.Fatal("30 second polling did not reconcile file updates, deletion and directory moves")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if after, exists := find("B"); !exists || after.ID != unchanged.ID || after.UpdatedAt != unchanged.UpdatedAt {
		test.Fatal("local refresh rewrote the unaffected movie")
	}
	otherRoot := test.TempDir()
	writeFile(test, filepath.Join(otherRoot, "other.strm"), "http://media.test/other.mp4\n")
	writeFile(test, filepath.Join(otherRoot, "other.nfo"), "<movie><title>Other library</title></movie>")
	body, err := json.Marshal(map[string]string{"Name": "other", "Path": otherRoot})
	if err != nil {
		test.Fatal(err)
	}
	if response, raw := embyRaw(test, server, http.MethodPost, "/api/admin/libraries", token, string(body)); response.StatusCode != http.StatusOK {
		test.Fatalf("add polled library: %d %s", response.StatusCode, raw)
	}
	awaitLibraryWatch(test, func() bool { _, found := find("Other library"); return found })
	if response, raw := embyRaw(test, server, http.MethodDelete, "/api/admin/libraries/1", token, ""); response.StatusCode != http.StatusNoContent {
		test.Fatalf("delete polled library: %d %s", response.StatusCode, raw)
	}
	app.watchMu.Lock()
	defer app.watchMu.Unlock()
	if app.monitors[1] != nil {
		test.Fatal("deleted library is still being polled")
	}
}

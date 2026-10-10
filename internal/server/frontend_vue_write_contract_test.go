package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the actual HTTP *bool defaults, not a second implementation of them.
func TestVueMigrationOverwriteContract(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, scenario := range []struct {
			name     string
			global   bool
			payload  string
			replaced bool
		}{
			{"explicit_false", true, `,"overwrite":false`, false},
			{"explicit_true", false, `,"overwrite":true`, true},
			{"omitted_global_false", false, "", false},
			{"omitted_global_true", true, "", true},
		} {
			t.Run(fmt.Sprintf("batch_%t/%s", batch, scenario.name), func(t *testing.T) {
				app, ts, token := contractEnv(t)
				backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if strings.HasSuffix(r.URL.Path, "/search") {
						fmt.Fprint(w, `{"data":[{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated"}]}`)
						return
					}
					fmt.Fprint(w, `{"data":{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated","summary":"New plot","director":"Missing director"}}`)
				}))
				defer backend.Close()
				for key, value := range map[string]string{settingMetatubeURL: backend.URL, settingMetatubeToken: "fixture", settingOverwrite: fmt.Sprint(scenario.global), settingDownloadImages: "false"} {
					if err := app.db.SetSetting(key, value); err != nil {
						t.Fatal(err)
					}
				}
				movies, _, err := app.db.SearchAll(0, "", "", "title", false, 10, 0)
				if err != nil || len(movies) != 1 {
					t.Fatalf("fixture: %v", err)
				}
				movie := movies[0]
				endpoint := fmt.Sprintf("/api/admin/items/%d/scrape", movie.ID)
				body := `{"provider":"fanza","id":"a"` + scenario.payload + `}`
				expected := http.StatusOK
				if batch {
					endpoint = "/api/admin/scrape/run"
					body = fmt.Sprintf(`{"library_id":%d,"only_missing":false%s}`, movie.LibraryID, scenario.payload)
					expected = http.StatusAccepted
				}
				response, raw := embyRaw(t, ts, "POST", endpoint, token, body)
				if response.StatusCode != expected {
					t.Fatalf("%d: %s", response.StatusCode, raw)
				}
				if batch {
					deadline := time.Now().Add(5 * time.Second)
					for app.scraping() && time.Now().Before(deadline) {
						time.Sleep(10 * time.Millisecond)
					}
					if app.scraping() {
						t.Fatal("batch did not finish")
					}
				}
				nfo, err := os.ReadFile(movie.NFOPath)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(nfo), "<title>ABF-018 Updated</title>") != scenario.replaced {
					t.Fatalf("unexpected title: %s", nfo)
				}
				if !strings.Contains(string(nfo), "Missing director") {
					t.Fatal("missing field was not filled")
				}
			})
		}
	}
}

func TestVueMigrationPreviewAndRootCancellation(t *testing.T) {
	app, ts, token := contractEnv(t)
	movies, _, err := app.db.SearchAll(0, "", "", "title", false, 10, 0)
	if err != nil || len(movies) != 1 {
		t.Fatalf("fixture: %v", err)
	}
	movie := movies[0]
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
			entered <- struct{}{}
			<-r.Context().Done()
			return
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/search") {
			fmt.Fprint(w, `{"data":[{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated"}]}`)
			return
		}
		fmt.Fprint(w, `{"data":{"provider":"fanza","id":"a","number":"ABF-018","title":"Updated"}}`)
	}))
	defer backend.Close()
	for key, value := range map[string]string{settingMetatubeURL: backend.URL, settingMetatubeToken: "fixture", settingDownloadImages: "false"} {
		if err := app.db.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		files, err := os.ReadDir(filepath.Dir(movie.SourcePath))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			name := filepath.Join(filepath.Dir(movie.SourcePath), file.Name())
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			out[file.Name()] = fmt.Sprintf("%x/%d", sha256.Sum256(data), info.ModTime().UnixNano())
		}
		return out
	}
	before, _ := json.Marshal(snapshot())
	for _, request := range []struct{ method, path, body string }{
		{"GET", fmt.Sprintf("/api/admin/items/%d/scrape/preview", movie.ID), ""},
		{"POST", fmt.Sprintf("/api/admin/items/%d/scrape/inspect", movie.ID), `{"provider":"fanza","id":"a"}`},
	} {
		response, raw := embyRaw(t, ts, request.method, request.path, token, request.body)
		if response.StatusCode != 200 {
			t.Fatalf("read %d: %s", response.StatusCode, raw)
		}
	}
	after, _ := json.Marshal(snapshot())
	if string(before) != string(after) {
		t.Fatal("preview/inspect changed file content or mtime")
	}
	close(block)
	response, raw := embyRaw(t, ts, "POST", "/api/admin/scrape/run", token, fmt.Sprintf(`{"library_id":%d,"only_missing":false}`, movie.LibraryID))
	if response.StatusCode != 202 {
		t.Fatalf("start %d: %s", response.StatusCode, raw)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream task never started")
	}
	app.rootCancel()
	deadline := time.Now().Add(5 * time.Second)
	for app.scraping() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	app.scrapeTaskMu.RLock()
	status := app.scrapeStatus
	app.scrapeTaskMu.RUnlock()
	if status.Running || !status.Cancelled || app.nfoBusyOwner() != "" {
		t.Fatalf("root cancellation did not release task: %+v", status)
	}
	after, _ = json.Marshal(snapshot())
	if string(before) != string(after) {
		t.Fatal("cancelled task changed untouched files")
	}
}

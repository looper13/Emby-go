package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestLibraryResolvedPlaybackURL(t *testing.T) {
	var cloudRequests atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudRequests.Add(1)
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer cloud.Close()
	const suffix = "/movie%20name.mp4?signature=a%2Bb&part=1"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-0" {
			t.Errorf("resolver must issue a bounded GET: %s %v", r.Method, r.Header)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Emby-Token") != "" {
			t.Error("account credentials forwarded to source")
		}
		if r.URL.Path == "/d/movie.mp4" {
			w.Header().Set("Location", "/redirect")
		} else {
			w.Header().Set("Location", cloud.URL+suffix)
		}
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	app := accountApp(t)
	token := accountLogin(t, app)
	root := t.TempDir()
	library, err := app.db.AddLibraryWithPlaybackSettings("Resolved", root, false, true)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "movie.strm"), upstream.URL+"/d/movie.mp4")
	id, err := app.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(root, "movie.strm"), Title: "Resolved", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	itemID := strconv.FormatInt(id, 10)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := accountRequest(app, method, "/emby/Videos/"+itemID+"/stream", "", "")
		if response.Code != http.StatusFound || response.Header().Get("Location") != cloud.URL+suffix {
			t.Fatalf("resolved stream: %d %v", response.Code, response.Header())
		}
	}
	for _, endpoint := range []string{"/Items/" + itemID + "/PlaybackInfo", "/emby/items/" + itemID + "/playbackinfo"} {
		response := accountRequest(app, http.MethodPost, endpoint, token, "")
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || playbackSource(t, body)["ExternalStreamUrl"] != cloud.URL+suffix {
			t.Fatalf("resolved external playback: %d %+v", response.Code, body)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("signed playback URL must not be cached")
		}
	}
	if cloudRequests.Load() != 0 {
		t.Fatal("resolving a URL fetched the cloud video")
	}
}

func TestLibraryResolveRedirectFailures(t *testing.T) {
	for _, scenario := range []string{"missing-location", "non-http", "loop", "upstream-error", "not-redirected"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "non-http":
					w.Header().Set("Location", "file:///private/video.mp4")
				case "loop":
					w.Header().Set("Location", "/loop")
				case "upstream-error":
					w.WriteHeader(http.StatusForbidden)
					return
				case "not-redirected":
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusFound)
			}))
			defer upstream.Close()
			app := accountApp(t)
			root := t.TempDir()
			lib, err := app.db.AddLibraryWithPlaybackSettings("Resolve", root, false, true)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(root, "movie.strm"), upstream.URL+"/loop")
			id, err := app.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "movie.strm"), Title: "Resolve", Status: "success"}, 0, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			response := accountRequest(app, http.MethodGet, "/Videos/"+strconv.FormatInt(id, 10)+"/stream", "", "")
			if response.Code != http.StatusBadGateway || response.Header().Get("Location") != "" {
				t.Fatalf("invalid redirect accepted: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestAdminLibraryResolveRedirectSettings(t *testing.T) {
	app := accountApp(t)
	token := accountLogin(t, app)
	payload, _ := json.Marshal(map[string]any{"Name": "Resolve", "Path": t.TempDir(), "ResolveRedirects": true})
	response := accountRequest(app, http.MethodPost, "/api/admin/libraries", token, string(payload))
	var library store.Library
	if err := json.Unmarshal(response.Body.Bytes(), &library); err != nil || response.Code != http.StatusOK || !library.ResolveRedirects {
		t.Fatalf("create resolve setting: %d %s", response.Code, response.Body)
	}
	endpoint := "/api/admin/libraries/" + strconv.FormatInt(library.ID, 10)
	response = accountRequest(app, http.MethodPut, endpoint, token, `{"ProxyEnabled":false,"Revision":1}`)
	if err := json.Unmarshal(response.Body.Bytes(), &library); err != nil || response.Code != http.StatusOK || !library.ResolveRedirects {
		t.Fatalf("legacy update discarded setting: %d %s", response.Code, response.Body)
	}
	response = accountRequest(app, http.MethodPut, endpoint, token, `{"ProxyEnabled":false,"ResolveRedirects":false,"Revision":2}`)
	if err := json.Unmarshal(response.Body.Bytes(), &library); err != nil || response.Code != http.StatusOK || library.ResolveRedirects {
		t.Fatalf("disable resolve setting: %d %s", response.Code, response.Body)
	}
}

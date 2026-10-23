package server

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/mediaassets"
	"emby-go/internal/metatube"
	"emby-go/internal/store"
)

func TestScrapeAssetsEmptyTokenSettingsAndLocalPlayback(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	root := t.TempDir()
	lib, err := a.db.AddLibrary("assets", root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "ALDN-001.strm")
	if err := os.WriteFile(source, []byte("http://example.invalid/full.mp4"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: source, OutputDir: root, Title: "Test", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var backend *httptest.Server
	backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("empty token sent Authorization")
		}
		switch r.URL.Path {
		case "/v1/movies/p/1":
			json.NewEncoder(w).Encode(map[string]any{"data": metatube.MovieInfo{ID: "1", Provider: "p", Number: "ALDN-001", Title: "Test", PreviewImages: []string{backend.URL + "/still.jpg"}, PreviewVideoURL: backend.URL + "/video.mp4"}})
		case "/still.jpg":
			jpeg.Encode(w, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil)
		case "/video.mp4":
			w.Write([]byte("\x00\x00\x00\x18ftypisom0123456789"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	payload := fmt.Sprintf(`{"metatube_url":%q,"download_images":false,"download_stills":true,"download_trailer":true}`, backend.URL)
	resp := accountRequest(a, "PUT", "/api/admin/scrape/settings", token, payload)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"download_stills":true`) || !strings.Contains(resp.Body.String(), `"configured":true`) {
		t.Fatalf("settings: %d %s", resp.Code, resp.Body.String())
	}
	resp = accountRequest(a, "POST", fmt.Sprintf("/api/admin/items/%d/scrape", id), token, `{"provider":"p","id":"1"}`)
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"trailer_downloaded":true`) {
		t.Fatalf("scrape: %d %s", resp.Code, resp.Body.String())
	}
	// A local copy plays even after the upstream source disappears.
	backend.Close()
	resp = accountRequest(a, "GET", fmt.Sprintf("/api/admin/items/%d/detail", id), token, "")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), fmt.Sprintf("/Videos/%d/trailer.mp4", id)) || !strings.Contains(resp.Body.String(), `"ImageType":"Backdrop"`) {
		t.Fatalf("detail: %d %s", resp.Code, resp.Body.String())
	}
	request := httptest.NewRequest("GET", fmt.Sprintf("/Videos/%d/trailer", id), nil)
	request.Header.Set("X-Emby-Token", token)
	request.Header.Set("Range", "bytes=4-7")
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, request)
	if response.Code != 206 || response.Body.String() != "ftyp" {
		t.Fatalf("local range: %d %q", response.Code, response.Body.String())
	}
	if response := accountRequest(a, "HEAD", fmt.Sprintf("/Videos/%d/trailer", id), token, ""); response.Code != 200 || response.Body.Len() != 0 {
		t.Fatalf("local HEAD: %d", response.Code)
	}
	// Media files retain the project's anonymous client playback policy.
	if response := accountRequest(a, "GET", fmt.Sprintf("/Videos/%d/trailer", id), "", ""); response.Code != 200 {
		t.Fatal("anonymous client playback regressed")
	}
	// An inaccessible library must remain inaccessible when the trailer is local.
	viewerID, viewer := createViewer(t, a, token, "trailer-viewer")
	var uid int64
	fmt.Sscan(viewerID, &uid)
	if err := a.db.ForUser(uid).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if response := accountRequest(a, "GET", fmt.Sprintf("/Videos/%d/trailer", id), viewer, ""); response.Code != 404 {
		t.Fatalf("library access leak: %d", response.Code)
	}
	if mediaassets.LocalTrailer(source, root) == "" {
		t.Fatal("trailer missing")
	}
	resp = accountRequest(a, "PUT", "/api/admin/scrape/settings", token, `{"download_stills":false,"download_trailer":false}`)
	if resp.Code != 200 || a.scrapeConfig().DownloadStills || a.scrapeConfig().DownloadTrailer {
		t.Fatal("cannot turn off saved download options")
	}
}

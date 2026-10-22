package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestHLSProxyRewritesNestedPlaylistsAndRanges(t *testing.T) {
	var masterRange string
	var segmentRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			masterRange = r.Header.Get("Range")
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nvariant/index.m3u8\n"))
		case "/variant/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"../keys/key.bin\"\r\n#EXT-X-MAP:URI=\"init.mp4\"\r\n#EXTINF:6,\r\nseg000.ts?token=abc\r\n#EXT-X-ENDLIST\r\n"))
		case "/keys/key.bin":
			_, _ = w.Write([]byte("key"))
		case "/variant/init.mp4":
			_, _ = w.Write([]byte("init"))
		case "/variant/seg000.ts":
			segmentRange = r.Header.Get("Range")
			if r.URL.Query().Get("token") != "abc" {
				http.Error(w, "missing upstream query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "video/mp2t")
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("cde"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	a := accountApp(t)
	token := accountLogin(t, a)
	root := t.TempDir()
	lib, err := a.db.AddLibraryWithProxy("HLS", root, true)
	if err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(root, "film.strm")
	if err := os.WriteFile(strm, []byte(upstream.URL+"/master.m3u8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: strm, Title: "Film", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item := fmt.Sprint(id)
	request := httptest.NewRequest(http.MethodGet, "/Videos/"+item+"/stream", nil)
	request.Header.Set("X-Emby-Token", token)
	request.Header.Set("Range", "bytes=0-99")
	response := httptest.NewRecorder()
	a.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "application/vnd.apple.mpegurl") {
		t.Fatalf("HLS master: status=%d content-type=%q body=%q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	if masterRange != "" {
		t.Fatalf("HLS playlist request must not forward Range, got %q", masterRange)
	}
	masterResource := hlsProxyURLInBody(t, response.Body.String())
	if !strings.Contains(masterResource, "/Videos/"+item+"/hls-proxy/") {
		t.Fatalf("master URI was not rewritten to the local proxy: %q", masterResource)
	}

	nested := accountRequest(a, http.MethodGet, masterResource, "", "")
	if nested.Code != http.StatusOK || !strings.Contains(nested.Header().Get("Content-Type"), "application/vnd.apple.mpegurl") {
		t.Fatalf("nested playlist: status=%d content-type=%q body=%q", nested.Code, nested.Header().Get("Content-Type"), nested.Body.String())
	}
	resources := hlsProxyURLsInBody(t, nested.Body.String())
	if len(resources) != 3 {
		t.Fatalf("expected rewritten key, map and segment URIs, got %d in %q", len(resources), nested.Body.String())
	}
	if !strings.Contains(nested.Body.String(), "#EXT-X-KEY:METHOD=AES-128,URI=\"") || !strings.Contains(nested.Body.String(), "#EXT-X-MAP:URI=\"") {
		t.Fatalf("HLS URI attributes were not rewritten: %q", nested.Body.String())
	}
	if strings.Contains(nested.Body.String(), "seg000.ts?token=abc") {
		t.Fatalf("upstream segment URI leaked into the client playlist: %q", nested.Body.String())
	}

	segmentRequest := httptest.NewRequest(http.MethodGet, resources[2], nil)
	segmentRequest.Header.Set("Range", "bytes=2-4")
	segmentResponse := httptest.NewRecorder()
	a.Handler().ServeHTTP(segmentResponse, segmentRequest)
	if segmentResponse.Code != http.StatusPartialContent || segmentResponse.Body.String() != "cde" || segmentResponse.Header().Get("Content-Range") != "bytes 2-4/6" {
		t.Fatalf("segment response: status=%d body=%q range=%q", segmentResponse.Code, segmentResponse.Body.String(), segmentResponse.Header().Get("Content-Range"))
	}
	if segmentRange != "bytes=2-4" {
		t.Fatalf("segment Range was not forwarded, got %q", segmentRange)
	}
	if invalid := accountRequest(a, http.MethodGet, strings.Replace(masterResource, "/Videos/"+item+"/", "/Videos/999999/", 1), "", ""); invalid.Code != http.StatusNotFound {
		t.Fatalf("HLS resource must be scoped to its media item, got %d", invalid.Code)
	}
}

func TestHLSProxyCapabilityRechecksLibraryAccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\nsegment.ts\n"))
	}))
	defer upstream.Close()

	a := accountApp(t)
	admin := accountLogin(t, a)
	viewerRawID, viewerToken := createViewer(t, a, admin, "hls-viewer")
	var viewerID int64
	if _, err := fmt.Sscan(viewerRawID, &viewerID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	lib, err := a.db.AddLibraryWithProxy("HLS viewer", root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}}); err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(root, "film.strm")
	if err = os.WriteFile(strm, []byte(upstream.URL+"/master.m3u8\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: strm, Title: "Film", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	manifest := accountRequest(a, http.MethodGet, fmt.Sprintf("/Videos/%d/stream", id), viewerToken, "")
	if manifest.Code != http.StatusOK {
		t.Fatalf("viewer manifest: status=%d body=%q", manifest.Code, manifest.Body.String())
	}
	resource := hlsProxyURLInBody(t, manifest.Body.String())
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if response := accountRequest(a, http.MethodGet, resource, "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("revoked viewer retained HLS access: status=%d body=%q", response.Code, response.Body.String())
	}
}

func hlsProxyURLInBody(t *testing.T, body string) string {
	t.Helper()
	urls := hlsProxyURLsInBody(t, body)
	if len(urls) == 0 {
		t.Fatalf("no local HLS proxy URL in %q", body)
	}
	return urls[0]
}

func hlsProxyURLsInBody(t *testing.T, body string) []string {
	t.Helper()
	var urls []string
	for _, line := range strings.Split(body, "\n") {
		for rest := line; ; {
			start := strings.Index(rest, "/Videos/")
			if start < 0 {
				break
			}
			rest = rest[start:]
			end := strings.IndexAny(rest, "\"\r\n")
			value := rest
			if end >= 0 {
				value = rest[:end]
			}
			urls = append(urls, value)
			if end < 0 {
				break
			}
			rest = rest[end+1:]
		}
	}
	return urls
}

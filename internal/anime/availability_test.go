package anime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMediaCheckRecognizesBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body, ct string
		status         int
		playable       bool
	}{
		{"mp4", string([]byte{0, 0, 0, 24}) + "ftypisom", "video/mp4", 206, true},
		{"range ignored", string([]byte{0, 0, 0, 24}) + "ftypisom" + strings.Repeat("x", 100000), "application/octet-stream", 200, true},
		{"login", "<!doctype html><html>login", "video/mp4", 200, false},
		{"torrent", "d8:announce123", "application/x-bittorrent", 200, false},
		{"404", "missing", "text/plain", 404, false},
		{"hls", "#EXTM3U\n#EXTINF:10\na.ts", "application/vnd.apple.mpegurl", 200, true},
		{"false video", "hello", "video/mp4", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "bytes=0-65535" {
					t.Error("missing bounded Range")
				}
				w.Header().Set("Content-Type", tc.ct)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			err := CheckMedia(context.Background(), ts.URL)
			if (err == nil) != tc.playable {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if CheckMedia(ctx, "http://127.0.0.1:1/media") == nil {
		t.Fatal("cancel ignored")
	}
}

func TestMediaCheckRedirectAndCancellationBounds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer ts.Close()
	if err := CheckMedia(context.Background(), ts.URL+"/loop"); err == nil {
		t.Fatal("redirect loop accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := CheckMedia(ctx, ts.URL+"/slow"); err == nil {
		t.Fatal("slow response accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("caller deadline not respected")
	}
}

func TestAnimeIDRoundTrip(t *testing.T) {
	for _, raw := range []string{ID("series", 42), ID("episode", 5), ID("movie", 7), SeasonID(42, 0), SeasonID(42, 1)} {
		if _, _, _, ok := ParseID(raw); !ok {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"42", "anime-episode:0", "anime-movie:-1", "anime-movie:01", "anime-episode:1:2", "anime-season:1:5", "anime-series:1:0", "anime-unknown:1"} {
		if _, _, _, ok := ParseID(raw); ok {
			t.Fatal(raw)
		}
	}
}

package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"emby-go/internal/imageutil"
	"emby-go/internal/mediaassets"
	"emby-go/internal/metatube"
	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

func TestExtrasOptionsAndRepeatScrape(t *testing.T) {
	var downloads atomic.Int32
	var backend *httptest.Server
	backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("public media received credentials")
		}
		downloads.Add(1)
		if r.URL.Path == "/still.jpg" {
			jpeg.Encode(w, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil)
			return
		}
		w.Write([]byte("\x00\x00\x00\x18ftypisom0123456789"))
	}))
	defer backend.Close()
	for _, options := range []struct{ stills, trailer bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprint(options), func(t *testing.T) {
			root := t.TempDir()
			movie := store.Movie{SourcePath: filepath.Join(root, "ALDN-001.strm"), OutputDir: root}
			info := metatube.MovieInfo{Number: "ALDN-001", Title: "Test", PreviewImages: []string{backend.URL + "/still.jpg", backend.URL + "/still.jpg"}, PreviewVideoURL: backend.URL + "/preview.mp4"}
			cfg := DefaultConfig()
			cfg.DownloadImages = false
			cfg.DownloadStills = options.stills
			cfg.DownloadTrailer = options.trailer
			s := New(cfg, nil)
			before := downloads.Load()
			result, err := s.Apply(context.Background(), movie, info, ApplyOptions{})
			if err != nil || len(result.ImageWarn) > 0 {
				t.Fatalf("apply: %+v %v", result, err)
			}
			want := 0
			if options.stills {
				want = 2
			}
			if result.ImageCount != want || (result.TrailerPath != "") != options.trailer {
				t.Fatalf("wrong downloads: %+v", result)
			}
			if options.trailer {
				want++
			}
			if int(downloads.Load()-before) != want {
				t.Fatal("disabled options still downloaded")
			}
			meta, err := nfo.Read(filepath.Join(root, "ALDN-001.nfo"))
			if err != nil || meta.TrailerURL() != info.PreviewVideoURL {
				t.Fatalf("NFO trailer: %+v %v", meta, err)
			}
			images := imageutil.FindMovieImages(movie.SourcePath, root, nonemptyFile)
			if len(images.Backdrops) != result.ImageCount {
				t.Fatalf("stills not discoverable: %+v", images)
			}
			other := imageutil.FindMovieImages(filepath.Join(root, "other.strm"), root, nonemptyFile)
			if len(other.Backdrops) != 0 {
				t.Fatal("stills leaked into another movie")
			}
			before = downloads.Load()
			again, err := s.Apply(context.Background(), movie, info, ApplyOptions{})
			if err != nil || again.ImageCount != 0 || downloads.Load() != before {
				t.Fatalf("repeat downloaded existing files: %+v %v", again, err)
			}
			_, err = s.Apply(context.Background(), movie, info, ApplyOptions{Overwrite: true})
			if err != nil || int(downloads.Load()-before) != want {
				t.Fatalf("overwrite did not refresh: %v", err)
			}
		})
	}
}

func TestTrailerFailureAndCancellationPreserveExisting(t *testing.T) {
	for _, cancelDownload := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDownload), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cancelDownload {
					w.Write([]byte("\x00\x00\x00\x18ftypisom"))
					w.(http.Flusher).Flush()
					cancel()
					<-r.Context().Done()
					return
				}
				w.Write([]byte("<html>not a video</html>"))
			}))
			defer backend.Close()
			root := t.TempDir()
			dest := filepath.Join(root, "film-trailer.mp4")
			if err := os.WriteFile(dest, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			s := New(DefaultConfig(), nil)
			_, err := s.downloadTrailer(ctx, backend.URL, dest, true)
			if err == nil {
				t.Fatal("expected failed download")
			}
			if cancelDownload && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			data, _ := os.ReadFile(dest)
			if string(data) != "original" {
				t.Fatal("existing trailer replaced")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatal("partial files left on disk")
			}
		})
	}
}

func TestBatchExtrasWarningsRemainVisible(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/search"):
			json.NewEncoder(w).Encode(map[string]any{"data": []metatube.MovieSearchResult{{Provider: "p", ID: "1", Number: "ALDN-001"}}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"data": metatube.MovieInfo{Provider: "p", ID: "1", Number: "ALDN-001", Title: "Test"}})
		}
	}))
	defer backend.Close()
	cfg := DefaultConfig()
	cfg.BaseURL = backend.URL
	cfg.DownloadImages = false
	cfg.DownloadStills = true
	cfg.DownloadTrailer = true
	var item ItemResult
	progress, err := New(cfg, nil).RunMovies(context.Background(), []store.Movie{{SourcePath: filepath.Join(t.TempDir(), "ALDN-001.strm")}}, RunOptions{OnItem: func(r ItemResult) { item = r }})
	if err != nil || progress.Success != 1 || progress.Partial != 1 || progress.DownloadFailed != 1 || len(item.Warnings) != 2 {
		t.Fatalf("hidden resource failure: %+v %+v %v", progress, item, err)
	}
}

func TestBatchDownloadsExtrasForMoviesSharingDirectory(t *testing.T) {
	var backend *httptest.Server
	backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/movies/search":
			code := r.URL.Query().Get("q")
			json.NewEncoder(w).Encode(map[string]any{"data": []metatube.MovieSearchResult{{Provider: "p", ID: code, Number: code}}})
		case strings.HasPrefix(r.URL.Path, "/v1/movies/p/"):
			code := strings.TrimPrefix(r.URL.Path, "/v1/movies/p/")
			json.NewEncoder(w).Encode(map[string]any{"data": metatube.MovieInfo{Provider: "p", ID: code, Number: code, Title: "Test", PreviewImages: []string{backend.URL + "/still.jpg"}, PreviewVideoURL: backend.URL + "/video.mp4"}})
		case r.URL.Path == "/still.jpg":
			jpeg.Encode(w, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil)
		case r.URL.Path == "/video.mp4":
			w.Write([]byte("\x00\x00\x00\x18ftypisom0123456789"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	root := t.TempDir()
	movies := []store.Movie{{SourcePath: filepath.Join(root, "ALDN-001.strm")}, {SourcePath: filepath.Join(root, "ALDN-002.strm")}}
	cfg := DefaultConfig()
	cfg.BaseURL = backend.URL
	cfg.DownloadImages = false
	cfg.DownloadStills = true
	cfg.DownloadTrailer = true
	progress, err := New(cfg, nil).RunMovies(context.Background(), movies, RunOptions{})
	if err != nil || progress.Success != 2 || progress.Partial != 0 {
		t.Fatalf("batch incomplete: %+v %v", progress, err)
	}
	for _, movie := range movies {
		images := imageutil.FindMovieImages(movie.SourcePath, root, nonemptyFile)
		if len(images.Backdrops) != 1 || mediaassets.LocalTrailer(movie.SourcePath, root) == "" {
			t.Fatalf("movie resources missing: %s %+v", movie.SourcePath, images)
		}
	}
}

// Opt-in real integration: isolated output only, never writes an existing library.
func TestMetaTubeExtrasLive(t *testing.T) {
	base := os.Getenv("EMBY_METATUBE_LIVE_URL")
	if base == "" {
		t.Skip("set EMBY_METATUBE_LIVE_URL to run the real integration")
	}
	root := os.Getenv("EMBY_METATUBE_LIVE_OUTPUT")
	if root == "" {
		root = t.TempDir()
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.BaseURL = base
	cfg.Token = ""
	cfg.DownloadImages = false
	cfg.DownloadStills = true
	cfg.DownloadTrailer = true
	s := New(cfg, nil)
	ctx := context.Background()
	results, err := s.Client().SearchMovies(ctx, "ALDN-001", "", true)
	if err != nil {
		t.Fatal(err)
	}
	chosen, ok := selectExact(results, "ALDN-001")
	if !ok {
		t.Fatal("no exact match")
	}
	info, err := s.MovieInfo(ctx, chosen.Provider, chosen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.PreviewImages) == 0 || info.PreviewVideoURL == "" {
		t.Fatal("missing real media fields")
	}
	movie := store.Movie{SourcePath: filepath.Join(root, "ALDN-001.strm"), OutputDir: root}
	if err := os.WriteFile(movie.SourcePath, []byte("http://example.invalid/movie.mp4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.Apply(ctx, movie, info, ApplyOptions{Overwrite: true})
	if err != nil || len(result.ImageWarn) > 0 || result.ImageCount != len(info.PreviewImages) || mediaassets.LocalTrailer(movie.SourcePath, root) == "" {
		t.Fatalf("live apply: %+v %v", result, err)
	}
	t.Logf("downloaded %d stills; trailer=%s; output=%s", result.ImageCount, result.TrailerPath, root)
	result, err = s.Apply(ctx, movie, info, ApplyOptions{})
	if err != nil || result.ImageCount != 0 || result.TrailerPath != "" {
		t.Fatalf("repeat: %+v %v", result, err)
	}
}

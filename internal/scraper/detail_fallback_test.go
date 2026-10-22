package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"emby-go/internal/nfo"
	"emby-go/internal/store"
)

func TestMovieDetailFallsBackOnlyToExactMatches(t *testing.T) {
	var details atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/movies/search":
			_, _ = w.Write([]byte(`{"data":[{"provider":"p","id":"bad","number":"ABF-018"},{"provider":"p","id":"wrong","number":"ABF-019"},{"provider":"p","id":"good","number":"ABF-018"}]}`))
		case "/v1/movies/p/bad":
			details.Add(1)
			http.Error(w, "incomplete metadata", http.StatusInternalServerError)
		case "/v1/movies/p/good":
			details.Add(1)
			_, _ = w.Write([]byte(`{"data":{"provider":"p","id":"good","number":"ABF-018","title":"correct"}}`))
		default:
			t.Errorf("non-exact candidate requested: %s", r.URL.Path)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer backend.Close()
	dir := t.TempDir()
	s := New(Config{BaseURL: backend.URL, Token: "test", Concurrency: 1}, nil)
	p, err := s.RunMovies(context.Background(), []store.Movie{{SourcePath: filepath.Join(dir, "ABF-018.strm"), Number: "ABF-018"}}, RunOptions{Overwrite: true})
	if err != nil || p.Success != 1 || details.Load() != 2 {
		t.Fatalf("fallback=%+v attempts=%d err=%v", p, details.Load(), err)
	}
	meta, err := nfo.Read(filepath.Join(dir, "ABF-018.nfo"))
	if err != nil || meta.ProviderID() != "metatube:p:good" || meta.Title != "ABF-018 correct" {
		t.Fatalf("wrong metadata: %+v %v", meta, err)
	}
}

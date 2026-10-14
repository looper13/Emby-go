package anime

import (
	"context"
	"emby-go/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTMDBEpisodeIdentityAndStills(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.Query().Get("api_key") != "fixture-key" || len(r.URL.Query()) != 1 {
			t.Error("invalid authentication or image language filter")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":91,"stills":[{"file_path":"/one.jpg"},{"file_path":"/one.jpg"},{"file_path":"/two.png"},{"file_path":"https://bad/image.jpg"}]}`))
	}))
	defer ts.Close()
	c := NewTMDBClient("fixture-key")
	c.HTTP = ts.Client()
	c.BaseURL = ts.URL
	paths, err := c.Stills(context.Background(), 10, 0, 1, 91)
	if err != nil || len(paths) != 2 || TMDBImageURL(paths[0]) != "https://image.tmdb.org/t/p/w780/one.jpg" {
		t.Fatal(paths, err)
	}
	if _, err = c.Stills(context.Background(), 10, 0, 1, 92); err == nil {
		t.Fatal("wrong episode accepted")
	}
	if TMDBImageURL("/../evil.jpg") != "" {
		t.Fatal("unsafe path")
	}
	c.APIKey = ""
	if _, err = c.Series(context.Background(), 1); err == nil || strings.Contains(err.Error(), "fixture-key") {
		t.Fatal(err)
	}
}

func TestTMDBLiveDefaultKey(t *testing.T) {
	if os.Getenv("EMBY_TMDB_LIVE_TEST") != "1" {
		t.Skip("explicit live check only")
	}
	cfg, err := config.Load("../../anime-tmdb.local.yaml")
	if err != nil {
		t.Fatal("local TMDB configuration unavailable")
	}
	if cfg.TMDBAPIKey == "" {
		t.Fatal("local API key missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = NewTMDBClient(cfg.TMDBAPIKey).Validate(ctx); err != nil {
		t.Fatal(err)
	}
}

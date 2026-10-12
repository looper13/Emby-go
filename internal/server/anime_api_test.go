package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/cache"
	"emby-go/internal/config"
)

func TestAnimeAdminAuthValidationAndSettingsConflict(t *testing.T) {
	a, err := newApp(config.Config{DBPath: filepath.Join(t.TempDir(), "db")}, cache.NewMemory(64))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.db.SaveAccessToken("anime-api-test"); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, token string, status int) {
		t.Helper()
		r := httptest.NewRequest(method, "/api/admin/anime"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("X-Emby-Token", token)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
	}
	request("GET", "/settings", "", "", 401)
	request("PUT", "/settings", `{}`, "bad-token", 401)
	request("GET", "/catalog?year=bad", "", "anime-api-test", 400)
	request("GET", "/catalog?season=5", "", "anime-api-test", 400)
	request("GET", "/catalog?weekday=9", "", "anime-api-test", 400)
	request("GET", "/catalog?source=invalid", "", "anime-api-test", 400)
	request("GET", "/entries/-1", "", "anime-api-test", 400)
	request("GET", "/entries/123456789", "", "anime-api-test", 404)
	request("GET", "/search?q=", "", "anime-api-test", 400)
	request("POST", "/refresh", `{"year":2026,"season":9}`, "anime-api-test", 400)
	request("POST", "/test", `{"mikan_url":"http://127.0.0.1"}`, "anime-api-test", 400)
	cfg, err := a.db.AnimeSettings()
	if err != nil {
		t.Fatal(err)
	}
	lib, err := a.db.AnimeLibrary()
	if err != nil {
		t.Fatal(err)
	}
	lib.Name, lib.Enabled = "Updated", false
	body, err := json.Marshal(map[string]any{"source": cfg, "library": lib})
	if err != nil {
		t.Fatal(err)
	}
	request("PUT", "/settings", string(body), "anime-api-test", 200)
	request("PUT", "/settings", string(body), "anime-api-test", 409)
	request("GET", "/catalog", "", "anime-api-test", 409)
	request("GET", "/settings", "", "anime-api-test", 200)
}

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func actorQueryFixture(t testing.TB, count int) *App {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	root := t.TempDir()
	cfg := config.Config{DBPath: filepath.Join(root, "actors.db"), ActorsDir: filepath.Join(root, "actors")}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := &App{cfg: cfg, db: db, rootCtx: context.Background(), imgMeta: cache.NewMemory(imageMetaCacheSize), cache: cache.NewManaged(cache.NewMemory(64))}
	if err := os.MkdirAll(cfg.ActorsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ActorsDir, "layout.json"), []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib, err := db.AddLibrary("Actors", filepath.Join(root, "media"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", cfg.DBPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO actors(name) SELECT printf('Actor%05d',x) FROM n`, []any{count}},
		{`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO movies(id,library_id,source_path,status,title,created_at,updated_at) SELECT x,?,printf('movie-%06d.strm',x),'success','fixture','','' FROM n`, []any{count * 2, lib.ID}},
		{`WITH RECURSIVE k(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM k WHERE x<9) INSERT INTO movie_actors(movie_id,actor_name) SELECT m.id,printf('Actor%05d',((m.id-1+k.x)%?)+1) FROM movies m CROSS JOIN k`, []any{count}},
	} {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return app
}

func actorQueryRequest(app *App, path string, handler func(*gin.Context)) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", path, nil)
	handler(c)
	return w
}

func TestAdminActorsSnapshotAcrossPages(t *testing.T) {
	app := actorQueryFixture(t, imageMetaCacheSize+8)
	get := func(query string) []adminActor {
		t.Helper()
		w := actorQueryRequest(app, "/api/admin/actors?"+query, app.adminActors)
		var result struct {
			Items []adminActor `json:"items"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return result.Items
	}
	first := get("limit=1")
	// Page data must survive eviction from the shared per-image LRU.
	app.imgMeta.Clear()
	second := get("limit=1&offset=1")
	if first[0].Name == second[0].Name {
		t.Fatal("pagination did not advance")
	}
	if loads := app.cache.Stats()["other"].Loads; loads != 1 {
		t.Fatalf("inventory loads=%d, want one snapshot shared by pages", loads)
	}
	if err := app.db.SetItemFavorite(entityId("Person", second[0].Name), true); err != nil {
		t.Fatal(err)
	}
	favorites := get("favorite=true")
	if len(favorites) != 1 || favorites[0].Name != second[0].Name || !favorites[0].Favorite {
		t.Fatal(favorites)
	}
	if loads := app.cache.Stats()["other"].Loads; loads != 1 {
		t.Fatal("favorites rebuilt the portrait inventory", loads)
	}
	if err := app.db.SetActorAvatar(first[0].Name, "", ""); err != nil {
		t.Fatal(err)
	}
	get("limit=1")
	if loads := app.cache.Stats()["other"].Loads; loads != 2 {
		t.Fatal("actor change did not invalidate inventory", loads)
	}
	if err := app.db.BumpVersion(1); err != nil {
		t.Fatal(err)
	}
	get("limit=1")
	if loads := app.cache.Stats()["other"].Loads; loads != 3 {
		t.Fatal("scan did not invalidate inventory", loads)
	}
	app.cache.Clear()
	get("limit=1")
	if loads := app.cache.Stats()["other"].Loads; loads != 4 {
		t.Fatal("settings/task invalidation was ignored", loads)
	}
}

func TestAdminActorsConcurrentSnapshot(t *testing.T) {
	app := actorQueryFixture(t, 100)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			w := actorQueryRequest(app, "/api/admin/actors?limit=1&offset="+strconv.Itoa(offset), app.adminActors)
			if w.Code != 200 {
				t.Errorf("status=%d body=%s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if loads := app.cache.Stats()["other"].Loads; loads != 1 {
		t.Fatalf("concurrent inventory loads=%d", loads)
	}
}

func BenchmarkActorQueries(b *testing.B) {
	app := actorQueryFixture(b, 9000)
	for _, tc := range []struct {
		name, path string
		cold       bool
		handler    func(*gin.Context)
	}{
		{"AdminCold", "/api/admin/actors?limit=48", true, app.adminActors},
		{"AdminWarm", "/api/admin/actors?limit=48", false, app.adminActors},
		{"AdminSearch", "/api/admin/actors?search=NobodyMatches&limit=48", false, app.adminActors},
		{"PersonsCold1000", "/Persons", true, app.persons},
		{"PersonsNoImages1000", "/Persons?EnableImages=false", true, app.persons},
	} {
		b.Run(tc.name, func(b *testing.B) {
			actorQueryRequest(app, tc.path, tc.handler)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tc.cold {
					app.cache.Clear()
					app.imgMeta.Clear()
				}
				w := actorQueryRequest(app, tc.path, tc.handler)
				if w.Code != 200 {
					b.Fatalf("%d %s", w.Code, w.Body.String())
				}
			}
		})
	}
}

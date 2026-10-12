package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/actorassets"
	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/scanner"
	"emby-go/internal/scheduler"
)

func TestActorAssetAPIAndDatabaseRebuild(t *testing.T) {
	app, ts, token := contractEnv(t)
	movies, _, _ := app.db.SearchAll(0, "", "", "title", false, 10, 0)
	movie := movies[0]
	lib, err := app.db.Library(movie.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(movie.NFOPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("<thumb>https://x/a.jpg</thumb>"), nil)
	if err := os.WriteFile(movie.NFOPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.RescanOne(app.db, lib, movie.SourcePath); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 5, 7))); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index" {
			w.Write([]byte(`{"Information":{"Timestamp":1.5},"Content":{"0-Hand-Storage":{"演员甲.jpg":"演员甲.jpg"}}}`))
			return
		}
		if r.URL.Path != "/Content/0-Hand-Storage/演员甲.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Write(buf.Bytes())
	}))
	defer upstream.Close()
	body, _ := json.Marshal(map[string]any{"avatar_source": "gfriends", "gfriends": map[string]any{"index_url": upstream.URL + "/index", "base_url": upstream.URL, "refresh_hours": 24}})
	response, settings := embyCall(t, ts, "PUT", "/api/admin/scrape/settings", token, string(body))
	if response.StatusCode != 200 || settings["configured"] != false || settings["avatar_configured"] != true {
		t.Fatal(response.StatusCode, settings)
	}
	response, _ = embyCall(t, ts, "POST", "/api/admin/scrape/test", token, `{"target":"gfriends"}`)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	before, _ := os.ReadFile(movie.NFOPath)
	if !bytes.Equal(before, raw) {
		t.Fatal("source test modified NFO")
	}
	response, _ = embyCall(t, ts, "POST", "/api/admin/scrape/avatars", token, `{}`)
	if response.StatusCode != 202 {
		t.Fatal(response.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for app.scraping() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if app.scraping() {
		t.Fatal("avatar task did not finish")
	}
	_, progress := embyCall(t, ts, "GET", "/api/admin/scrape/progress", token, "")
	if progress["success"] != float64(1) || progress["failed"] != float64(0) {
		t.Fatal(progress)
	}
	// A scheduled call has its own readiness check and must not require MetaTube.
	assetPath, _, err := app.actorAssets().Resolve("演员甲")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(assetPath); err != nil {
		t.Fatal(err)
	}
	if err := app.runScheduledScrapeAvatars(scheduler.Task{Name: "GFriends", Type: "scrape_avatars"}); err != nil {
		t.Fatal(err)
	}
	_, tag, err := app.actorAssets().Resolve("演员甲")
	if err != nil {
		t.Fatal(err)
	}
	_, detail := embyCall(t, ts, "GET", "/api/admin/items/1/detail", token, "")
	actors, _ := detail["actors"].([]any)
	if len(actors) != 1 || actors[0].(map[string]any)["image_tag"] != tag {
		t.Fatal(detail)
	}
	// Rebuild into a new SQLite database from the same NFO and actor directory.
	rebuilt, err := newApp(config.Config{DBPath: filepath.Join(t.TempDir(), "rebuilt.db"), ActorsDir: app.actorAssets().Dir}, cache.NewMemory(64))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rebuilt.Close)
	if _, err := rebuilt.db.AddLibrary("restored", lib.Path); err != nil {
		t.Fatal(err)
	}
	ts2, token2 := detailServer(t, rebuilt)
	scanOnce(t, ts2, token2)
	_, item := embyCall(t, ts2, "GET", "/Users/1/Items/1", token2, "")
	people := item["People"].([]any)
	if people[0].(map[string]any)["PrimaryImageTag"] != tag {
		t.Fatal(item)
	}
	response, picture := embyRaw(t, ts2, "GET", "/Items/"+entityId("Person", "演员甲")+"/Images/Primary", token2, "")
	if response.StatusCode != 200 || actorassets.Hash(picture)[:16] != tag {
		t.Fatal("restored image", response.StatusCode)
	}
	// Preview never imports or rewrites a source; import respects the existing task gate.
	if !rebuilt.claimNFO("scan") {
		t.Fatal("gate")
	}
	response, _ = embyCall(t, ts2, "POST", "/api/admin/actors/import", token2, `{}`)
	rebuilt.releaseNFO("scan")
	if response.StatusCode != 409 {
		t.Fatal("import ignored gate", response.StatusCode)
	}
	response, _ = embyCall(t, ts2, "POST", "/api/admin/actors/import/preview", token2, `{}`)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	response, invalid := embyCall(t, ts2, "PUT", "/api/admin/scrape/settings", token2, `{"avatar_source":"typo"}`)
	if response.StatusCode != 400 || !strings.Contains(invalid["error"].(string), "来源") {
		t.Fatal(invalid)
	}
}

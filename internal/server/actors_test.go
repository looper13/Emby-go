package server

import (
	"bytes"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"emby-go/internal/actorassets"
	"emby-go/internal/avatar"
)

func TestAdminActorsInventory(t *testing.T) {
	app, ts, token := contractEnv(t)
	if err := app.db.SetActorAvatar("Orphan", "https://example.invalid/orphan.webp", ""); err != nil {
		t.Fatal(err)
	}
	if resp, _ := embyCall(t, ts, "GET", "/api/admin/actors", "", ""); resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
	// A remote NFO URL and a movie poster do not count as a local portrait.
	_, initial := embyCall(t, ts, "GET", "/api/admin/actors", token, "")
	if initial["stats"].(map[string]any)["missing"] != float64(1) {
		t.Fatal(initial)
	}
	if initial["items"].([]any)[0].(map[string]any)["has_image"] != false {
		t.Fatal(initial)
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "one.strm"), "https://media.invalid/one.mp4")
	writeFile(t, filepath.Join(root, "one.nfo"), `<movie><title>One</title><actor><name>演员甲</name></actor><actor><name>Legacy</name></actor><actor><name>New</name></actor></movie>`)
	writeFile(t, filepath.Join(root, "two.strm"), "https://media.invalid/two.mp4")
	writeFile(t, filepath.Join(root, "two.nfo"), `<movie><title>Two</title><actor><name>Legacy</name></actor><actor><name>演员甲</name></actor></movie>`)
	lib, err := app.db.AddLibrary("Actors", root)
	if err != nil {
		t.Fatal(err)
	}
	scanOnce(t, ts, token)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(app.avatarsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(avatar.Path(app.avatarsDir(), "Legacy"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	m, data, err := actorassets.Prepare("New", buf.Bytes(), actorassets.Avatar{Origin: "legacy_local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := app.actorAssets().Stage(m, data, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.actorAssets().Publish(&pending); err != nil {
		t.Fatal(err)
	}
	app.imgMeta.Clear()

	get := func(query string) map[string]any {
		t.Helper()
		resp, body := embyCall(t, ts, "GET", "/api/admin/actors"+query, token, "")
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode, body)
		}
		return body
	}
	all := get("")
	stats := all["stats"].(map[string]any)
	if stats["total"] != float64(3) || stats["ready"] != float64(2) || stats["missing"] != float64(1) {
		t.Fatal(all)
	}
	items := all["items"].([]any)
	if items[0].(map[string]any)["movie_count"] != float64(2) || items[2].(map[string]any)["movie_count"] != float64(3) {
		t.Fatal(items)
	}
	if items[1].(map[string]any)["image_tag"] != m.Avatar.SHA256[:16] {
		t.Fatal(items)
	}
	page := get("?status=ready&limit=1&offset=1")
	if page["total"] != float64(2) || page["items"].([]any)[0].(map[string]any)["name"] != "New" {
		t.Fatal(page)
	}
	missing := get("?status=missing&search=" + url.QueryEscape("演员"))
	if missing["total"] != float64(1) {
		t.Fatal(missing)
	}
	if get("?search=legACY")["total"] != float64(1) || get("?search=%25")["total"] != float64(0) {
		t.Fatal("literal case-insensitive search")
	}
	scoped := get("?library_id=" + strconv.FormatInt(lib.ID, 10) + "&search=" + url.QueryEscape("演员甲"))
	if scoped["items"].([]any)[0].(map[string]any)["movie_count"] != float64(2) || scoped["stats"].(map[string]any)["total"] != float64(3) {
		t.Fatal(scoped)
	}
	if len(get("?offset=999")["items"].([]any)) != 0 || get("?library_id=999")["total"] != float64(0) {
		t.Fatal("empty page/library")
	}
	for _, query := range []string{"?library_id=-1", "?library_id=abc", "?limit=201", "?limit=0", "?offset=-1", "?status=typo"} {
		if resp, _ := embyCall(t, ts, "GET", "/api/admin/actors"+query, token, ""); resp.StatusCode != 400 {
			t.Fatal(query, resp.StatusCode)
		}
	}
	// Read-only listing must not repair an incomplete asset or write NFOs.
	if _, err := app.actorAssets().Pending("New"); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(filepath.Join(root, "one.nfo"))
	if bytes.Contains(contents, []byte("<thumb>")) {
		t.Fatal("listing modified NFO")
	}
}

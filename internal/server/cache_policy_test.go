package server

import (
	"context"
	"emby-go/internal/librarywatch"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestScopedResponsesPreserveOtherLibrariesAndAuthentication(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.strm"), "http://media.test/a.mp4")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>A</title></movie>")
	app, server, token := newProbeTestApp(t, root)
	libs, err := app.db.Libraries()
	if err != nil {
		t.Fatal(err)
	}
	first := libs[0]
	otherRoot := t.TempDir()
	writeFile(t, filepath.Join(otherRoot, "b.strm"), "http://media.test/b.mp4")
	writeFile(t, filepath.Join(otherRoot, "b.nfo"), "<movie><title>B</title></movie>")
	other, err := app.db.AddLibrary("Other", otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.scanLibraries(0); err != nil {
		t.Fatal(err)
	}
	aID, err := app.db.MovieIDByPath(filepath.Join(root, "a.strm"))
	if err != nil {
		t.Fatal(err)
	}
	bID, err := app.db.MovieIDByPath(filepath.Join(otherRoot, "b.strm"))
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, want int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("X-Emby-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, body)
		}
		return body
	}
	detailA := "/Users/1/Items/" + strconv.FormatInt(aID, 10)
	detailB := "/Users/1/Items/" + strconv.FormatInt(bID, 10)
	listA := "/Users/1/Items?ParentId=" + externalLibraryID(first.ID)
	listB := "/Users/1/Items?ParentId=" + externalLibraryID(other.ID)
	all := "/Users/1/Items"
	adminA := "/api/admin/items?library_id=" + strconv.FormatInt(first.ID, 10)
	adminB := "/api/admin/items?library_id=" + strconv.FormatInt(other.ID, 10)
	for _, path := range []string{detailA, detailB, listA, listB, all, adminA, adminB} {
		get(path, 200)
		get(path, 200)
	}
	stats := app.cache.Stats()
	if stats["item"].Loads != 2 || stats["items"].Loads != 3 || stats["adminitems"].Loads != 2 {
		t.Fatalf("warmup stats: %+v", stats)
	}
	// Keep unrelated decoded images and NFO data through a local file event.
	app.imgThumb.Set("t:"+filepath.Join(otherRoot, "poster.jpg")+":v0:old:10x10:q80", []byte("keep"), time.Hour)
	app.nfoMu.Lock()
	app.nfos[filepath.Join(otherRoot, "b.nfo")+"|tag"] = nfoCacheEntry{}
	app.nfoMu.Unlock()
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>Updated A</title></movie>")
	if err := app.refreshLibraryChanges(context.Background(), first, []librarywatch.Change{{Path: filepath.Join(root, "a.nfo")}}); err != nil {
		t.Fatal(err)
	}
	before := app.cache.Stats()
	get(detailB, 200)
	get(listB, 200)
	get(adminB, 200)
	after := app.cache.Stats()
	if after["item"].Loads != before["item"].Loads || after["items"].Loads != before["items"].Loads || after["adminitems"].Loads != before["adminitems"].Loads {
		t.Fatal("unrelated library reloaded")
	}
	if body := get(detailA, 200); body["Name"] != "Updated A" {
		t.Fatalf("stale detail: %v", body)
	}
	get(listA, 200)
	get(all, 200)
	get(adminA, 200)
	after = app.cache.Stats()
	if after["items"].Loads != before["items"].Loads+2 || after["adminitems"].Loads != before["adminitems"].Loads+1 {
		t.Fatalf("changed scopes did not reload: %+v", after)
	}
	if _, ok := app.cache.Get("token:" + token); !ok {
		t.Fatal("token invalidated")
	}
	if _, ok := app.imgThumb.Get("t:" + filepath.Join(otherRoot, "poster.jpg") + ":v0:old:10x10:q80"); !ok {
		t.Fatal("unrelated thumbnail evicted")
	}
	app.nfoMu.Lock()
	_, nfoKept := app.nfos[filepath.Join(otherRoot, "b.nfo")+"|tag"]
	app.nfoMu.Unlock()
	if !nfoKept {
		t.Fatal("unrelated NFO evicted")
	}
	// Failed scrape status changes only the affected library's admin cache.
	before = app.cache.Stats()
	if err := app.db.SetScrapeResult(bID, "failed"); err != nil {
		t.Fatal(err)
	}
	get(adminA, 200)
	get(adminB, 200)
	after = app.cache.Stats()
	if after["adminitems"].Loads != before["adminitems"].Loads+1 {
		t.Fatal("scrape result scope incorrect")
	}
	// User changes invalidate affected detail/list immediately, progress alone
	// invalidates detail while list progress remains bounded by its short TTL.
	if err := app.db.SetFavorite(aID, true); err != nil {
		t.Fatal(err)
	}
	app.userDataWrite(first.ID)
	if body := get(detailA, 200); body["UserData"].(map[string]any)["IsFavorite"] != true {
		t.Fatal("favorite detail stale")
	}
	get(adminA, 200)
	if err := app.db.SavePlayback(aID, 123456, 1, "", 0); err != nil {
		t.Fatal(err)
	}
	if body := get(detailA, 200); body["UserData"].(map[string]any)["PlaybackPositionTicks"] != float64(123456) {
		t.Fatal("playback detail stale")
	}
	if err := app.db.DeleteMovie(aID); err != nil {
		t.Fatal(err)
	}
	get(detailA, 404)
	if err := app.db.DeleteLibrary(other.ID); err != nil {
		t.Fatal(err)
	}
	get(detailB, 404)
	settings := get("/api/admin/settings", 200)
	if settings["cache_stats"] == nil {
		t.Fatal("metrics missing")
	}
}
func TestDiskInvalidationHonorsDirectoryBoundaries(t *testing.T) {
	root := t.TempDir()
	app, _, _ := newProbeTestApp(t, root)
	a := filepath.Join(root, "movie")
	b := filepath.Join(root, "movie-other")
	aPath, bPath := filepath.Join(a, "poster.jpg"), filepath.Join(b, "poster.jpg")
	app.tags[aPath] = tagEntry{tag: "a"}
	app.tags[bPath] = tagEntry{tag: "b"}
	app.nfos[filepath.Join(a, "a.nfo")+"|tag"] = nfoCacheEntry{}
	app.nfos[filepath.Join(b, "b.nfo")+"|tag"] = nfoCacheEntry{}
	aKey, bKey := "t:"+aPath+":v0:v99:10x10:q80", "t:"+bPath+":v0:v99:10x10:q80"
	app.imgThumb.Set(aKey, []byte("a"), time.Hour)
	app.imgThumb.Set(bKey, []byte("b"), time.Hour)
	app.invalidateDiskPaths([]string{a}, true)
	if _, ok := app.tags[aPath]; ok {
		t.Fatal("changed tag retained")
	}
	if _, ok := app.tags[bPath]; !ok {
		t.Fatal("sibling tag evicted")
	}
	if _, ok := app.imgThumb.Get(aKey); ok {
		t.Fatal("changed thumbnail retained")
	}
	if _, ok := app.imgThumb.Get(bKey); !ok {
		t.Fatal("sibling thumbnail evicted")
	}
	if len(app.nfos) != 1 {
		t.Fatal("incorrect NFO scope")
	}
}

func TestDiskInvalidationMatchesPathCase(t *testing.T) {
	root := t.TempDir()
	app, _, _ := newProbeTestApp(t, root)
	path := filepath.Join(root, "Poster.jpg")
	key := "t:" + path + ":v0:v99:10x10:q80"
	app.imgThumb.Set(key, []byte("old"), time.Hour)
	app.tags[path] = tagEntry{tag: "old"}
	before := app.diskVersion(path)
	changed := path
	if runtime.GOOS == "windows" {
		changed = strings.ToUpper(path)
	}
	app.invalidateLibraryChanges([]librarywatch.Change{{Path: changed}, {Path: filepath.Join(root, "other.jpg")}})
	if _, ok := app.imgThumb.Get(key); ok {
		t.Fatal("changed thumbnail retained")
	}
	if _, ok := app.tags[path]; ok {
		t.Fatal("changed tag retained")
	}
	if app.diskVersion(path) == before {
		t.Fatal("path version not advanced")
	}
}

func TestRootArtRefreshAndPreservedTimestamp(t *testing.T) {
	root := t.TempDir()
	imagePath := filepath.Join(root, "poster.jpg")
	writeJPEGImage(t, imagePath, 80, 40)
	app, _, _ := newProbeTestApp(t, root)
	libs, err := app.db.Libraries()
	if err != nil {
		t.Fatal(err)
	}
	library := libs[0]
	if app.libraryCoverPath(library) != imagePath || app.cachedCoverRatio(imagePath) != 2 {
		t.Fatal("initial root art incorrect")
	}
	before, err := os.Stat(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	oldDiskVersion := app.diskVersion(imagePath)
	writeJPEGImage(t, imagePath, 40, 80)
	if err := os.Chtimes(imagePath, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: imagePath}}); err != nil {
		t.Fatal(err)
	}
	if app.diskVersion(imagePath) == oldDiskVersion || app.cachedCoverRatio(imagePath) != 0.5 {
		t.Fatal("preserved timestamp retained stale aspect ratio")
	}
	preferred := filepath.Join(root, "poster.webp")
	writeFile(t, preferred, "preferred root art")
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: preferred}}); err != nil {
		t.Fatal(err)
	}
	if app.libraryCoverPath(library) != preferred {
		t.Fatal("root cover path stayed stale without indexed movies")
	}
}
func TestUnchangedManualScanPreservesDiskAndDetailCaches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.strm"), "http://media.test/a.mp4")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>A</title></movie>")
	app, _, _ := newProbeTestApp(t, root)
	libs, err := app.db.Libraries()
	if err != nil {
		t.Fatal(err)
	}
	id, err := app.db.MovieIDByPath(filepath.Join(root, "a.strm"))
	if err != nil {
		t.Fatal(err)
	}
	movieVersion, scope := app.db.MovieVersion(id), app.cacheScope(libs[0].ID)
	imagePath := filepath.Join(root, "poster.jpg")
	app.diskVersion(imagePath)
	key := "t:" + imagePath + ":v0:old:10x10:q80"
	app.imgThumb.Set(key, []byte("keep"), time.Hour)
	app.tags[imagePath] = tagEntry{tag: "keep"}
	if _, err := app.scanLibraries(libs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.imgThumb.Get(key); !ok {
		t.Fatal("unchanged scan evicted thumbnail")
	}
	if _, ok := app.tags[imagePath]; !ok {
		t.Fatal("unchanged scan evicted tag")
	}
	if app.db.MovieVersion(id) != movieVersion || app.cacheScope(libs[0].ID) != scope {
		t.Fatal("unchanged scan invalidated response scopes")
	}
}

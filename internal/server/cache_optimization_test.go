package server

import (
	"context"
	"emby-go/internal/cache"
	"emby-go/internal/librarywatch"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type detailLifetimeCache struct {
	cache.Cache
	lifetimes []time.Duration
}

func (c *detailLifetimeCache) Set(key string, body []byte, ttl time.Duration) {
	if strings.Contains(key, ":item:") {
		c.lifetimes = append(c.lifetimes, ttl)
	}
	c.Cache.Set(key, body, ttl)
}

func TestPreservedTimestampChangesClientImageValidator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "poster.jpg")
	writeJPEGImage(t, path, 80, 40)
	app, _, _ := newProbeTestApp(t, root)
	libs, err := app.db.Libraries()
	if err != nil {
		t.Fatal(err)
	}
	request := func(etag string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/image?maxWidth=32", nil)
		c.Request.Header.Set("If-None-Match", etag)
		app.serveImage(c, path)
		c.Writer.WriteHeaderNow()
		return recorder
	}
	first := request("")
	if first.Code != 200 || first.Header().Get("ETag") == "" {
		t.Fatal("initial image response invalid")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeJPEGImage(t, path, 40, 80)
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), libs[0], []librarywatch.Change{{Path: path}}); err != nil {
		t.Fatal(err)
	}
	fresh := request(first.Header().Get("ETag"))
	if fresh.Code != 200 || fresh.Header().Get("ETag") == first.Header().Get("ETag") || fresh.Body.String() == first.Body.String() {
		t.Fatal("changed image reused its old client validator or thumbnail")
	}
	if request(fresh.Header().Get("ETag")).Code != 304 {
		t.Fatal("unchanged image failed conditional revalidation")
	}
}

func TestDirectoryInvalidationChangesClientImageValidator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "poster.jpg")
	writeJPEGImage(t, path, 80, 40)
	app, _, _ := newProbeTestApp(t, root)
	request := func(etag string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/image?maxWidth=32", nil)
		c.Request.Header.Set("If-None-Match", etag)
		app.serveImage(c, path)
		c.Writer.WriteHeaderNow()
		return recorder
	}
	first := request("")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// 一个尚未完成读盘的请求只有版本快照，还没有 tag/NFO 缓存条目。
	inflightPath := filepath.Join(root, "nested", "movie.nfo")
	inflightVersion := app.diskVersion(inflightPath)
	sibling := filepath.Join(root+"-sibling", "poster.jpg")
	siblingVersion := app.diskVersion(sibling)
	writeJPEGImage(t, path, 40, 80)
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	app.invalidateLibraryChanges([]librarywatch.Change{{Path: root, Directory: true}})
	fresh := request(first.Header().Get("ETag"))
	if first.Code != 200 || fresh.Code != 200 || fresh.Header().Get("ETag") == first.Header().Get("ETag") || fresh.Body.String() == first.Body.String() {
		t.Fatal("directory refresh reused old validator or thumbnail")
	}
	if app.diskVersion(inflightPath) == inflightVersion {
		t.Fatal("directory refresh did not invalidate an in-flight child read")
	}
	if app.diskVersion(sibling) != siblingVersion {
		t.Fatal("directory refresh invalidated sibling directory")
	}
	if request(fresh.Header().Get("ETag")).Code != 304 {
		t.Fatal("unchanged image failed conditional revalidation")
	}
}

func TestPlaybackPreservesImageMetadataAndRefreshesDetail(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.strm")
	writeFile(t, source, "http://media.test/a.mp4")
	writeFile(t, filepath.Join(root, "a.nfo"), "<movie><title>A</title></movie>")
	app, _, _ := newProbeTestApp(t, root)
	id, err := app.db.MovieIDByPath(source)
	if err != nil {
		t.Fatal(err)
	}
	backend := &detailLifetimeCache{Cache: cache.NewMemory(256)}
	app.cache = cache.NewManaged(backend)
	if _, ok := app.cachedMovie(id); !ok {
		t.Fatal("missing cached movie")
	}
	metadataVersion := app.db.MovieMetadataVersion(id)
	imageKey := "m:" + metadataVersion + ":" + strconv.FormatInt(id, 10)
	// A sentinel distinguishes reuse from fetching the unchanged database row.
	cached, _ := app.cachedMovie(id)
	cached.Title = "cached image metadata"
	raw, _ := json.Marshal(cached)
	app.imgMeta.Set(imageKey, raw, time.Minute)
	for index := 1; index <= 20; index++ {
		if err := app.db.SavePlayback(id, int64(index), 0, "", -1); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/Items/"+strconv.FormatInt(id, 10), nil)
		c.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}
		app.item(c)
		var detail map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 200 || detail["UserData"].(map[string]any)["PlaybackPositionTicks"] != float64(index) {
			t.Fatal("playback detail is stale")
		}
		if movie, ok := app.cachedMovie(id); !ok || movie.Title != "cached image metadata" || app.db.MovieMetadataVersion(id) != metadataVersion {
			t.Fatal("playback evicted image metadata")
		}
	}
	if len(backend.lifetimes) != 20 {
		t.Fatal("expected each changed progress value to refresh detail")
	}
	for _, ttl := range backend.lifetimes {
		if ttl <= 0 || ttl > time.Minute {
			t.Fatal("superseded progress details persist longer than one minute")
		}
	}
}

func TestScrapeRefreshInvalidatesSharedDependenciesOncePerPhase(t *testing.T) {
	root := t.TempDir()
	poster := filepath.Join(root, "poster.jpg")
	writeJPEGImage(t, poster, 80, 40)
	for _, name := range []string{"a", "b", "c", "other"} {
		writeFile(t, filepath.Join(root, name+".strm"), "http://media.test/movie.mp4")
		writeFile(t, filepath.Join(root, name+".nfo"), "<movie><title>Old</title></movie>")
	}
	app, _, _ := newProbeTestApp(t, root)
	var ids []int64
	for _, name := range []string{"a", "b", "c"} {
		id, err := app.db.MovieIDByPath(filepath.Join(root, name+".strm"))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		writeFile(t, filepath.Join(root, name+".nfo"), "<movie><title>Updated</title></movie>")
	}
	writeFile(t, filepath.Join(root, "other.nfo"), "<movie><title>Leave untouched</title></movie>")
	private := filepath.Join(root, "a-poster.webp")
	writeFile(t, private, "new private poster")
	before := app.diskVersion(poster)
	ids = append(ids, ids[0])
	if err := app.refreshScrapedMovies(ids); err != nil {
		t.Fatal(err)
	}
	if app.diskVersion(poster) != before+2 || app.diskVersion(private) == 0 {
		t.Fatal("shared paths invalidated repeatedly or newly selected art retained")
	}
	for _, id := range ids {
		movie, err := app.db.Movie(id)
		if err != nil || movie.Title != "Updated" {
			t.Fatalf("scraped movie not refreshed: %+v %v", movie, err)
		}
	}
	otherID, _ := app.db.MovieIDByPath(filepath.Join(root, "other.strm"))
	other, err := app.db.Movie(otherID)
	if err != nil || other.Title != "Old" {
		t.Fatal("scrape refresh reindexed its unrelated sibling")
	}
}

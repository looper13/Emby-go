package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

func TestR18LargeCoverCachingRestartSharingAndAccess(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	random := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32()), 255
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil || source.Len() <= r18ImageCacheLimit {
		t.Fatal("fixture must exceed the old 512 KiB cache cutoff", source.Len(), err)
	}
	var covers, galleries atomic.Int64
	var unavailable atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for _, key := range []string{"Cookie", "X-Emby-Token", "Authorization"} {
			if req.Header.Get(key) != "" {
				t.Error("forwarded client credential", key)
			}
		}
		if req.URL.Path == "/gallery" {
			galleries.Add(1)
		} else {
			covers.Add(1)
		}
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write(source.Bytes())
	}))
	defer upstream.Close()
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, _ := a.db.CreateR18Library("Cached covers", r18.Config{Host: "db.invalid", Database: "catalog", Username: "reader"})
	v := r18.Video{ID: r18.EncodeID("cached001", "mono"), Number: "CACHE-001", CoverURL: upstream.URL + "/cover", Images: []string{upstream.URL + "/gallery"}}
	fake := &fakeR18Catalog{videos: []r18.Video{v}}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	a.r18ImageHTTP = upstream.Client()
	a.r18Presentation(l, v)
	web := "/api/r18/libraries/" + strconv.FormatInt(l.ID, 10) + "/videos/" + v.ID + "/images/"
	emby := "/Items/" + r18ItemID(l.ID, v.ID) + "/Images/Primary"
	call := func(server *httptest.Server, method, path, auth, tag string, want int) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, nil)
		if auth != "" {
			req.Header.Set("X-Emby-Token", auth)
		}
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return nil, nil
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Errorf("%s %s: %d, want %d", method, path, resp.StatusCode, want)
		}
		return resp, data
	}
	call(server, "GET", emby, "", "", 401)
	if _, err := os.Stat(r18CoverCacheDirectory(a.configSnapshot().DBPath, a.serverID)); !os.IsNotExist(err) {
		t.Fatal("unauthorized request initialized disk cache", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := web + "poster"
			if i%2 == 0 {
				path = emby
			}
			call(server, "GET", path, token, "", 200)
		}(i)
	}
	wg.Wait()
	resp, poster := call(server, "GET", web+"poster", token, "", 200)
	config, _, err := image.DecodeConfig(bytes.NewReader(poster))
	if err != nil || config.Width != 320 || config.Height != 480 || resp.Header.Get("Content-Type") != "image/webp" || covers.Load() != 1 || galleries.Load() != 0 {
		t.Fatal("web/Emby default posters must share one source load, without gallery prefetch", config, covers.Load(), galleries.Load(), err)
	}
	_, samePoster := call(server, "GET", emby, token, "", 200)
	if !bytes.Equal(poster, samePoster) {
		t.Fatal("web and Emby defaults did not share the same poster")
	}
	_, resized := call(server, "GET", emby+"?maxWidth=100", token, "", 200)
	config, _, err = image.DecodeConfig(bytes.NewReader(resized))
	if err != nil || config.Width != 100 || config.Height != 150 || covers.Load() != 1 {
		t.Fatal("new dimensions redownloaded an oversized original", config, covers.Load(), err)
	}
	_, original := call(server, "GET", web+"cover", token, "", 200)
	if !bytes.Equal(original, source.Bytes()) || covers.Load() != 1 {
		t.Fatal("full cover must reuse its bounded disk cache")
	}
	unavailable.Store(true)
	a.r18CoverMemory.Backend().Clear()
	// Even with the original removed, a derived hit must not read/fetch it.
	sum := sha256.Sum256([]byte(r18VideoCacheKey(l, v.ID) + ":" + v.CoverURL))
	sourceKey := "r18cover:v2:" + hex.EncodeToString(sum[:])
	if err := a.r18CoverFiles.Delete(sourceKey + ":original"); err != nil {
		t.Fatal(err)
	}
	call(server, "HEAD", emby, token, "", 200)
	call(server, "GET", web+"poster", token, resp.Header.Get("ETag"), 304)
	if covers.Load() != 1 {
		t.Fatal("disk-derived hit fetched the original")
	}
	cfg := a.configSnapshot()
	server.Close()
	a.Close()
	restarted, err := newApp(cfg, cache.NewMemory(256))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	restarted.r18ImageHTTP = upstream.Client()
	nextServer := httptest.NewServer(restarted.Handler())
	defer nextServer.Close()
	_, afterRestart := call(nextServer, "GET", web+"poster", token, "", 200)
	if !bytes.Equal(afterRestart, poster) || covers.Load() != 1 {
		t.Fatal("restart lost persistent cover cache", covers.Load())
	}
	user, _ := restarted.db.CreateUser("cover-reader", "password-1234")
	_ = restarted.db.SaveUserToken("cover-reader-token", user.ID)
	_ = restarted.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	call(nextServer, "GET", emby, "cover-reader-token", "", 200)
	_ = restarted.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	call(nextServer, "HEAD", emby, "cover-reader-token", resp.Header.Get("ETag"), 404)
	call(nextServer, "GET", web+"poster", "cover-reader-token", resp.Header.Get("ETag"), 404)
	if covers.Load() != 1 || galleries.Load() != 0 {
		t.Fatal("revoked user or gallery preload contacted upstream")
	}
	unavailable.Store(false)
	v.CoverURL = upstream.URL + "/changed-cover"
	fake.videos[0] = v
	restarted.r18Presentation(l, v)
	call(nextServer, "GET", web+"poster", token, "", 200)
	if covers.Load() != 2 {
		t.Fatal("changed source URL reused an old cache identity")
	}
	call(nextServer, "GET", web+"0", token, "", 200)
	if galleries.Load() != 1 {
		t.Fatal("gallery should only load on its own detail request")
	}
}

func TestR18CoverCacheExpiryAndDiskFailureFallback(t *testing.T) {
	a, _, _ := newProbeTestApp(t, t.TempDir())
	a.initR18CoverCache()
	blocked := filepath.Join(t.TempDir(), "blocked")
	_ = os.WriteFile(blocked, []byte("keep"), 0600)
	a.r18CoverFiles = cache.NewFiles(blocked, 4096, 2, 1024)
	key := "expiry-fixture"
	a.r18CoverMemory.Set(key, coverPacket(cache.FileValue{Data: []byte("old"), Expires: time.Now().Add(-time.Hour)}), time.Hour)
	loads := 0
	loader := func() (cache.FileValue, error) {
		loads++
		return cache.FileValue{Data: []byte("new"), Expires: time.Now().Add(time.Hour)}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := a.loadR18Cover(context.Background(), key, loader)
		if err != nil || string(got.Data) != "new" {
			t.Fatal("disk failure should not fail cover delivery", got, err)
		}
	}
	if loads != 1 {
		t.Fatal("expired memory was used, or disk failure disabled hot caching", loads)
	}
	if raw, _ := os.ReadFile(blocked); string(raw) != "keep" {
		t.Fatal("fallback touched an unrelated file")
	}
}

func TestR18CoverDerivedKeepsOriginalExpiry(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, err := a.db.CreateR18Library("Cover freshness", r18.Config{Host: "db.invalid", Database: "catalog", Username: "reader"})
	if err != nil {
		t.Fatal(err)
	}
	v := r18.Video{ID: r18.EncodeID("freshness001", "mono"), CoverURL: "https://images.invalid/cover.png"}
	a.r18Presentation(l, v)
	a.initR18CoverCache()
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 80, 120))); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(r18VideoCacheKey(l, v.ID) + ":" + v.CoverURL))
	key := "r18cover:v2:" + hex.EncodeToString(sum[:])
	expires := time.Now().Add(time.Hour)
	if err := a.r18CoverFiles.Set(key+":original", cache.FileValue{Data: packR18Image("image/png", source.Bytes()), Expires: expires}); err != nil {
		t.Fatal(err)
	}
	path := server.URL + "/Items/" + r18ItemID(l.ID, v.ID) + "/Images/Primary?maxWidth=40"
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatal("cached original did not produce a poster", resp.StatusCode)
		}
		value, hit, err := a.r18CoverFiles.Get(key + ":poster:40x0:82")
		if err != nil || !hit || !value.Expires.Equal(expires) {
			t.Fatal("derived poster extended its original's freshness", value.Expires, expires, hit, err)
		}
		a.r18CoverMemory.Backend().Clear()
	}
}

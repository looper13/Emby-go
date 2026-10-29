package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

func TestR18ImageProxyPresentationAuthCacheAndFailures(t *testing.T) {
	var pngImage bytes.Buffer
	_ = png.Encode(&pngImage, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		for _, header := range []string{"Cookie", "Authorization", "X-API-Key", "X-Emby-Token", "X-MediaBrowser-Token", "Referer"} {
			if r.Header.Get(header) != "" {
				t.Errorf("forwarded credential header %s", header)
			}
		}
		switch r.URL.Path {
		case "/cover.png", "/gallery.png":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pngImage.Bytes())
		case "/html":
			_, _ = w.Write([]byte("<html>not an image</html>"))
		case "/large":
			_, _ = w.Write(bytes.Repeat([]byte{'a'}, r18ImageLimit+1))
		default:
			w.WriteHeader(503)
		}
	}))
	defer upstream.Close()
	a, server, token := newProbeTestApp(t, t.TempDir())
	// Only this fixture uses loopback; production transport rejects private targets.
	a.r18ImageHTTP = upstream.Client()
	lib, err := a.db.CreateR18Library("Images", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("images", "digital"), ContentID: "images", ServiceCode: "digital", CoverURL: upstream.URL + "/cover.png", Images: []string{upstream.URL + "/gallery.png"}}}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	base := "/api/r18/libraries/" + strconv.FormatInt(lib.ID, 10) + "/videos/" + fake.video.ID
	call := func(method, path, auth, etag string, want int) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), method, server.URL+path, nil)
		if auth != "" {
			req.AddCookie(&http.Cookie{Name: "emby_session", Value: auth})
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d (%s)", method, path, resp.StatusCode, body)
		}
		return resp, body
	}
	_, body := call("GET", base, token, "", 200)
	var detail struct {
		Video r18.Video `json:"video"`
	}
	_ = json.Unmarshal(body, &detail)
	if detail.Video.CoverURL != base+"/images/poster" || len(detail.Video.Images) != 1 || detail.Video.Images[0] != base+"/images/0" || bytes.Contains(body, []byte(upstream.URL)) {
		t.Fatal("direct image URL exposed", string(body))
	}
	reads := fake.reads
	call("GET", base+"/images/cover", "", "", 401)
	user, _ := a.db.CreateUser("image-viewer", "password-1234")
	_ = a.db.SaveUserToken("image-user", user.ID)
	call("GET", base+"/images/cover", "image-user", "", 404)
	if hits != 0 || fake.reads != reads {
		t.Fatal("unauthorized request contacted source")
	}
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}})
	resp, body := call("GET", base+"/images/cover?url=https://unrelated.example/", "image-user", "", 200)
	if !bytes.Equal(body, pngImage.Bytes()) || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("Cache-Control") != "private, no-cache" || resp.Header.Get("X-Content-Type-Options") != "nosniff" || fake.reads != reads {
		t.Fatal("invalid proxy response/metadata reuse")
	}
	etag := resp.Header.Get("ETag")
	if etag == "" || !strings.Contains(resp.Header.Get("Vary"), "Cookie") {
		t.Fatal("missing private image validators")
	}
	call("GET", base+"/images/cover", token, etag, 304)
	_, body = call("HEAD", base+"/images/cover", token, "", 200)
	if len(body) != 0 || hits != 1 {
		t.Fatal("cache/HEAD repeated upstream request")
	}
	call("GET", base+"/images/0", token, "", 200)
	call("GET", base+"/images/1", token, "", 404)
	call("GET", base+"/images/999", token, "", 404)
	if hits != 2 {
		t.Fatal("invalid image index reached upstream")
	}
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	call("GET", base+"/images/cover", "image-user", etag, 404)
	if hits != 2 {
		t.Fatal("warm cache bypassed revoked access")
	}
	// JavDB 剧照数量不再受旧数据库首尾两张的限制。
	fake.video.Images = []string{upstream.URL + "/gallery.png", upstream.URL + "/gallery.png?index=1", upstream.URL + "/gallery.png?index=2"}
	a.r18Presentation(lib, fake.video)
	call("GET", base+"/images/2", token, "", 200)
	before := hits
	for _, index := range []string{"3", "99", "100", "-1", "01"} {
		call("GET", base+"/images/"+index, token, "", 404)
	}
	if hits != before {
		t.Fatal("不存在或非法剧照索引触发了上游请求")
	}
	for _, path := range []string{"/html", "/large", "/unavailable"} {
		fake.video.CoverURL = upstream.URL + path
		a.r18Presentation(lib, fake.video)
		before := hits
		resp, _ = call("GET", base+"/images/cover", token, "", 502)
		call("GET", base+"/images/cover", token, "", 502)
		if hits != before+2 || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("failed image cached")
		}
	}
}

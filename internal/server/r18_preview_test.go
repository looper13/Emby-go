package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

const previewFixtureMaster = "https://manifest.example/master.m3u8?signature=master-signed"

type r18PreviewTestTransport func(*http.Request) (*http.Response, error)

func (f r18PreviewTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type r18PreviewTestRequest struct {
	host, path, query, byteRange string
}

type r18PreviewFixture struct {
	app      *App
	server   *httptest.Server
	token    string
	library  store.Library
	fake     *fakeR18
	item     string
	mu       sync.Mutex
	requests []r18PreviewTestRequest
	respond  r18PreviewTestTransport
}

func previewFixtureResponse(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}},
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func newR18PreviewFixture(t *testing.T) *r18PreviewFixture {
	t.Helper()
	f := &r18PreviewFixture{}
	f.app, f.server, f.token = newProbeTestApp(t, t.TempDir())
	var err error
	f.library, err = f.app.db.CreateR18Library("Preview", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-api-key"})
	if err != nil {
		t.Fatal(err)
	}
	f.fake = &fakeR18{video: r18.Video{ID: r18.EncodeID("preview001", "javdb"), ContentID: "preview001", ServiceCode: "javdb",
		Number: "PREVIEW-001", Title: "Preview", Tags: []string{"剧情", "Fixture"}, HasPreview: true, PreviewURL: previewFixtureMaster, Images: []string{}}}
	f.app.r18Open = func(r18.Config) (r18Source, error) { return f.fake, nil }
	f.item = r18PreviewBase(f.library, f.fake.video.ID)
	f.respond = func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/master.m3u8":
			return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nvariants/low.m3u8?signature=nested%2Bsig\n")), nil
		case "/variants/low.m3u8":
			return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", []byte("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MAP:URI=\"https://assets.example/init.mp4?signature=map%2Bsig\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example/license.key?signature=key-signed\",IV=0x0001\n#EXTINF:4,\nhttps://media.example/segment.ts?signature=segment%2Bsig\n#EXT-X-ENDLIST\n")), nil
		case "/license.key":
			return previewFixtureResponse(req, 200, "application/octet-stream", []byte("0123456789abcdef")), nil
		case "/init.mp4":
			return previewFixtureResponse(req, 200, "application/mp4", []byte("fixture-init")), nil
		case "/segment.ts":
			body := []byte("0123456789abcdef")
			status, contentRange := 200, ""
			switch req.Header.Get("Range") {
			case "":
			case "bytes=2-5":
				status, contentRange, body = 206, "bytes 2-5/16", body[2:6]
			case "bytes=99-":
				status, contentRange, body = 416, "bytes */16", nil
			default:
				t.Errorf("未预期的分片 Range：%s", req.Header.Get("Range"))
			}
			resp := previewFixtureResponse(req, status, "video/mp2t", body)
			resp.Header.Set("Content-Range", contentRange)
			resp.Header.Set("Accept-Ranges", "bytes")
			resp.Header.Set("ETag", `"fixture-etag"`)
			resp.Header.Set("Set-Cookie", "upstream-private-cookie=fixture")
			return resp, nil
		default:
			t.Errorf("出现未预期的公网资源请求：%s", req.URL.Path)
			return previewFixtureResponse(req, 404, "text/plain", nil), nil
		}
	}
	previous := r18PreviewHTTP
	r18PreviewHTTP = &http.Client{Transport: r18PreviewTestTransport(func(req *http.Request) (*http.Response, error) {
		for _, header := range []string{"X-API-Key", "Cookie", "Authorization", "X-Emby-Token", "X-MediaBrowser-Token", "X-Emby-Authorization", "Referer"} {
			if req.Header.Get(header) != "" {
				t.Errorf("公网预览收到了浏览器或 API 凭据：%s", header)
			}
		}
		if req.Method != http.MethodGet || req.Header.Get("Accept-Encoding") != "identity" {
			t.Error("公网预览未使用固定 GET 或禁用压缩")
		}
		f.mu.Lock()
		f.requests = append(f.requests, r18PreviewTestRequest{req.URL.Hostname(), req.URL.Path, req.URL.RawQuery, req.Header.Get("Range")})
		f.mu.Unlock()
		return f.respond(req)
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() { r18PreviewHTTP = previous })
	return f
}

func (f *r18PreviewFixture) calls() []r18PreviewTestRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]r18PreviewTestRequest(nil), f.requests...)
}

func (f *r18PreviewFixture) call(t *testing.T, method, path, token, byteRange string, want int) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
		req.AddCookie(&http.Cookie{Name: "emby_session", Value: token})
	}
	req.AddCookie(&http.Cookie{Name: "private-browser-cookie", Value: "browser-secret"})
	req.Header.Set("Authorization", "Bearer browser-auth-secret")
	req.Header.Set("X-API-Key", "browser-api-secret")
	req.Header.Set("Referer", "https://browser.invalid/private")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHLSPlaylistBytes+1))
	if err != nil || resp.StatusCode != want || len(body) > maxHLSPlaylistBytes {
		prefix := body
		if len(prefix) > 256 {
			prefix = prefix[:256]
		}
		t.Fatalf("%s %s：状态 %d，预期 %d，读取错误 %v，响应前缀 %s", method, path, resp.StatusCode, want, err, prefix)
	}
	return resp, body
}

func previewFixtureURIs(t *testing.T, body []byte) []string {
	t.Helper()
	if !bytes.HasPrefix(body, []byte("#EXTM3U")) {
		t.Fatal("第一方预览不是 HLS 清单")
	}
	uris := []string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			uris = append(uris, line)
		}
		for _, match := range hlsURIAttribute.FindAllStringSubmatch(line, -1) {
			uris = append(uris, match[1])
		}
	}
	for _, uri := range uris {
		if !strings.HasPrefix(uri, "/api/r18/libraries/") || !strings.Contains(uri, "/preview/") || strings.ContainsAny(uri, "?#") {
			t.Fatal("清单保留了公网或带签名的资源地址")
		}
	}
	return uris
}

func previewFixtureSafeResponse(t *testing.T, body []byte) {
	t.Helper()
	for _, forbidden := range []string{"manifest.example", "assets.example", "keys.example", "media.example", "master-signed", "nested%2Bsig", "map%2Bsig", "key-signed", "segment%2Bsig", "fixture-api-key", "browser-auth-secret", "browser-secret", "browser-api-secret"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatal("预览响应回显了原地址、签名或凭据")
		}
	}
}

func TestR18PreviewPresentationAndHLSChainRanges(t *testing.T) {
	f := newR18PreviewFixture(t)
	_, detail := f.call(t, "GET", f.item, f.token, "", 200)
	var parsed struct {
		Video r18.Video `json:"video"`
	}
	if err := json.Unmarshal(detail, &parsed); err != nil || parsed.Video.PreviewURL != f.item+"/preview.m3u8" || !parsed.Video.HasPreview || !reflect.DeepEqual(parsed.Video.Tags, f.fake.video.Tags) {
		t.Fatal("详情预览或影片标签契约错误", err)
	}
	previewFixtureSafeResponse(t, detail)
	listPath := "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/videos"
	_, listing := f.call(t, "GET", listPath, f.token, "", 200)
	previewFixtureSafeResponse(t, listing)
	var page struct {
		Items []r18.Video `json:"items"`
	}
	if err := json.Unmarshal(listing, &page); err != nil || len(page.Items) != 1 || !page.Items[0].HasPreview || page.Items[0].PreviewURL != f.item+"/preview.m3u8" || !reflect.DeepEqual(page.Items[0].Tags, f.fake.video.Tags) {
		t.Fatal("列表预览或影片标签契约错误", err)
	}
	if len(f.calls()) != 0 {
		t.Fatal("查看详情或列表提前获取了公网预览")
	}
	reads := f.fake.reads
	_, master := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "bytes=2-5", 200)
	previewFixtureSafeResponse(t, master)
	masterURIs := previewFixtureURIs(t, master)
	if len(masterURIs) != 1 || len(f.calls()) != 1 || f.fake.reads != reads {
		t.Fatal("点击预览未复用完整详情，或预取了子清单")
	}
	_, nested := f.call(t, "GET", masterURIs[0], f.token, "bytes=2-5", 200)
	previewFixtureSafeResponse(t, nested)
	uris := previewFixtureURIs(t, nested)
	if len(uris) != 3 || len(f.calls()) != 2 {
		t.Fatal("子清单没有改写 MAP、KEY、TS，或提前预取了资产")
	}
	for i, wantBody := range []string{"fixture-init", "0123456789abcdef", "0123456789abcdef"} {
		_, body := f.call(t, "GET", uris[i], f.token, "", 200)
		if string(body) != wantBody {
			t.Fatal("跨域预览资产未按映射返回")
		}
	}
	calls := f.calls()
	wantCalls := []r18PreviewTestRequest{
		{"manifest.example", "/master.m3u8", "signature=master-signed", ""},
		{"manifest.example", "/variants/low.m3u8", "signature=nested%2Bsig", ""},
		{"assets.example", "/init.mp4", "signature=map%2Bsig", ""},
		{"keys.example", "/license.key", "signature=key-signed", ""},
		{"media.example", "/segment.ts", "signature=segment%2Bsig", ""},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatal("清单相对解析、跨域地址或原始查询签名未保留")
	}
	resp, body := f.call(t, "GET", uris[2], f.token, "bytes=2-5", 206)
	if string(body) != "2345" || resp.Header.Get("Content-Range") != "bytes 2-5/16" || resp.Header.Get("Accept-Ranges") != "bytes" || resp.Header.Get("Set-Cookie") != "" {
		t.Fatal("分片 Range 或安全响应头错误")
	}
	resp, body = f.call(t, "HEAD", uris[2], f.token, "bytes=2-5", 206)
	if len(body) != 0 || resp.ContentLength != 4 || resp.Header.Get("Content-Range") != "bytes 2-5/16" {
		t.Fatal("HEAD 分片返回正文或丢失长度与范围")
	}
	resp, body = f.call(t, "GET", uris[2], f.token, "bytes=99-", 416)
	if len(body) != 0 || resp.Header.Get("Content-Range") != "bytes */16" {
		t.Fatal("分片 416 没有保留总长度或返回了上游正文")
	}
	resp, body = f.call(t, "HEAD", f.item+"/preview.m3u8", f.token, "", 200)
	if len(body) != 0 || resp.Header.Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatal("HEAD 主清单契约错误")
	}
}

func TestR18PreviewSessionPermissionsAndScope(t *testing.T) {
	f := newR18PreviewFixture(t)
	f.call(t, "GET", f.item+"/preview.m3u8", "", "", 401)
	viewer, err := f.app.db.CreateUser("preview-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	const viewerToken = "preview-viewer-token"
	if err = f.app.db.SaveUserToken(viewerToken, viewer.ID); err != nil {
		t.Fatal(err)
	}
	f.call(t, "GET", f.item+"/preview.m3u8", viewerToken, "", 404)
	if len(f.calls()) != 0 || f.fake.reads != 0 {
		t.Fatal("未登录或无库权限请求访问了上游")
	}
	viewerDB := f.app.db.ForUser(viewer.ID)
	if err = viewerDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	_, master := f.call(t, "GET", f.item+"/preview.m3u8", viewerToken, "", 200)
	asset := previewFixtureURIs(t, master)[0]
	f.call(t, "GET", asset, "", "", 401)
	f.call(t, "GET", asset, f.token, "", 404)
	other, err := f.app.db.CreateUser("preview-other", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.app.db.SaveUserToken("preview-other-token", other.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.app.db.ForUser(other.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	f.call(t, "GET", asset, "preview-other-token", "", 404)
	f.call(t, "GET", strings.Replace(asset, f.fake.video.ID, r18.EncodeID("preview002", "javdb"), 1), viewerToken, "", 404)
	parts := strings.Split(asset, "/")
	parts[len(parts)-1] = "missing-resource"
	f.call(t, "GET", strings.Join(parts, "/"), viewerToken, "", 404)
	parts = strings.Split(asset, "/")
	parts[len(parts)-2] = "missing-session"
	f.call(t, "GET", strings.Join(parts, "/"), viewerToken, "", 404)
	otherLibrary, err := f.app.db.CreateR18Library("Other preview", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-api-key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = viewerDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID, otherLibrary.ID}}); err != nil {
		t.Fatal(err)
	}
	wrongLibrary := strings.Replace(asset, "/libraries/"+strconv.FormatInt(f.library.ID, 10)+"/", "/libraries/"+strconv.FormatInt(otherLibrary.ID, 10)+"/", 1)
	f.call(t, "GET", wrongLibrary, viewerToken, "", 404)
	if len(f.calls()) != 1 || f.fake.reads != 1 {
		t.Fatal("会话未绑定用户、影片、资料库，或拒绝时访问了上游")
	}
	f.call(t, "GET", asset, viewerToken, "", 200)
	if err = viewerDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	f.call(t, "GET", asset, viewerToken, "", 404)
	if len(f.calls()) != 2 {
		t.Fatal("撤权后暖会话仍访问上游")
	}
	if err = viewerDB.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.db.UpdateR18Connection(f.library.ID, f.library.Revision, "Changed preview", r18.Config{BaseURL: "https://changed-avdb.invalid", APIKey: "new-fixture-api-key"}); err != nil {
		t.Fatal(err)
	}
	f.call(t, "GET", asset, viewerToken, "", 404)
	if len(f.calls()) != 2 || f.fake.reads != 1 {
		t.Fatal("库 revision 变化后复用了旧预览会话")
	}
}

func TestR18PreviewCompleteMetadataSurvivesSparseLists(t *testing.T) {
	for _, primeDetail := range []bool{true, false} {
		t.Run(strconv.FormatBool(primeDetail), func(t *testing.T) {
			f := newR18PreviewFixture(t)
			complete := f.fake.video
			if primeDetail {
				f.call(t, "GET", f.item, f.token, "", 200)
			}
			f.fake.video.HasPreview, f.fake.video.PreviewURL, f.fake.video.Tags = false, "", nil
			listPath := "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/videos"
			f.call(t, "GET", listPath, f.token, "", 200)
			reads := f.fake.reads
			f.fake.video = complete
			f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
			wantReads := reads
			if !primeDetail {
				wantReads++
			}
			if f.fake.reads != wantReads || len(f.calls()) != 1 {
				t.Fatal("稀疏列表误判预览能力或未复用完整详情")
			}
		})
	}
}

func TestR18PreviewFailuresAreBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		noPreview  bool
		badURL     bool
		missingURL bool
		status     int
		body       []byte
		failure    error
	}{
		{name: "no_preview", noPreview: true},
		{name: "unsafe_original", badURL: true},
		{name: "missing_original", missingURL: true},
		{name: "invalid_playlist", status: 200, body: []byte("fixture-api-key master-signed https://manifest.example/private")},
		{name: "invalid_hls_header", status: 200, body: []byte("#EXTM3U-invalid\n")},
		{name: "html_playlist", status: 200, body: []byte("<html>master-signed fixture-api-key</html>")},
		{name: "unsafe_playlist_uri", status: 200, body: []byte("#EXTM3U\njavascript:master-signed\n")},
		{name: "oversize_playlist", status: 200, body: append([]byte("#EXTM3U\n#"), bytes.Repeat([]byte{'x'}, maxHLSPlaylistBytes)...)},
		{name: "upstream_denied", status: 403, body: []byte("fixture-api-key https://manifest.example/?signature=master-signed")},
		{name: "upstream_error", failure: errors.New("fixture-api-key https://manifest.example/?signature=master-signed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreviewFixture(t)
			if tc.noPreview {
				f.fake.video.HasPreview = false
			}
			if tc.badURL {
				f.fake.video.PreviewURL = "https://user:fixture-api-key@manifest.example/master.m3u8"
			}
			if tc.missingURL {
				f.fake.video.PreviewURL = ""
			}
			if tc.noPreview || tc.missingURL || tc.badURL {
				_, detail := f.call(t, "GET", f.item, f.token, "", 200)
				previewFixtureSafeResponse(t, detail)
				var parsed struct {
					Video r18.Video `json:"video"`
				}
				if err := json.Unmarshal(detail, &parsed); err != nil || parsed.Video.HasPreview != !tc.noPreview || parsed.Video.PreviewURL != "" {
					t.Fatal("无预览时公开详情仍暴露播放地址", err)
				}
			}
			f.respond = func(req *http.Request) (*http.Response, error) {
				if tc.failure != nil {
					return nil, tc.failure
				}
				return previewFixtureResponse(req, tc.status, "text/html", tc.body), nil
			}
			want := 502
			if tc.noPreview || tc.badURL || tc.missingURL {
				want = 404
			}
			_, body := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", want)
			previewFixtureSafeResponse(t, body)
			if (tc.noPreview || tc.badURL || tc.missingURL) && len(f.calls()) != 0 {
				t.Fatal("无预览或非法原地址访问了公网")
			}
		})
	}
	for _, tc := range []struct {
		name        string
		contentType string
		body        []byte
		tooLarge    bool
	}{
		{"html_asset", "application/octet-stream", []byte("<html>fixture-api-key master-signed</html>"), false},
		{"json_asset", "application/json", []byte(`{"error":"master-signed"}`), false},
		{"oversize_asset", "video/mp2t", []byte("fixture"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreviewFixture(t)
			_, master := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
			_, nested := f.call(t, "GET", previewFixtureURIs(t, master)[0], f.token, "", 200)
			asset := previewFixtureURIs(t, nested)[2]
			f.respond = func(req *http.Request) (*http.Response, error) {
				resp := previewFixtureResponse(req, 200, tc.contentType, tc.body)
				if tc.tooLarge {
					resp.ContentLength = r18PreviewResourceLimit + 1
				}
				return resp, nil
			}
			_, body := f.call(t, "GET", asset, f.token, "", 502)
			previewFixtureSafeResponse(t, body)
		})
	}
}

func TestR18PreviewHEADDoesNotAllocateOrEvictSessions(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		f := newR18PreviewFixture(t)
		f.call(t, "HEAD", f.item+"/preview.m3u8", f.token, "", 200)
		f.app.hlsProxyMu.Lock()
		count := len(f.app.hlsProxySessions)
		f.app.hlsProxyMu.Unlock()
		if count != 0 {
			t.Fatal("HEAD 主清单创建了无用 HLS 会话")
		}
	})
	t.Run("nested", func(t *testing.T) {
		f := newR18PreviewFixture(t)
		_, master := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
		asset := previewFixtureURIs(t, master)[0]
		f.app.hlsProxyMu.Lock()
		beforeSessions, beforeResources := len(f.app.hlsProxySessions), 0
		for _, session := range f.app.hlsProxySessions {
			beforeResources += len(session.resources)
		}
		f.app.hlsProxyMu.Unlock()
		f.call(t, "HEAD", asset, f.token, "", 200)
		f.app.hlsProxyMu.Lock()
		afterSessions, afterResources := len(f.app.hlsProxySessions), 0
		for _, session := range f.app.hlsProxySessions {
			afterResources += len(session.resources)
		}
		f.app.hlsProxyMu.Unlock()
		if beforeSessions != afterSessions || beforeResources != afterResources {
			t.Fatal("HEAD 子清单创建了无用会话或资源映射")
		}
	})
	t.Run("existing_movie_session", func(t *testing.T) {
		f := newR18PreviewFixture(t)
		protected := ""
		for i := 0; i < maxHLSSessionCount; i++ {
			id, err := f.app.newHLSProxySession(strconv.Itoa(i + 1))
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				protected = id
			}
		}
		f.app.hlsProxyMu.Lock()
		f.app.hlsProxySessions[protected].expires = time.Now().Add(time.Minute)
		f.app.hlsProxyMu.Unlock()
		f.call(t, "HEAD", f.item+"/preview.m3u8", f.token, "", 200)
		f.app.hlsProxyMu.Lock()
		_, survives := f.app.hlsProxySessions[protected]
		count := len(f.app.hlsProxySessions)
		f.app.hlsProxyMu.Unlock()
		if !survives || count != maxHLSSessionCount {
			t.Fatal("HEAD 预览驱逐了已有正片 HLS 会话")
		}
	})
}

func TestR18PreviewMultipartRangesPreserveBoundaryAndOWS(t *testing.T) {
	f := newR18PreviewFixture(t)
	_, master := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
	_, nested := f.call(t, "GET", previewFixtureURIs(t, master)[0], f.token, "", 200)
	asset := previewFixtureURIs(t, nested)[2]
	const contentType = "multipart/byteranges; boundary=fixture-boundary"
	body := []byte("--fixture-boundary\r\nContent-Type: video/mp2t\r\nContent-Range: bytes 0-1/16\r\n\r\n01\r\n--fixture-boundary\r\nContent-Type: video/mp2t\r\nContent-Range: bytes 4-5/16\r\n\r\n45\r\n--fixture-boundary--\r\n")
	for _, byteRange := range []string{"bytes=0-1,4-5", "bytes=0-1, 4-5", "bytes=0-1 , 4-5"} {
		t.Run(byteRange, func(t *testing.T) {
			f.respond = func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Range") != byteRange {
					t.Error("合法多范围请求未原样传递")
				}
				return previewFixtureResponse(req, 206, contentType, body), nil
			}
			resp, got := f.call(t, "GET", asset, f.token, byteRange, 206)
			if !bytes.Equal(got, body) || resp.Header.Get("Content-Type") != contentType {
				t.Fatal("多范围响应被拒绝或丢失 multipart boundary")
			}
		})
	}
}

// 按需生成数据，超限回归不在夹具中预先分配或保存视频文件。
type r18PreviewGeneratedStream struct {
	remaining int64
	terminal  error
}

func (r *r18PreviewGeneratedStream) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		if r.terminal != nil {
			return 0, r.terminal
		}
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	for i := range p {
		p[i] = 'G'
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestR18PreviewUnknownLengthIsBoundedBeforeReply(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		err  error
		want int
	}{
		{"valid_unknown_length", 1024, nil, 200},
		{"oversize_unknown_length", r18PreviewResourceLimit + 1, nil, 502},
		{"interrupted_unknown_length", 1024, errors.New("fixture-api-key master-signed interrupted"), 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreviewFixture(t)
			_, master := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
			_, nested := f.call(t, "GET", previewFixtureURIs(t, master)[0], f.token, "", 200)
			asset := previewFixtureURIs(t, nested)[2]
			f.respond = func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, ContentLength: -1, Request: req,
					Header: http.Header{"Content-Type": {"video/mp2t"}}, Body: io.NopCloser(&r18PreviewGeneratedStream{remaining: tc.size, terminal: tc.err})}, nil
			}
			_, body := f.call(t, "GET", asset, f.token, "", tc.want)
			if tc.want == 200 {
				if len(body) != int(tc.size) {
					t.Fatal("合法未知长度资源被截断")
				}
			} else {
				previewFixtureSafeResponse(t, body)
			}
		})
	}
}

func TestR18PreviewBOMHeaderIsNotRewrittenAsResource(t *testing.T) {
	f := newR18PreviewFixture(t)
	f.respond = func(req *http.Request) (*http.Response, error) {
		return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", []byte("\uFEFF#EXTM3U\n#EXTINF:4,\nhttps://media.example/segment.ts?signature=segment%2Bsig\n#EXT-X-ENDLIST\n")), nil
	}
	_, body := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
	if len(previewFixtureURIs(t, body)) != 1 || len(f.calls()) != 1 {
		t.Fatal("BOM 清单头被错误注册为预览资源")
	}
	previewFixtureSafeResponse(t, body)
}

func TestR18PreviewMP4RootSupportsRangeAndHEAD(t *testing.T) {
	f := newR18PreviewFixture(t)
	f.fake.video.PreviewURL = "https://media.example/clip.mp4?signature=mp4%2Bsig"
	f.respond = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/clip.mp4" || req.URL.RawQuery != "signature=mp4%2Bsig" || req.Header.Get("Range") != "bytes=2-5" {
			t.Error("MP4 原始地址签名或 Range 未保留")
		}
		resp := previewFixtureResponse(req, 206, "video/mp4", []byte("2345"))
		resp.Header.Set("Content-Range", "bytes 2-5/16")
		return resp, nil
	}
	_, detail := f.call(t, "GET", f.item, f.token, "", 200)
	var parsed struct {
		Video r18.Video `json:"video"`
	}
	if err := json.Unmarshal(detail, &parsed); err != nil || parsed.Video.PreviewURL != f.item+"/preview.mp4" {
		t.Fatal("明确 MP4 原预览未映射第一方 MP4 入口", err)
	}
	if bytes.Contains(detail, []byte("mp4%2Bsig")) || len(f.calls()) != 0 {
		t.Fatal("详情泄露或预取了 MP4 原预览")
	}
	resp, body := f.call(t, "GET", f.item+"/preview.mp4", f.token, "bytes=2-5", 206)
	if string(body) != "2345" || resp.Header.Get("Content-Type") != "video/mp4" || resp.Header.Get("Content-Range") != "bytes 2-5/16" {
		t.Fatal("MP4 根预览被强制解释为 HLS 或丢失 Range")
	}
	resp, body = f.call(t, "HEAD", f.item+"/preview.mp4", f.token, "bytes=2-5", 206)
	if len(body) != 0 || resp.ContentLength != 4 || resp.Header.Get("Content-Range") != "bytes 2-5/16" {
		t.Fatal("MP4 HEAD 返回正文或丢失部分响应长度")
	}
	f.app.hlsProxyMu.Lock()
	count := len(f.app.hlsProxySessions)
	f.app.hlsProxyMu.Unlock()
	if count != 0 {
		t.Fatal("MP4 预览分配了不需要的 HLS 会话")
	}
}

func TestR18PreviewURIAttributesAreCanonicalAndQuoteAware(t *testing.T) {
	for _, attribute := range []string{
		`URI=https://keys.example/license.key?signature=key-signed`,
		`URI ="https://keys.example/license.key?signature=key-signed"`,
		`URI= "https://keys.example/license.key?signature=key-signed"`,
		`uri="https://keys.example/license.key?signature=key-signed"`,
		`URI='https://keys.example/license.key?signature=key-signed'`,
		`uri='https://keys.example/license.key?signature=key-signed'`,
		`URI="https://keys.example/license.key?signature=key-signed`,
	} {
		name := strings.Split(attribute, "https:")[0]
		t.Run(name, func(t *testing.T) {
			f := newR18PreviewFixture(t)
			f.respond = func(req *http.Request) (*http.Response, error) {
				return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", []byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,"+attribute+"\n#EXT-X-ENDLIST\n")), nil
			}
			_, body := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 502)
			previewFixtureSafeResponse(t, body)
			if len(f.calls()) != 1 {
				t.Fatal("非法密钥 URI 被预取或绕过第一方代理")
			}
		})
	}
	t.Run("quoted_commas_and_non_uri_value", func(t *testing.T) {
		f := newR18PreviewFixture(t)
		f.respond = func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/license.key" {
				if req.URL.RawQuery != "signature=a,b" {
					t.Error("带逗号的原始签名被属性分割器损坏")
				}
				return previewFixtureResponse(req, 200, "application/octet-stream", []byte("0123456789abcdef")), nil
			}
			body := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,VALUE=\"note URI=not-a-resource, keep-me\",URI=\"https://keys.example/license.key?signature=a,b\"\n#EXT-X-ENDLIST\n"
			return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", []byte(body)), nil
		}
		_, body := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 200)
		uris := previewFixtureURIs(t, body)
		if len(uris) != 1 || !bytes.Contains(body, []byte(`VALUE="note URI=not-a-resource, keep-me"`)) {
			t.Fatal("普通属性的 URI= 文本被误判为资源或合法引号内逗号被拒绝")
		}
		previewFixtureSafeResponse(t, body)
		_, key := f.call(t, "GET", uris[0], f.token, "", 200)
		if string(key) != "0123456789abcdef" {
			t.Fatal("合法密钥属性改写后不可读取")
		}
	})
}

func TestR18PreviewFailedGETPreflightDoesNotAllocateSessions(t *testing.T) {
	playlist := func(count int) []byte {
		var body strings.Builder
		body.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n")
		for i := 0; i < count; i++ {
			body.WriteString("#EXTINF:1,\nr")
			body.WriteString(strconv.FormatInt(int64(i), 36))
			body.WriteString(".ts\n")
		}
		body.WriteString("#EXT-X-ENDLIST\n")
		return []byte(body.String())
	}
	for _, tc := range []struct {
		name   string
		body   []byte
		source string
	}{
		{"bad_header", []byte("#EXTM3U-invalid\n"), previewFixtureMaster},
		{"unsafe_uri", []byte("#EXTM3U\n#EXTINF:1,\njavascript:fixture-api-key\n#EXT-X-ENDLIST\n"), previewFixtureMaster},
		{"resource_count", playlist(maxHLSResourceCount + 1), previewFixtureMaster},
		// 清单本身很小，但相对地址按深目录展开后超过会话地址总量限制。
		{"expanded_source_bytes", playlist(400), "https://manifest.example/" + strings.Repeat("directory/", 350) + "master.m3u8?signature=master-signed"},
	} {
		if len(tc.body) > maxHLSPlaylistBytes {
			t.Fatal("预检夹具错误地先触发了清单正文大小限制")
		}
		for _, fullSessions := range []bool{false, true} {
			t.Run(tc.name+"/full_"+strconv.FormatBool(fullSessions), func(t *testing.T) {
				f := newR18PreviewFixture(t)
				f.fake.video.PreviewURL = tc.source
				if fullSessions {
					for i := 0; i < maxHLSSessionCount; i++ {
						id, err := f.app.newHLSProxySession(strconv.Itoa(i + 1))
						if err != nil {
							t.Fatal(err)
						}
						if i == 0 {
							f.app.hlsProxyMu.Lock()
							f.app.hlsProxySessions[id].expires = time.Now().Add(time.Minute)
							f.app.hlsProxyMu.Unlock()
						}
					}
				}
				snapshot := func() map[string]int {
					f.app.hlsProxyMu.Lock()
					defer f.app.hlsProxyMu.Unlock()
					ids := make(map[string]int, len(f.app.hlsProxySessions))
					for id, session := range f.app.hlsProxySessions {
						ids[id] = len(session.resources)
					}
					return ids
				}
				before := snapshot()
				f.respond = func(req *http.Request) (*http.Response, error) {
					return previewFixtureResponse(req, 200, "application/vnd.apple.mpegurl", tc.body), nil
				}
				_, body := f.call(t, "GET", f.item+"/preview.m3u8", f.token, "", 502)
				previewFixtureSafeResponse(t, body)
				if !reflect.DeepEqual(before, snapshot()) {
					t.Fatal("失败 GET 创建了会话、注册了资源或驱逐了已有正片会话")
				}
				if len(f.calls()) != 1 {
					t.Fatal("失败清单预检访问了分片或密钥资源")
				}
			})
		}
	}
}

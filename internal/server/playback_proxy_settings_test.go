package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func headerProxyMovie(t *testing.T, app *App, source string, headers map[string]string) (store.Library, store.Movie) {
	t.Helper()
	root := t.TempDir()
	library, err := app.db.AddDirectoryLibrary("请求头媒体库", []string{root}, store.LibraryMovies, true, true, headers)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "film.strm")
	writeFile(t, path, source)
	movie := store.Movie{LibraryID: library.ID, SourcePath: path, Title: "Film", Status: "success"}
	movie.ID, err = app.db.UpsertMovie(movie, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return library, movie
}

func TestLibraryProxyHeadersStreamAndExternalPlayback(t *testing.T) {
	var requests atomic.Int32
	headers := map[string]string{"Referer": "https://player.example/", "Origin": "https://player.example", "User-Agent": "Library Player", "X-Source": "library"}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		for name, want := range headers {
			if r.Header.Get(name) != want {
				t.Errorf("请求头 %s = %q，预期 %q", name, r.Header.Get(name), want)
			}
		}
		for _, name := range []string{"Cookie", "Authorization", "X-Emby-Token"} {
			if r.Header.Get(name) != "" {
				t.Errorf("浏览器认证头被转发：%s", name)
			}
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("代理未禁用内容压缩")
		}
		http.ServeContent(w, r, "film.mp4", time.Time{}, bytes.NewReader([]byte("0123456789")))
	}))
	defer origin.Close()
	app := accountApp(t)
	token := accountLogin(t, app)
	_, movie := headerProxyMovie(t, app, origin.URL+"/film.mp4", headers)
	part := filepath.Join(filepath.Dir(movie.SourcePath), "film-cd2.strm")
	writeFile(t, part, origin.URL+"/film-cd2.mp4")
	movie.AdditionalParts = []string{part}
	if _, err := app.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(movie.ID, 10)
	for _, endpoint := range []string{"/Videos/" + id + "/stream", "/Videos/" + id + "/proxy", "/Videos/" + virtualPartID(movie.ID, 2) + "/stream"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		request.Header.Set("X-Emby-Token", token)
		request.Header.Set("Cookie", "private=browser")
		request.Header.Set("Authorization", "Bearer browser-secret")
		request.Header.Set("Referer", "https://browser.example/private")
		request.Header.Set("Origin", "https://browser.example")
		request.Header.Set("User-Agent", "Browser Player")
		request.Header.Set("Range", "bytes=2-4")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusPartialContent || response.Body.String() != "234" || response.Header().Get("Content-Range") != "bytes 2-4/10" {
			t.Fatalf("代理 Range 响应错误：%d %v %q", response.Code, response.Header(), response.Body.String())
		}
	}
	head := httptest.NewRequest(http.MethodHead, "/Videos/"+id+"/stream", nil)
	head.Header.Set("Range", "bytes=2-4")
	headResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(headResponse, head)
	if headResponse.Code != http.StatusPartialContent || headResponse.Body.Len() != 0 || headResponse.Header().Get("Content-Range") != "bytes 2-4/10" {
		t.Fatalf("代理 HEAD 响应错误：%d %v", headResponse.Code, headResponse.Header())
	}
	before := requests.Load()
	for _, itemID := range []string{id, virtualPartID(movie.ID, 2)} {
		response := accountRequest(app, http.MethodPost, "/emby/Items/"+itemID+"/PlaybackInfo", token, "")
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
			t.Fatalf("播放信息失败：%d %s %v", response.Code, response.Body, err)
		}
		source := playbackSource(t, body)
		external, err := url.Parse(source["ExternalStreamUrl"].(string))
		if err != nil || external.Scheme != "http" || external.Host != "example.com" || external.Path != "/emby/Videos/"+itemID+"/stream" || external.Query().Get("api_key") != token {
			t.Fatalf("外部播放器未使用已授权的代理地址：%v %v", external, err)
		}
		if value, ok := source["RequiredHttpHeaders"].(map[string]any); !ok || len(value) != 0 {
			t.Fatalf("播放信息暴露了源请求头：%v", source["RequiredHttpHeaders"])
		}
	}
	if requests.Load() != before {
		t.Fatal("开启代理时 PlaybackInfo 仍请求了解析直链接口")
	}
}

func TestLibraryProxyHeadersTelevisionVersions(t *testing.T) {
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Referer") != "https://player.example/" || r.Header.Get("Origin") != "https://player.example" {
			t.Errorf("电视剧版本未应用库请求头：%v", r.Header)
		}
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\nsegment.ts\n")
			return
		}
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer origin.Close()
	app := accountApp(t)
	token := accountLogin(t, app)
	_, movie := headerProxyMovie(t, app, origin.URL+"/episode-1080p.mp4", map[string]string{"Referer": "https://player.example/", "Origin": "https://player.example"})
	movie.Television = &store.Television{SeriesPath: filepath.Dir(movie.SourcePath), Title: "Show", Season: 1, Episode: 1}
	if _, err := app.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	other := movie
	other.ID = 0
	other.SourcePath = filepath.Join(filepath.Dir(movie.SourcePath), "episode-4k.strm")
	writeFile(t, other.SourcePath, origin.URL+"/episode-4k.mp4")
	var err error
	other.ID, err = app.db.UpsertMovie(other, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	selected := "mediasource_" + strconv.FormatInt(other.ID, 10)
	response := accountRequest(app, http.MethodPost, fmt.Sprintf("/Items/%d/PlaybackInfo", movie.ID), token, `{"MediaSourceId":"`+selected+`"}`)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
		t.Fatalf("电视剧播放信息失败：%d %s %v", response.Code, response.Body, err)
	}
	sources := body["MediaSources"].([]any)
	if len(sources) != 2 || sources[0].(map[string]any)["Id"] != selected {
		t.Fatalf("电视剧播放源选择错误：%v", sources)
	}
	for _, item := range sources {
		source := item.(map[string]any)
		external, err := url.Parse(source["ExternalStreamUrl"].(string))
		id := strings.TrimPrefix(source["Id"].(string), "mediasource_")
		if err != nil || external.Host != "example.com" || external.Path != "/Videos/"+id+"/stream" || external.Query().Get("MediaSourceId") != source["Id"] || external.Query().Get("api_key") != token {
			t.Fatalf("电视剧外部播放源未指向对应代理：%v %v", source, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("启用代理的电视剧播放信息预先请求了源站")
	}
	response = accountRequest(app, http.MethodGet, fmt.Sprintf("/Videos/%d/stream?MediaSourceId=%s", movie.ID, selected), token, "")
	if response.Code != http.StatusOK || response.Body.String() != "/episode-4k.mp4" {
		t.Fatalf("电视剧流没有使用所选版本：%d %s", response.Code, response.Body)
	}
	writeFile(t, other.SourcePath, origin.URL+"/episode-4k.m3u8")
	response = accountRequest(app, http.MethodGet, fmt.Sprintf("/Videos/%d/stream?MediaSourceId=%s", movie.ID, selected), token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("电视剧 HLS 代理失败：%d %s", response.Code, response.Body)
	}
	resource := hlsProxyURLInBody(t, response.Body.String())
	other.Status = "pending"
	if _, err := app.db.UpsertMovie(other, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if response := accountRequest(app, http.MethodGet, resource, "", ""); response.Code != http.StatusNotFound {
		t.Fatal("所选电视剧版本失效后，其 HLS 会话仍能回源")
	}
}

func TestLibraryProxyHeadersRedirectKeepsOriginWithoutCredentials(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("https=%t", secure), func(t *testing.T) {
			headers := map[string]string{"Referer": "https://player.example/", "Origin": "https://player.example", "Authorization": "Bearer library-secret", "Cookie": "session=source", "X-API-Key": "source-secret"}
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, name := range []string{"Referer", "Origin"} {
					if r.Header.Get(name) != headers[name] {
						t.Errorf("重定向丢失 %s：%q", name, r.Header.Get(name))
					}
				}
				for _, name := range []string{"Authorization", "Cookie", "X-API-Key"} {
					if r.Header.Get(name) != "" {
						t.Errorf("重定向泄漏了敏感源请求头：%s", name)
					}
				}
				_, _ = io.WriteString(w, "media")
			}))
			defer cdn.Close()
			originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for name, want := range headers {
					if r.Header.Get(name) != want {
						t.Errorf("初始来源未收到 %s", name)
					}
				}
				http.Redirect(w, r, cdn.URL+"/film.mp4", http.StatusFound)
			})
			var origin *httptest.Server
			if secure {
				origin = httptest.NewTLSServer(originHandler)
				previous := streamClient
				streamClient = origin.Client()
				defer func() { streamClient = previous }()
			} else {
				origin = httptest.NewServer(originHandler)
			}
			defer origin.Close()
			app := accountApp(t)
			_, movie := headerProxyMovie(t, app, origin.URL+"/film.mp4", headers)
			response := accountRequest(app, http.MethodGet, fmt.Sprintf("/Videos/%d/stream", movie.ID), "", "")
			if response.Code != http.StatusOK || response.Body.String() != "media" {
				t.Fatalf("重定向播放失败：%d %s", response.Code, response.Body)
			}
		})
	}
}

func TestLibraryProxyHeadersHLSResourcesUseLatestSettings(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := fmt.Sprintf("https://player%d.example/", version.Load())
		if r.Header.Get("Referer") != want || r.Header.Get("Origin") != strings.TrimSuffix(want, "/") {
			t.Errorf("HLS 资源 %s 未使用最新请求头：%v", r.URL.Path, r.Header)
		}
		switch r.URL.Path {
		case "/master":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if r.Header.Get("Range") != "" {
				w.WriteHeader(http.StatusPartialContent)
			}
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nvariant.m3u8\n")
		case "/variant.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:5,\nsegment.ts\n")
		case "/key.bin", "/init.mp4", "/segment.ts":
			http.ServeContent(w, r, "segment.ts", time.Time{}, bytes.NewReader([]byte("0123456789")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	app := accountApp(t)
	library, movie := headerProxyMovie(t, app, origin.URL+"/master", map[string]string{"Referer": "https://player1.example/", "Origin": "https://player1.example"})
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Videos/%d/stream", movie.ID), nil)
	request.Header.Set("Range", "bytes=0-99")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HLS 主清单失败：%d %s", response.Code, response.Body)
	}
	variant := hlsProxyURLInBody(t, response.Body.String())
	var err error
	library, err = app.db.UpdateDirectoryLibrary(library.ID, library.Name, library.Paths, true, library.ResolveRedirects, map[string]string{"Referer": "https://player2.example/", "Origin": "https://player2.example"}, library.Revision)
	if err != nil {
		t.Fatal(err)
	}
	version.Store(2)
	app.invalidateLibraryPlayback(library.ID)
	nested := accountRequest(app, http.MethodGet, variant, "", "")
	if nested.Code != http.StatusOK {
		t.Fatalf("HLS 子清单失败：%d %s", nested.Code, nested.Body)
	}
	resources := hlsProxyURLsInBody(t, nested.Body.String())
	if len(resources) != 3 {
		t.Fatalf("HLS 密钥、初始化段和分片未全部改写：%s", nested.Body)
	}
	for _, resource := range resources {
		request := httptest.NewRequest(http.MethodGet, resource, nil)
		request.Header.Set("Range", "bytes=3-5")
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusPartialContent || response.Body.String() != "345" {
			t.Fatalf("HLS 资源 Range 响应失败：%d %s", response.Code, response.Body)
		}
	}
	if _, err := app.db.UpdateDirectoryLibrary(library.ID, library.Name, library.Paths, false, library.ResolveRedirects, library.ProxyHeaders, library.Revision); err != nil {
		t.Fatal(err)
	}
	if response := accountRequest(app, http.MethodGet, resources[0], "", ""); response.Code != http.StatusNotFound {
		t.Fatal("关闭代理后旧 HLS 能力仍能回源")
	}
}

func TestLibraryProxyHeadersHLSCrossOriginAndTrailerIsolation(t *testing.T) {
	headers := map[string]string{"Referer": "https://player.example/", "Origin": "https://player.example", "Cookie": "session=source", "Authorization": "Bearer source"}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != headers["Referer"] || r.Header.Get("Origin") != headers["Origin"] || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("HLS 跨来源请求头不符合边界：%v", r.Header)
		}
		_, _ = io.WriteString(w, "segment")
	}))
	defer cdn.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trailer.mp4" {
			for name := range headers {
				if r.Header.Get(name) != "" {
					t.Errorf("预告片收到媒体库请求头：%s", name)
				}
			}
			_, _ = io.WriteString(w, "trailer")
			return
		}
		for name, value := range headers {
			if r.Header.Get(name) != value {
				t.Errorf("HLS 主源未收到配置请求头：%s", name)
			}
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = fmt.Fprintf(w, "#EXTM3U\n%s/segment.ts\n", cdn.URL)
	}))
	defer origin.Close()
	app := accountApp(t)
	_, movie := headerProxyMovie(t, app, origin.URL+"/master.m3u8", headers)
	movie.TrailerURL = origin.URL + "/trailer.mp4"
	if _, err := app.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	manifest := accountRequest(app, http.MethodGet, fmt.Sprintf("/Videos/%d/stream", movie.ID), "", "")
	resource := hlsProxyURLInBody(t, manifest.Body.String())
	if response := accountRequest(app, http.MethodGet, resource, "", ""); response.Code != http.StatusOK || response.Body.String() != "segment" {
		t.Fatalf("跨来源分片代理失败：%d %s", response.Code, response.Body)
	}
	if response := accountRequest(app, http.MethodGet, fmt.Sprintf("/Videos/%d/trailer", movie.ID), "", ""); response.Code != http.StatusOK || response.Body.String() != "trailer" {
		t.Fatalf("预告片失败：%d %s", response.Code, response.Body)
	}
}

type proxyHeaderRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyHeaderRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProxyHeadersOverrideDMMReferer(t *testing.T) {
	previous := streamClient
	streamClient = &http.Client{Transport: proxyHeaderRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Referer") != "https://custom.example/" || request.Header.Get("Origin") != "https://custom.example" {
			t.Errorf("配置请求头没有覆盖 DMM 默认值：%v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("media")), Request: request}, nil
	})}
	defer func() { streamClient = previous }()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/Videos/1/stream", nil)
	app := accountApp(t)
	response, err := app.doProxyRequest(c, "https://video.dmm.co.jp/media.mp4", true, map[string]string{"Referer": "https://custom.example/", "Origin": "https://custom.example"})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestProxyMediaInputUsesHeadersAndCloses(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://player.example/" || r.Header.Get("Origin") != "https://player.example" {
			t.Errorf("本机代理输入未应用请求头：%v", r.Header)
		}
		http.ServeContent(w, r, "media.mp4", time.Time{}, bytes.NewReader([]byte("0123456789")))
	}))
	defer origin.Close()
	app := accountApp(t)
	_, movie := headerProxyMovie(t, app, origin.URL+"/media.mp4", map[string]string{"Referer": "https://player.example/", "Origin": "https://player.example"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, cleanup, err := app.proxyMediaInput(ctx, movie, strconv.FormatInt(movie.ID, 10), origin.URL+"/media.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	request, _ := http.NewRequest(http.MethodGet, input, nil)
	request.Header.Set("Range", "bytes=4-6")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusPartialContent || string(data) != "456" {
		t.Fatalf("本机输入失败：%d %q %v", response.StatusCode, data, err)
	}
	parsed, _ := url.Parse(input)
	parsed.Path = "/input"
	if response, err := http.Get(parsed.String()); err != nil {
		t.Fatal(err)
	} else {
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatal("本机输入未要求随机能力路径")
		}
	}
	cleanup()
	if response, err := http.Get(input); err == nil {
		response.Body.Close()
		t.Fatal("清理后本机播放输入仍在监听")
	}
}

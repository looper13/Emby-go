package r18

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixtureMovie(index int) apiMovie {
	return apiMovie{ID: fmt.Sprintf("movie%03d", index), Number: fmt.Sprintf("TEST-%03d", index), Title: "Fixture film", OriginTitle: "Fixture original", ReleaseDate: "2026-01-02", CoverURL: "https://images.invalid/cover.jpg"}
}

func writeFixtureResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.Header.Get("X-API-Key") != "fixture-key" || req.Header.Get("Accept") != "application/json" {
			t.Error("请求没有使用只读 GET 和后端 API Key Header")
		}
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.URL.Query().Get("api_key") != "" || req.URL.Query().Get("access_token") != "" {
			t.Error("发送了非必要凭证或将 Key 放入 URL")
		}
		handler(w, req)
	}))
	client, err := Open(Config{BaseURL: server.URL, APIKey: "fixture-key"})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); server.Close() })
	return client
}

func TestR18IdentitiesAndSafeResources(t *testing.T) {
	id := EncodeID("movie001", "javdb")
	content, service, err := DecodeID(id)
	if err != nil || content != "movie001" || service != "javdb" || id == EncodeID("movie001", "other") {
		t.Fatal("复合影片标识丢失来源或无法往返", err)
	}
	for _, invalid := range []string{"bad", "W10", strings.Repeat("a", 1025)} {
		if _, _, err := DecodeID(invalid); err == nil {
			t.Fatal("接受了非法影片标识")
		}
	}
	for _, invalid := range []string{"javascript:alert(1)", "data:text/html,test", "https://user:secret@example.com/a", "//example.com/a"} {
		if SafeHTTPURL(invalid) != "" {
			t.Fatal("接受了不安全的图片地址")
		}
	}
	if SafeHTTPURL("https://example.com/cover.jpg") == "" {
		t.Fatal("拒绝合法图片地址")
	}
	for _, invalid := range []string{"javascript:alert(1)", "https://example.com/", "magnet:?xt=urn:btih:garbage", "magnet:?xt=urn:btih:" + strings.Repeat("0", 39), "magnet://example.com/?xt=urn:btih:" + strings.Repeat("0", 40), "magnet:?xt=urn:btih:" + strings.Repeat("0", 40) + "&dn=%ZZ"} {
		if SafeMagnet(invalid) != "" {
			t.Fatal("接受了非法磁力或协议")
		}
	}
	for _, hash := range []string{"urn:btih:" + strings.Repeat("a", 40), "urn:btih:" + strings.Repeat("A", 32), "urn:btmh:1220" + strings.Repeat("a", 64)} {
		if SafeMagnet("magnet:?xt="+hash) == "" {
			t.Fatal("拒绝合法磁力哈希")
		}
	}
}

func TestR18APIConfigAndPrefixedBaseURL(t *testing.T) {
	for _, config := range []Config{
		{BaseURL: "https://avdb.invalid", APIKey: ""},
		{BaseURL: "https://user:secret@avdb.invalid", APIKey: "fixture-key"},
		{BaseURL: "https://avdb.invalid?api_key=secret", APIKey: "fixture-key"},
		{BaseURL: "https://avdb.invalid#fragment", APIKey: "fixture-key"},
		{BaseURL: "ftp://avdb.invalid", APIKey: "fixture-key"},
		{BaseURL: "https://avdb.invalid", APIKey: "fixture\nkey"},
		{BaseURL: "https://avdb.invalid", APIKey: strings.Repeat("x", 4097)},
	} {
		if config.Validate() == nil {
			t.Fatal("接受了不安全的 API 配置")
		}
	}
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/prefix/api/v1/javdb/movies/latest" || req.URL.Query().Get("limit") != "1" {
			t.Error("基础路径前缀或只读连接探针丢失")
		}
		writeFixtureResponse(w, map[string]any{"movies": []apiMovie{}, "current_page": 1, "has_next": false})
	})
	client.cfg.BaseURL += "/prefix"
	if err := client.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(client.cfg)
	if strings.Contains(string(encoded), "fixture-key") || strings.Contains(string(encoded), "api_key") {
		t.Fatal("配置 JSON 回显了 API Key")
	}
}

func TestR18APIErrorsDoNotExposeResponseSecrets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{"unauthorized", 401, "fixture-key private upstream", false},
		{"forbidden", 403, "fixture-key", false},
		{"missing", 404, "fixture-key", true},
		{"unavailable", 503, "fixture-key", false},
		{"business", 200, `{"code":7,"message":"fixture-key","data":{}}`, false},
		{"malformed", 200, `{"code":0,"message":"fixture-key","data":`, false},
		{"missing code", 200, `{"message":"fixture-key","data":{}}`, false},
		{"null data", 200, `{"code":0,"data":null}`, false},
		{"wrong data", 200, `{"code":0,"data":"fixture-key"}`, false},
		{"oversized", 200, strings.Repeat("x", apiBodyLimit+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := client.Check(context.Background())
			if err == nil || strings.Contains(err.Error(), "fixture-key") || strings.Contains(err.Error(), client.cfg.BaseURL) || (tc.missing && !errors.Is(err, sql.ErrNoRows)) {
				t.Fatal("错误未拒绝非法响应或泄露上游详情", err)
			}
		})
	}
}

func TestR18APIRedirectAndCancellation(t *testing.T) {
	hits := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { hits++; w.WriteHeader(200) }))
	defer destination.Close()
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, destination.URL, http.StatusFound)
	})
	if err := client.Check(context.Background()); err == nil || hits != 0 {
		t.Fatal("API 请求跟随了重定向，存在 Key 泄露风险", err, hits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("请求没有响应上下文取消", err)
	}
}

func TestR18LiveReadOnlyPaginationAndResources(t *testing.T) {
	baseURL, key := os.Getenv("EMBY_AVDB_TEST_URL"), os.Getenv("EMBY_AVDB_TEST_KEY")
	if baseURL == "" || key == "" {
		t.Skip("设置 EMBY_AVDB_TEST_URL 和 EMBY_AVDB_TEST_KEY 后执行只读 JavDB 验收")
	}
	client, err := Open(Config{BaseURL: baseURL, APIKey: key})
	if err != nil {
		t.Fatal("真实 API 配置校验失败")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if client.Check(ctx) != nil {
		t.Fatal("真实 API 只读连接校验失败")
	}
	first, err := client.List(ctx, Query{Limit: 2})
	if err != nil || len(first.Items) == 0 || first.Page != 1 || first.PageSize != 2 || first.HasPrevious {
		t.Fatal("真实 API 首页读取失败")
	}
	seen := map[string]bool{}
	for _, video := range first.Items {
		if video.ServiceCode != "javdb" || video.Number == "" || seen[video.ID] {
			t.Fatal("真实 API 首页面数据不正确")
		}
		seen[video.ID] = true
	}
	if first.HasMore {
		second, err := client.List(ctx, Query{Limit: 2, Page: first.Page + 1})
		if err != nil || second.Page != 2 || !second.HasPrevious {
			t.Fatal("真实 API 下一页读取失败")
		}
		previous, err := client.List(ctx, Query{Limit: 2, Page: second.Page - 1})
		if err != nil || previous.Page != 1 || previous.HasPrevious {
			t.Fatal("真实 API 上一页读取失败")
		}
	}
	videoID := first.Items[0].ID
	if configuredID := strings.TrimSpace(os.Getenv("EMBY_AVDB_TEST_MOVIE_ID")); configuredID != "" {
		videoID = EncodeID(configuredID, "javdb")
	}
	video, err := client.Get(ctx, videoID)
	if err != nil || video.ID != videoID || video.ServiceCode != "javdb" {
		t.Fatal("真实 API 详情身份校验失败")
	}
	resources, err := client.Resources(ctx, video, 0)
	if err != nil {
		t.Fatal("真实 API 磁力资源读取失败")
	}
	resourceIDs := map[string]bool{}
	for _, resource := range resources.Items {
		if SafeMagnet(resource.Magnet) == "" || magnetID(resource.Magnet) != resource.ID || resourceIDs[resource.ID] || resource.Number != video.Number || len(resource.Origins) == 0 {
			t.Fatal("真实 API 磁力归属、规范哈希或稳定 ID 不正确")
		}
		resourceIDs[resource.ID] = true
		previous := -1
		for _, origin := range resource.Origins {
			order := -1
			switch origin {
			case "online":
				order = 0
			case "library":
				order = 1
			}
			if order < 0 || order <= previous {
				t.Fatal("真实 API 磁力来源未知、重复或顺序不稳定")
			}
			previous = order
		}
	}
	if len(resources.Items) > 0 {
		resource, err := client.Resource(ctx, video, resources.Items[0].ID)
		if err != nil || resource.ID != resources.Items[0].ID {
			t.Fatal("真实 API 资源归属查询失败")
		}
	}
	t.Logf("只读 JavDB 验收通过：首页面 %d 条，剧照 %d 张，磁力资源 %d 条", len(first.Items), len(video.Images), len(resources.Items))
}

func TestR18LiveMovieOverrideUsesSpecifiedResources(t *testing.T) {
	details, resources := 0, 0
	client := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/v1/javdb/movies/latest":
			writeFixtureResponse(w, map[string]any{"movies": []apiMovie{fixtureMovie(1)}, "current_page": 1, "has_next": false})
		case "/api/v1/javdb/movies/movie009":
			details++
			writeFixtureResponse(w, map[string]any{"movie": fixtureMovie(9)})
		case "/api/v1/javdb/movies/movie009/magnets":
			resources++
			if req.URL.Query().Get("number") != fixtureMovie(9).Number {
				t.Error("指定影片的磁力请求错误地使用了首页番号")
			}
			magnet := "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)
			writeFixtureResponse(w, map[string]any{
				"online_magnets":  []apiMagnet{{Name: "Fixture resource", MagnetURL: magnet}},
				"library_magnets": []apiMagnet{{Name: "Fixture local duplicate", MagnetURL: magnet, Number: fixtureMovie(9).Number}},
			})
		default:
			t.Error("影片覆盖参数未应用到详情和在线资源查询")
			w.WriteHeader(http.StatusNotFound)
		}
	})
	t.Setenv("EMBY_AVDB_TEST_URL", client.cfg.BaseURL)
	t.Setenv("EMBY_AVDB_TEST_KEY", "fixture-key")
	t.Setenv("EMBY_AVDB_TEST_MOVIE_ID", "movie009")
	TestR18LiveReadOnlyPaginationAndResources(t)
	if details != 1 || resources != 2 {
		t.Fatal("指定影片没有执行详情、磁力列表及稳定资源 ID 复查", details, resources)
	}
}

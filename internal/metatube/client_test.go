package metatube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicMediaDownloadLimitsAndCredentials(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint(chunked), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("MetaTube token leaked to public source")
				}
				if chunked {
					w.(http.Flusher).Flush()
				}
				w.Write([]byte("123456789"))
			}))
			defer ts.Close()
			client := New(ts.URL, "private-token", time.Second)
			var dst bytes.Buffer
			_, err := client.DownloadTo(context.Background(), ts.URL, &dst, 5, time.Second)
			if err == nil || dst.Len() > 6 {
				t.Fatalf("size bound not enforced: n=%d err=%v", dst.Len(), err)
			}
			if _, err := client.Download(context.Background(), ts.URL, 5); err == nil {
				t.Fatal("byte download silently truncated")
			}
		})
	}
}

func TestDMMMediaHeaders(t *testing.T) {
	for _, host := range []string{"dmm.co.jp", "awsimgsrc.dmm.co.jp", "cc3001.dmm.co.jp", "dmm.com", "example.org", "dmm.co.jp.example.org"} {
		req, _ := http.NewRequest("GET", "https://"+host+"/media", nil)
		setMediaHeaders(req)
		want := host != "example.org" && host != "dmm.co.jp.example.org"
		if (req.Header.Get("Referer") != "") != want {
			t.Fatalf("incorrect DMM referer for %s", host)
		}
	}
}

// newTestServer 起一个假 MetaTube 后端，记录收到的请求以便断言鉴权与查询参数。
func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *[]*http.Request) {
	t.Helper()
	seen := &[]*http.Request{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clone := r.Clone(context.Background())
		*seen = append(*seen, clone)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	return New(ts.URL, "secret-token", 5*time.Second), seen
}

func TestSearchMovies(t *testing.T) {
	client, seen := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"abc","number":"ABF-018","title":"标题","provider":"fanza",
			"thumb_url":"https://t/1.jpg","cover_url":"https://c/1.jpg","score":9.5,"actors":["A"]}]}`))
	})
	results, err := client.SearchMovies(context.Background(), "ABF-018", "", true)
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(results) != 1 || results[0].Number != "ABF-018" || results[0].Provider != "fanza" {
		t.Fatalf("结果不对: %+v", results)
	}
	request := (*seen)[0]
	if got := request.URL.Path; got != "/v1/movies/search" {
		t.Errorf("路径 = %q", got)
	}
	if got := request.URL.Query().Get("q"); got != "ABF-018" {
		t.Errorf("q = %q", got)
	}
	if got := request.URL.Query().Get("fallback"); got != "true" {
		t.Errorf("fallback = %q", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Errorf("鉴权头 = %q", got)
	}
}

func TestSearchMoviesNotFound(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
	})
	results, err := client.SearchMovies(context.Background(), "nope", "", true)
	if err != nil {
		t.Fatalf("无结果不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("应返回空结果: %+v", results)
	}
}

func TestMovieInfoAndLazyParam(t *testing.T) {
	client, seen := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"abc","provider":"fanza","number":"ABF-018","title":"T",
			"summary":"S","maker":"M","label":"L","series":"Ser","genres":["剧情"],"score":8.1,
			"runtime":120,"release_date":"2024-01-02","actors":["A","B"],
			"cover_url":"https://c/1.jpg","big_cover_url":"https://c/big.jpg"}}`))
	})
	info, err := client.MovieInfo(context.Background(), "fanza", "abc", true)
	if err != nil {
		t.Fatalf("MovieInfo: %v", err)
	}
	if info.Title != "T" || info.Runtime != 120 || len(info.Actors) != 2 || info.ReleaseDate != "2024-01-02" {
		t.Fatalf("详情不对: %+v", info)
	}
	request := (*seen)[0]
	if got := request.URL.Path; got != "/v1/movies/fanza/abc" {
		t.Errorf("路径 = %q", got)
	}
	if got := request.URL.Query().Get("lazy"); got != "true" {
		t.Errorf("lazy = %q", got)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"invalid token"}}`))
	})
	_, err := client.MovieInfo(context.Background(), "p", "1", true)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("错误信息应带后端消息，得到 %q", err.Error())
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Errorf("应解出 APIError(401): %v", err)
	}
}

func TestSearchActors(t *testing.T) {
	client, seen := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1","name":"三上悠亜","provider":"fanza",
			"aliases":["みかみゆあ"],"images":["https://i/1.jpg"]}]}`))
	})
	results, err := client.SearchActors(context.Background(), "三上悠亜", "", true)
	if err != nil {
		t.Fatalf("SearchActors: %v", err)
	}
	if len(results) != 1 || results[0].Name != "三上悠亜" || len(results[0].Images) != 1 {
		t.Fatalf("演员结果不对: %+v", results)
	}
	if got := (*seen)[0].URL.Path; got != "/v1/actors/search" {
		t.Errorf("路径 = %q", got)
	}
}

func TestImageURL(t *testing.T) {
	client := New("https://mt.example.com/", "", 0)
	got := client.ImageURL("primary", "fanza", "abc", 90, "", "")
	want := "https://mt.example.com/v1/images/primary/fanza/abc?quality=90"
	if got != want {
		t.Errorf("ImageURL = %q，期望 %q", got, want)
	}
	got = client.ImageURL("thumb", "p", "i", 0, "https://remote/x.jpg", "")
	if !strings.Contains(got, "url=") || strings.Contains(got, "quality") {
		t.Errorf("带远端 url 且不指定质量时不应出现 quality: %q", got)
	}
	// badge 只在非空时出现：空串不能让上游收到 badge=。
	got = client.ImageURL("primary", "fanza", "abc", 90, "", "u.png")
	if !strings.Contains(got, "badge=u.png") {
		t.Errorf("指定 badge 时应带上参数: %q", got)
	}
	got = client.ImageURL("primary", "fanza", "abc", 90, "", "   ")
	if strings.Contains(got, "badge") {
		t.Errorf("空白 badge 不应出现参数: %q", got)
	}
}

func TestDownloadAndHealth(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte(`{"app":"metatube","version":"1.0"}`))
			return
		}
		_, _ = w.Write([]byte("binary-image"))
	})
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	data, err := client.Download(context.Background(), client.BaseURL()+"/v1/images/primary/p/i", 0)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(data) != "binary-image" {
		t.Errorf("内容 = %q", string(data))
	}
	// 图片端点不带鉴权头（后端把它归为公开端点）。
	if data, err := client.Download(context.Background(), client.BaseURL()+"/x", 0); err != nil || len(data) == 0 {
		t.Fatalf("Download 失败: %v", err)
	}
}

func TestDecodeErrorFallback(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	})
	_, err := client.MovieInfo(context.Background(), "p", "1", true)
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("非 JSON 错误体应退回原文片段，得到 %v", err)
	}
}

// asAPIError 是 errors.As 的薄封装，避免测试文件重复 import errors。
func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if apiErr, ok := err.(*APIError); ok {
			*target = apiErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// IsFatal distinguishes service failures from per-item metadata failures.
func TestIsFatal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"404 上游没有这部片", &APIError{StatusCode: 404, Message: "info not found"}, false},
		{"500 元数据不全", &APIError{StatusCode: 500, Message: "incomplete metadata"}, false},
		{"400 参数问题", &APIError{StatusCode: 400}, false},
		{"401 鉴权失败", &APIError{StatusCode: 401, Message: "invalid token"}, true},
		{"403 鉴权失败", &APIError{StatusCode: 403}, true},
		{"502 网关故障", &APIError{StatusCode: 502}, true},
		{"503 服务不可用", &APIError{StatusCode: 503}, true},
		{"504 网关超时", &APIError{StatusCode: 504}, true},
		{"429 请求过多", &APIError{StatusCode: 429}, true},
		{"连接失败", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, true},
		{"解析响应失败", &json.SyntaxError{}, false},
		{"响应缺少 data", errors.New("metatube: 响应缺少 data"), false},
		{"读取响应中断", fmt.Errorf("read response: %w", io.ErrUnexpectedEOF), true},
		{"被 wrap 的鉴权失败", fmt.Errorf("metatube: 请求 /x 失败: %w", &APIError{StatusCode: 401}), true},
		{"被 wrap 的数据问题", fmt.Errorf("metatube: 请求 /x 失败: %w", &APIError{StatusCode: 500}), false},
		{"请求取消", context.Canceled, false},
		{"请求超时", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		if got := IsFatal(tc.err); got != tc.want {
			t.Errorf("%s: IsFatal = %v，期望 %v", tc.name, got, tc.want)
		}
	}
}

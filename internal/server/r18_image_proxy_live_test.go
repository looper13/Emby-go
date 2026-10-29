package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/r18"
)

func TestR18LiveImagesViaAVdbProxy(t *testing.T) {
	baseURL, key := os.Getenv("EMBY_AVDB_TEST_URL"), os.Getenv("EMBY_AVDB_TEST_KEY")
	if baseURL == "" || key == "" {
		t.Skip("设置只读 AVdb 测试环境变量后验证真实图片代理")
	}
	a, server, token := newProbeTestApp(t, t.TempDir())
	library, err := a.db.CreateR18Library("Live image proxy", r18.Config{BaseURL: baseURL, APIKey: key})
	if err != nil {
		t.Fatal("创建临时图片资料库失败")
	}
	movie := strings.TrimSpace(os.Getenv("EMBY_AVDB_TEST_MOVIE_ID"))
	if movie == "" {
		movie = "z4KGO5"
	}
	base := "/api/r18/libraries/" + strconv.FormatInt(library.ID, 10) + "/videos/" + r18.EncodeID(movie, "javdb")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 23 * time.Second}
	get := func(path, tag string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal("创建临时实例图片请求失败")
		}
		// 与浏览器相同，图片请求只使用第一方登录 Cookie。
		req.AddCookie(&http.Cookie{Name: "emby_session", Value: token})
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("临时实例真实图片请求失败或超时")
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, r18ImageLimit+1))
		if err != nil || len(body) > r18ImageLimit || bytes.Contains(body, []byte(key)) {
			t.Fatal("真实图片响应读取失败、超限或包含凭据")
		}
		return resp, body
	}
	resp, body := get(base, "")
	var detail struct {
		Video r18.Video `json:"video"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &detail) != nil || detail.Video.CoverURL != base+"/images/poster" || len(detail.Video.Images) == 0 {
		t.Fatal("真实影片没有有效同源封面与剧照资料")
	}
	for _, path := range []string{detail.Video.CoverURL, base + "/images/cover", detail.Video.Images[0]} {
		start := time.Now()
		resp, body := get(path, "")
		mime := http.DetectContentType(body)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(mime, "image/") || resp.Header.Get("Content-Type") != mime || resp.Header.Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("真实代理图片失败：status=%d type=%s bytes=%d", resp.StatusCode, mime, len(body))
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || config.Width <= 0 || config.Height <= 0 {
			t.Fatal("真实代理图片无法解码")
		}
		t.Logf("真实图片通过：type=%s bytes=%d dimensions=%dx%d duration=%s", mime, len(body), config.Width, config.Height, time.Since(start).Round(time.Millisecond))
		tag := resp.Header.Get("ETag")
		resp, body = get(path, tag)
		if tag == "" || resp.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Fatal("真实图片缓存重验证失败")
		}
	}
}

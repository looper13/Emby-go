package r18

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type previewTestTransport func(*http.Request) (*http.Response, error)

func (f previewTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestR18PreviewRequestRejectsUnsafeURLsAndRange(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:secret@8.8.8.8/sample.ts", "https://8.8.8.8/sample.ts#fragment", "https://8.8.8.8/a\\b", "https://8.8.8.8/" + strings.Repeat("a", maxPreviewURLBytes)} {
		if _, err := PreviewRequest(context.Background(), raw, ""); err == nil {
			t.Error("未拒绝不安全的预览地址")
		}
	}
	for _, value := range []string{"bytes=", "bytes=abc", "bytes=1-2\r\nX-API-Key: secret", "items=1-2"} {
		if _, err := PreviewRequest(context.Background(), "https://8.8.8.8/sample.ts", value); err == nil {
			t.Error("未拒绝无效 Range")
		}
	}
	for _, value := range []string{"", "bytes=2-4", "bytes=2-", "bytes=-4", "bytes=0-1,4-5"} {
		req, err := PreviewRequest(context.Background(), "https://8.8.8.8/sample.ts?signature=a%2Bb", value)
		if err != nil || req.Header.Get("Range") != value || req.URL.RawQuery != "signature=a%2Bb" {
			t.Fatal("预览地址签名或合法 Range 未保留")
		}
	}
}

func TestR18PreviewTransportDropsCredentialsAndPinsPublicDNS(t *testing.T) {
	called := 0
	transport := previewTransport{base: imageTransport{
		base: previewTestTransport(func(req *http.Request) (*http.Response, error) {
			called++
			for _, key := range []string{"X-API-Key", "Cookie", "Authorization", "X-Emby-Token", "Referer", "X-Forwarded-For"} {
				if req.Header.Get(key) != "" {
					t.Errorf("公网预览收到敏感请求头：%s", key)
				}
			}
			if req.Header.Get("Range") != "bytes=2-4" || req.Header.Get("Accept-Encoding") != "identity" || req.Method != http.MethodGet {
				t.Error("预览未保留 Range、禁压缩或固定方法")
			}
			target, ok := req.Context().Value(imageTargetKey{}).(imageTarget)
			if !ok || target.host != "preview.example" || len(target.ips) != 1 || !target.ips[0].IP.Equal(net.ParseIP("8.8.8.8")) {
				t.Error("预览未绑定已验证的公网 DNS 地址")
			}
			return &http.Response{StatusCode: 206, Body: io.NopCloser(strings.NewReader("cde")), Header: http.Header{}, Request: req}, nil
		}),
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		},
	}}
	req, err := PreviewRequest(context.Background(), "https://preview.example/sample.ts?signature=a%2Bb", "bytes=2-4")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"X-API-Key", "Cookie", "Authorization", "X-Emby-Token", "Referer", "X-Forwarded-For"} {
		req.Header.Set(key, "private-fixture")
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if called != 1 {
		t.Fatal("预览未使用公网校验客户端")
	}
}

func TestR18PreviewTransportRejectsPrivateAndMixedDNS(t *testing.T) {
	for _, ips := range [][]net.IPAddr{
		{{IP: net.ParseIP("127.0.0.1")}}, {{IP: net.ParseIP("192.168.1.1")}}, {{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}},
	} {
		called := false
		transport := previewTransport{base: imageTransport{
			base:   previewTestTransport(func(req *http.Request) (*http.Response, error) { called = true; return nil, errors.New("unexpected") }),
			lookup: func(context.Context, string) ([]net.IPAddr, error) { return ips, nil },
		}}
		req, _ := PreviewRequest(context.Background(), "https://preview.example/sample.ts", "")
		if _, err := transport.RoundTrip(req); err == nil || called {
			t.Fatal("预览未拒绝私网或混合 DNS")
		}
	}
}

func TestR18PreviewRedirectIsRevalidatedWithoutCredentials(t *testing.T) {
	client := NewPreviewClient()
	calls := 0
	client.Transport = previewTransport{base: imageTransport{
		lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
			ip := "8.8.8.8"
			if host == "private.example" {
				ip = "127.0.0.1"
			}
			return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
		},
		base: previewTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://private.example/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}),
	}}
	req, _ := PreviewRequest(context.Background(), "https://preview.example/master.m3u8", "")
	if _, err := client.Do(req); err == nil || calls != 1 {
		t.Fatal("预览重定向未重新校验公网地址")
	}
}

func TestR18PreviewDMMRefererAndCancellation(t *testing.T) {
	for _, host := range []string{"dmm.co.jp", "cc3001.dmm.co.jp", "dmm.com", "cc3001.dmm.com"} {
		req, err := PreviewRequest(context.Background(), "https://"+host+"/preview.m3u8", "")
		if err != nil || req.Header.Get("Referer") != "https://www.dmm.co.jp/" {
			t.Fatal("DMM 预览缺少固定公开 Referer")
		}
	}
	for _, host := range []string{"notdmm.com", "dmm.com.evil.example"} {
		req, _ := PreviewRequest(context.Background(), "https://"+host+"/preview.m3u8", "")
		if req.Header.Get("Referer") != "" {
			t.Fatal("Referer 域名判断越界")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := PreviewRequest(ctx, "https://8.8.8.8/preview.m3u8", "")
	if _, err := NewPreviewClient().Do(req); !errors.Is(err, context.Canceled) {
		t.Fatal("预览没有保留取消语义")
	}
}

func TestR18PreviewConcurrencySlotCoversBodyAndCanceledWait(t *testing.T) {
	transport := previewTransport{base: previewTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fixture")), Header: http.Header{}, Request: req}, nil
	})}
	var bodies []io.ReadCloser
	t.Cleanup(func() {
		for _, body := range bodies {
			body.Close()
		}
	})
	for i := 0; i < cap(previewSlots); i++ {
		req, _ := PreviewRequest(context.Background(), "https://8.8.8.8/segment.ts", "")
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, resp.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := PreviewRequest(ctx, "https://8.8.4.4/segment.ts", "")
	if _, err := transport.RoundTrip(req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("响应体尚未关闭时，跨域请求绕过四路限制或取消失效")
	}
	bodies[0].Close()
	bodies[0].Close()
	req, _ = PreviewRequest(context.Background(), "https://8.8.4.4/segment.ts", "")
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal("关闭响应体没有释放槽位")
	}
	bodies = append(bodies, resp.Body)
}

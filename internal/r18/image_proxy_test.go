package r18

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func proxyPublicLookup(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("2606:4700::1111")}}, nil
}

func TestR18ProxyImageRequestContract(t *testing.T) {
	raw := "https://images.example/covers/a.jpg?signature=fixture%2Bkey%2Fvalue%3D&expires=42&label=a+b&empty="
	for _, base := range []string{"https://avdb.invalid", "http://192.168.50.20:8800/", "https://avdb.invalid/prefix/nested/"} {
		req, err := ProxyImageRequest(context.Background(), base, raw)
		if err != nil {
			t.Fatal(err)
		}
		baseURL, _ := url.Parse(strings.TrimRight(base, "/"))
		if req.Method != http.MethodGet || req.Body != nil || req.URL.Scheme != baseURL.Scheme || req.URL.Host != baseURL.Host || req.URL.Path != baseURL.Path+"/api/v1/img-proxy/" {
			t.Fatal("固定代理地址或路径前缀改变")
		}
		query := req.URL.Query()
		if len(query) != 1 || len(query["url"]) != 1 || query.Get("url") != raw || req.URL.RawQuery != (url.Values{"url": {raw}}).Encode() {
			t.Fatal("完整签名原图地址未编码为唯一 url 参数")
		}
		for _, header := range []string{"X-API-Key", "Cookie", "Authorization", "Referer", "X-Emby-Token", "X-MediaBrowser-Token"} {
			if req.Header.Get(header) != "" {
				t.Fatal("公开代理请求包含凭据", header)
			}
		}
		if req.Header.Get("Accept") == "" || req.Header.Get("User-Agent") == "" {
			t.Fatal("图片请求缺少内容协商标头")
		}
	}
	for _, base := range []string{"", "file:///avdb", "https://user:fixture-key@avdb.invalid", "https://avdb.invalid?key=fixture-key", "https://avdb.invalid?", "https://avdb.invalid#fragment", "https://avdb.invalid#", "https://avdb.invalid/\\path"} {
		if _, err := ProxyImageRequest(context.Background(), base, raw); err == nil {
			t.Fatal("无效配置基础地址被接受")
		}
	}
	for _, raw := range []string{"", "file:///cover.jpg", "//images.example/a.jpg", "https://user:fixture-key@images.example/a.jpg", "javascript:alert(1)"} {
		if _, err := ProxyImageRequest(context.Background(), "https://avdb.invalid", raw); err == nil {
			t.Fatal("无效原图地址被接受")
		}
	}
	if _, err := ProxyImageRequest(nil, "https://avdb.invalid", raw); err == nil {
		t.Fatal("空上下文被接受")
	}
}

func TestR18ProxyImagePublicDNSBoundary(t *testing.T) {
	tests := []struct {
		name string
		ips  []net.IPAddr
		err  error
	}{
		{name: "私网", ips: []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}},
		{name: "回环", ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}},
		{name: "混合地址", ips: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("192.168.1.2")}}},
		{name: "混合IPv6", ips: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("fc00::1")}}},
		{name: "IPv6区域", ips: []net.IPAddr{{IP: net.ParseIP("2606:4700::1111"), Zone: "eth0"}}},
		{name: "CGNAT", ips: []net.IPAddr{{IP: net.ParseIP("100.100.100.200")}}},
		{name: "NAT64", ips: []net.IPAddr{{IP: net.ParseIP("64:ff9b::a00:1")}}},
		{name: "无地址"},
		{name: "DNS异常", err: errors.New("fixture-key upstream failure")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			transport := imageProxyTransport{
				lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
					if host != "images.example" {
						t.Fatal("公网校验错误地解析了管理员私网 AVdb 地址")
					}
					return tt.ips, tt.err
				},
				base: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return nil, errors.New("不得请求上游")
				}),
			}
			req, _ := ProxyImageRequest(context.Background(), "http://192.168.50.20:8800", "https://images.example/a.jpg")
			if _, err := transport.RoundTrip(req); err == nil || calls != 0 || strings.Contains(err.Error(), "fixture-key") {
				t.Fatal("不安全 DNS 到达上游或错误泄露上游信息", calls)
			}
		})
	}
	for _, raw := range []string{"http://8.8.8.8/a.jpg", "https://[2606:4700::1111]/a.jpg", "https://b\u00fccher.example/a.jpg"} {
		req, _ := ProxyImageRequest(context.Background(), "http://192.168.50.20:8800", raw)
		transport := imageProxyTransport{
			lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
				if strings.Contains(host, "\u00fc") || strings.ContainsAny(host, "[]") {
					t.Fatal("原图域名未完成 IDNA/IP 规范化")
				}
				return proxyPublicLookup(context.Background(), host)
			},
			base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "192.168.50.20:8800" || req.Context().Value(imageTargetKey{}) != nil {
					t.Fatal("原图 IP 被错误用于连接固定 AVdb 地址")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
			}),
		}
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal("有效公网原图或管理员私网代理被拒绝", err)
		}
		resp.Body.Close()
	}
}

func TestR18ProxyImageRejectsRequestMutation(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*http.Request) *http.Request
	}{
		{name: "丢失上下文", edit: func(req *http.Request) *http.Request { return req.WithContext(context.Background()) }},
		{name: "改变主机", edit: func(req *http.Request) *http.Request { req.URL.Host = "other.invalid"; return req }},
		{name: "改变路径", edit: func(req *http.Request) *http.Request { req.URL.Path = "/api/v1/other"; return req }},
		{name: "改变原图", edit: func(req *http.Request) *http.Request { req.URL.RawQuery = "url=http%3A%2F%2F127.0.0.1%2F"; return req }},
		{name: "改变Host", edit: func(req *http.Request) *http.Request { req.Host = "other.invalid"; return req }},
		{name: "改变方法", edit: func(req *http.Request) *http.Request { req.Method = http.MethodPost; return req }},
		{name: "添加请求体", edit: func(req *http.Request) *http.Request {
			req.Body = io.NopCloser(strings.NewReader("fixture-key"))
			return req
		}},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			transport := imageProxyTransport{
				lookup: func(context.Context, string) ([]net.IPAddr, error) {
					calls++
					return proxyPublicLookup(context.Background(), "")
				},
				base: imageRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }),
			}
			req, _ := ProxyImageRequest(context.Background(), "https://avdb.invalid", "https://images.example/a.jpg")
			req = tt.edit(req)
			if req.Body != nil {
				defer req.Body.Close()
			}
			if _, err := transport.RoundTrip(req); err == nil || calls != 0 {
				t.Fatal("改变的代理请求被执行")
			}
		})
	}
	transport := NewProxyImageClient().Transport
	if _, err := transport.RoundTrip(nil); err == nil {
		t.Fatal("空代理请求被接受")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/img-proxy/?url=http://127.0.0.1/", nil)
	if _, err := transport.RoundTrip(req); err == nil {
		t.Fatal("未授权通用请求被接受")
	}
}

func TestR18ProxyImageCredentialsAndMIMEIsolation(t *testing.T) {
	for _, contentType := range []string{"image/jpeg", "application/octet-stream", "text/html"} {
		transport := imageProxyTransport{
			lookup: proxyPublicLookup,
			base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if len(req.Header) != 2 || req.Trailer != nil || req.TransferEncoding != nil {
					t.Fatal("公开代理继承调用方标头或尾部凭据")
				}
				for _, header := range []string{"X-API-Key", "Cookie", "Authorization", "Referer", "X-Emby-Token", "X-MediaBrowser-Token"} {
					if req.Header.Get(header) != "" {
						t.Fatal("公开代理泄露调用方凭据", header)
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader("fixture-body"))}, nil
			}),
		}
		req, _ := ProxyImageRequest(context.Background(), "https://avdb.invalid", "https://images.example/a.jpg")
		for _, header := range []string{"X-API-Key", "Cookie", "Authorization", "Referer", "X-Emby-Token", "X-MediaBrowser-Token"} {
			req.Header.Set(header, "fixture-key")
		}
		req.Trailer = http.Header{"X-API-Key": {"fixture-key"}}
		resp, err := transport.RoundTrip(req)
		if err != nil || resp.Header.Get("Content-Type") != contentType {
			t.Fatal("代理客户端提前判断或改变 MIME，内容校验应交由服务器")
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "fixture-body" {
			t.Fatal("代理客户端改变了响应内容")
		}
	}
}

func TestR18ProxyImageClientRejectsRedirectsAndCancels(t *testing.T) {
	client := NewProxyImageClient()
	base := client.Transport.(imageProxyTransport).base.(*http.Transport)
	if client.Timeout != 15*time.Second || base.MaxConnsPerHost != 4 || base.ResponseHeaderTimeout != 8*time.Second || client.Jar != nil {
		t.Fatal("图片代理连接或时间预算错误")
	}
	defer client.CloseIdleConnections()
	for _, location := range []string{"http://127.0.0.1/private", "https://images.example/a.jpg", "/api/v1/img-proxy/?url=other"} {
		calls := 0
		client.Transport = imageProxyTransport{
			lookup: proxyPublicLookup,
			base: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: http.NoBody, Request: req}, nil
			}),
		}
		req, _ := ProxyImageRequest(context.Background(), "https://avdb.invalid", "https://images.example/a.jpg")
		resp, err := client.Do(req)
		if err != nil || calls != 1 || resp.StatusCode != http.StatusFound {
			t.Fatal("图片代理跟随了上游重定向")
		}
		resp.Body.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := ProxyImageRequest(ctx, "https://avdb.invalid", "https://images.example/a.jpg")
	calls := 0
	client.Transport = imageProxyTransport{
		lookup: func(context.Context, string) ([]net.IPAddr, error) { calls++; return nil, nil },
		base:   imageRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }),
	}
	if _, err := client.Do(req); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("已取消的图片代理请求仍查询 DNS 或请求上游")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	req, _ = ProxyImageRequest(ctx, "https://avdb.invalid", "https://images.example/a.jpg")
	client.Transport = imageProxyTransport{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			cancel()
			return proxyPublicLookup(context.Background(), "")
		},
		base: imageRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }),
	}
	if _, err := client.Do(req); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("DNS 校验期间取消后仍请求上游")
	}
}

func TestR18ProxyImageReturnsDecodedJPEGFixture(t *testing.T) {
	// 仿真源站的非标准图片字节由固定 AVdb 代理转换，客户端不自行猜测解密。
	raw := "https://images.example/covers/fixture.jpg?signature=fixture%2Bkey&expires=42"
	encoded := []byte{0xc8, 0x37, 0x10, 0x37, 0x13, 0xc8, 0x8b, 0xc8}
	var decoded bytes.Buffer
	if err := jpeg.Encode(&decoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	jpegBody := decoded.Bytes()
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.URL.Path != "/prefix/api/v1/img-proxy/" || req.URL.Query().Get("url") != raw || req.Header.Get("X-API-Key") != "" {
			t.Error("固定代理路径、完整原图参数或凭据隔离错误")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegBody)
	}))
	defer proxy.Close()
	client := NewProxyImageClient()
	transport := client.Transport.(imageProxyTransport)
	transport.lookup = proxyPublicLookup
	client.Transport = transport
	defer client.CloseIdleConnections()
	req, err := ProxyImageRequest(context.Background(), proxy.URL+"/prefix", raw)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || calls != 1 || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/jpeg" || !bytes.Equal(body, jpegBody) || bytes.Equal(body, encoded) {
		t.Fatal("未使用固定代理返回的标准 JPEG 字节")
	}
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Fatal("固定代理夹具未返回可解码 JPEG", err)
	}
}

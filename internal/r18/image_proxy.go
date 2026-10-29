package r18

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

type imageProxyTargetKey struct{}

type imageProxyTarget struct {
	endpoint string
	host     string
}

type imageProxyTransport struct {
	base   http.RoundTripper
	lookup func(context.Context, string) ([]net.IPAddr, error)
}

func (t imageProxyTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// 只信任管理员配置的固定代理地址，原图仍必须通过全部 DNS 公网校验。
func (t imageProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("无效的 AVdb 图片代理请求")
	}
	target, ok := req.Context().Value(imageProxyTargetKey{}).(imageProxyTarget)
	if !ok || target.endpoint == "" || target.host == "" || req.URL.String() != target.endpoint || req.Method != http.MethodGet || req.Body != nil || (req.Host != "" && req.Host != req.URL.Host) {
		return nil, errors.New("AVdb 图片代理请求未授权或已改变")
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if _, err := resolvePublicImageTarget(req.Context(), target.host, t.lookup); err != nil {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("AVdb 图片原图未通过公网校验")
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	// 调用方后续添加的 API Key、Cookie 和浏览器凭据也不能进入公开代理。
	req, err := http.NewRequestWithContext(req.Context(), http.MethodGet, target.endpoint, nil)
	if err != nil {
		return nil, errors.New("AVdb 图片代理请求创建失败")
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")
	req.Header.Set("User-Agent", "Emby-go/R18-Images")
	return t.base.RoundTrip(req)
}

func ProxyImageRequest(ctx context.Context, baseURL, raw string) (*http.Request, error) {
	if ctx == nil {
		return nil, errors.New("AVdb 图片代理请求缺少上下文")
	}
	// 借用配置地址校验，但不读取或发送真实 API Key。
	cfg := Config{BaseURL: baseURL, APIKey: "image-proxy-validation"}
	if err := cfg.Validate(); err != nil {
		return nil, errors.New("无效的 AVdb 图片代理基础地址")
	}
	raw = SafeHTTPURL(raw)
	if raw == "" {
		return nil, errors.New("无效的 AVdb 图片原图地址")
	}
	source, err := url.Parse(raw)
	if err != nil || source.Hostname() == "" {
		return nil, errors.New("无效的 AVdb 图片原图地址")
	}
	u, err := url.Parse(cfg.BaseURL + "/api/v1/img-proxy/")
	if err != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return nil, errors.New("无效的 AVdb 图片代理基础地址")
	}
	u.RawQuery = url.Values{"url": {raw}}.Encode()
	target := imageProxyTarget{endpoint: u.String(), host: source.Hostname()}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, imageProxyTargetKey{}, target), http.MethodGet, target.endpoint, nil)
	if err != nil {
		return nil, errors.New("AVdb 图片代理请求创建失败")
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")
	req.Header.Set("User-Agent", "Emby-go/R18-Images")
	return req, nil
}

func NewProxyImageClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 4
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{
		Transport: imageProxyTransport{base: transport, lookup: net.DefaultResolver.LookupIPAddr},
		Timeout:   15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

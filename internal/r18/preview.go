package r18

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

const maxPreviewURLBytes = 8192

var previewRange = regexp.MustCompile(`^bytes=(?:[0-9]+-[0-9]*|-[0-9]+)(?:[ \t]*,[ \t]*(?:[0-9]+-[0-9]*|-[0-9]+))*$`)
var previewSlots = make(chan struct{}, 4)

type previewTransport struct{ base http.RoundTripper }

type previewBody struct {
	io.ReadCloser
	once sync.Once
}

func (b *previewBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { <-previewSlots })
	return err
}

func (t previewTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.Method != http.MethodGet || req.Body != nil {
		return nil, errors.New("无效的预览资源请求")
	}
	// 每次请求及重定向都重建请求头，API Key 和浏览器凭据不能进入公网 CDN。
	clean, err := PreviewRequest(req.Context(), req.URL.String(), req.Header.Get("Range"))
	if err != nil {
		return nil, err
	}
	select {
	case previewSlots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	resp, err := t.base.RoundTrip(clean)
	if err != nil || resp == nil || resp.Body == nil {
		<-previewSlots
		return resp, err
	}
	// 槽位覆盖响应体读取，跨域分片也最多四路，未知长度缓冲保持有界。
	resp.Body = &previewBody{ReadCloser: resp.Body}
	return resp, nil
}

func (t previewTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func NewPreviewClient() *http.Client {
	// 复用逐次完整 DNS 公网检查、固定 IP 拨号、超时及连接数限制。
	client := NewImageClient()
	client.Transport = previewTransport{base: client.Transport}
	return client
}

func PreviewRequest(ctx context.Context, raw, byteRange string) (*http.Request, error) {
	if ctx == nil || len(raw) > maxPreviewURLBytes || strings.ContainsAny(raw, "\\\x00\r\n\t") || SafeHTTPURL(raw) == "" {
		return nil, errors.New("无效的预览资源地址")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("无效的预览资源地址")
	}
	if len(byteRange) > 256 || (byteRange != "" && !previewRange.MatchString(byteRange)) {
		return nil, errors.New("无效的预览 Range 请求")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("预览资源请求创建失败")
	}
	req.Header.Set("Accept", "application/vnd.apple.mpegurl,application/x-mpegurl,video/*,application/octet-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "Emby-go/R18-Preview")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	host := strings.ToLower(u.Hostname())
	if host == "dmm.co.jp" || strings.HasSuffix(host, ".dmm.co.jp") || host == "dmm.com" || strings.HasSuffix(host, ".dmm.com") {
		req.Header.Set("Referer", "https://www.dmm.co.jp/")
	}
	return req, nil
}

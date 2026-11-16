package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

// HLS 会话只保存来源边界，每次请求重新读取媒体库的最新请求头。
type proxyPlaybackSettings struct {
	libraryID int64
	movieID   int64
	sourceURL string
	headers   map[string]string
}

func playbackProxySettings(library store.Library, sourceURL string) proxyPlaybackSettings {
	if !library.IsDirectory() || !library.Enabled || !library.ProxyEnabled {
		return proxyPlaybackSettings{}
	}
	return proxyPlaybackSettings{libraryID: library.ID, sourceURL: sourceURL, headers: library.ProxyHeaders}
}

func sensitiveProxyHeader(name string) bool {
	name = strings.ToLower(name)
	return name == "authorization" || name == "cookie" || name == "cookie2" || name == "www-authenticate" || strings.Contains(name, "token") || strings.Contains(name, "api-key") || strings.Contains(name, "apikey") || name == "x-auth-key"
}

func sameProxyOrigin(source, target *url.URL) bool {
	return source != nil && target != nil && strings.EqualFold(source.Scheme, target.Scheme) && strings.EqualFold(source.Host, target.Host)
}

func proxyHeadersForTarget(settings proxyPlaybackSettings, raw string) map[string]string {
	if len(settings.headers) == 0 {
		return nil
	}
	source, _ := url.Parse(settings.sourceURL)
	target, _ := url.Parse(raw)
	headers := make(map[string]string, len(settings.headers))
	for name, value := range settings.headers {
		if !sensitiveProxyHeader(name) || sameProxyOrigin(source, target) {
			headers[name] = value
		}
	}
	return headers
}

func playbackExternalURL(c *gin.Context, id, raw string, library store.Library) (string, error) {
	if !library.ProxyEnabled {
		return libraryPlaybackURL(c, raw, library)
	}
	prefix := ""
	if strings.HasPrefix(strings.ToLower(c.Request.URL.Path), "/emby/") {
		prefix = "/emby"
	}
	query := url.Values{"Static": {"true"}, "MediaSourceId": {"mediasource_" + id}}
	if token := requestToken(c); token != "" {
		query.Set("api_key", token)
	}
	return requestScheme(c) + "://" + c.Request.Host + prefix + streamURL(id) + "?" + query.Encode(), nil
}

// 库头从不缓存；关闭代理或删除库时撤销旧 HLS 能力，字幕提取缓存也同步失效。
func (a *App) invalidateLibraryPlayback(id int64) {
	library, err := a.db.Library(id)
	if err != nil || !library.Enabled || !library.IsDirectory() || !library.ProxyEnabled {
		a.hlsProxyMu.Lock()
		for sessionID, session := range a.hlsProxySessions {
			if session.libraryID == id {
				delete(a.hlsProxySessions, sessionID)
			}
		}
		a.hlsProxyMu.Unlock()
	}
	a.mediaMu.Lock()
	if a.subtitleBytes != nil {
		a.subtitleBytes.Clear()
	}
	a.mediaMu.Unlock()
}

// 克隆客户端再加重定向规则，避免并发请求修改全局客户端。
func proxyClientWithHeaders(headers map[string]string) *http.Client {
	client := *streamClient
	previous := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameProxyOrigin(via[0].URL, request.URL) {
			for name := range headers {
				if sensitiveProxyHeader(name) {
					request.Header.Del(name)
				}
			}
		}
		// 防盗链头是管理员明确配置的来源标识，跨 CDN 或协议重定向也保持原值。
		for _, name := range []string{"Referer", "Origin"} {
			if value, exists := headers[name]; exists {
				request.Header.Set(name, value)
			}
		}
		if previous != nil {
			return previous(request, via)
		}
		if len(via) >= 10 {
			return errors.New("播放源重定向次数过多")
		}
		return nil
	}
	return &client
}

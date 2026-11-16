package store

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"golang.org/x/net/http/httpguts"
)

const (
	maxProxyHeaderCount      = 32
	maxProxyHeaderNameBytes  = 256
	maxProxyHeaderValueBytes = 8192
	maxProxyHeaderBytes      = 32768
)

// NormalizeProxyHeaders 校验播放源请求头，代理控制的协议字段不能由媒体库覆盖。
func NormalizeProxyHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) > maxProxyHeaderCount {
		return nil, fmt.Errorf("播放源请求头不能超过 %d 项", maxProxyHeaderCount)
	}
	normalized := make(map[string]string, len(headers))
	total := 0
	for rawName, rawValue := range headers {
		name := strings.TrimSpace(rawName)
		if strings.ContainsFunc(rawName, unicode.IsControl) || len(name) > maxProxyHeaderNameBytes || !httpguts.ValidHeaderFieldName(name) {
			return nil, fmt.Errorf("播放源请求头名称无效：%q", name)
		}
		name = http.CanonicalHeaderKey(name)
		switch name {
		case "Host", "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade", "Content-Length", "Content-Encoding", "Accept-Encoding", "Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "Expect", "X-Emby-Token", "X-Mediabrowser-Token", "X-Emby-Authorization":
			return nil, fmt.Errorf("播放源请求头 %s 由代理管理，不能覆盖", name)
		}
		if _, exists := normalized[name]; exists {
			return nil, fmt.Errorf("播放源请求头名称重复：%s", name)
		}
		if len(rawValue) > maxProxyHeaderValueBytes || !httpguts.ValidHeaderFieldValue(rawValue) || strings.ContainsFunc(rawValue, unicode.IsControl) {
			return nil, fmt.Errorf("播放源请求头 %s 的值无效或过长", name)
		}
		value := strings.TrimSpace(rawValue)
		total += len(name) + len(value)
		if total > maxProxyHeaderBytes {
			return nil, fmt.Errorf("播放源请求头总大小不能超过 %d 字节", maxProxyHeaderBytes)
		}
		normalized[name] = value
	}
	return normalized, nil
}

package server

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

const (
	maxHLSPlaylistBytes = 1 << 20
	maxHLSSessionCount  = 64
	maxHLSResourceCount = 8192
	maxHLSResourceBytes = 1 << 20
	hlsProxySessionTTL  = 30 * time.Minute
)

var hlsURIAttribute = regexp.MustCompile(`URI="([^"]*)"`)

type hlsProxySession struct {
	item      string
	userID    int64
	expires   time.Time
	nextID    uint64
	byteCount int
	resources map[string]string
}

func (a *App) newHLSProxySession(item string) (string, error) {
	id := randomSessionID()
	if id == "" {
		return "", errors.New("生成 HLS 代理会话 ID 失败")
	}
	now := time.Now()
	a.hlsProxyMu.Lock()
	defer a.hlsProxyMu.Unlock()
	if a.hlsProxySessions == nil {
		a.hlsProxySessions = make(map[string]*hlsProxySession)
	}
	for key, session := range a.hlsProxySessions {
		if now.After(session.expires) {
			delete(a.hlsProxySessions, key)
		}
	}
	if len(a.hlsProxySessions) >= maxHLSSessionCount {
		var oldestID string
		var oldest time.Time
		for key, session := range a.hlsProxySessions {
			if oldestID == "" || session.expires.Before(oldest) {
				oldestID, oldest = key, session.expires
			}
		}
		delete(a.hlsProxySessions, oldestID)
	}
	a.hlsProxySessions[id] = &hlsProxySession{
		item:      item,
		userID:    a.db.UserID(),
		expires:   now.Add(hlsProxySessionTTL),
		resources: make(map[string]string),
	}
	return id, nil
}

func (a *App) addHLSProxyResource(item, sessionID, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("HLS 资源 URI 不是有效 HTTP 地址")
	}
	now := time.Now()
	a.hlsProxyMu.Lock()
	defer a.hlsProxyMu.Unlock()
	session := a.hlsProxySessions[sessionID]
	if session == nil || session.item != item || now.After(session.expires) {
		return "", errors.New("HLS 代理会话已失效")
	}
	if len(session.resources) >= maxHLSResourceCount {
		return "", errors.New("HLS 资源数量超过限制")
	}
	resourceURL := parsed.String()
	if session.byteCount+len(resourceURL) > maxHLSResourceBytes {
		return "", errors.New("HLS 资源地址总大小超过限制")
	}
	resourceID := "r" + strconv.FormatUint(session.nextID, 36)
	session.nextID++
	session.resources[resourceID] = resourceURL
	session.byteCount += len(resourceURL)
	session.expires = now.Add(hlsProxySessionTTL)
	return resourceID, nil
}

func (a *App) hlsProxyTarget(item, sessionID, resourceID string) (string, int64, bool) {
	now := time.Now()
	a.hlsProxyMu.Lock()
	defer a.hlsProxyMu.Unlock()
	session := a.hlsProxySessions[sessionID]
	if session == nil {
		return "", 0, false
	}
	if now.After(session.expires) {
		delete(a.hlsProxySessions, sessionID)
		return "", 0, false
	}
	if session.item != item {
		return "", 0, false
	}
	target, ok := session.resources[resourceID]
	if !ok {
		return "", 0, false
	}
	session.expires = now.Add(hlsProxySessionTTL)
	return target, session.userID, true
}

func (a *App) rewriteHLSPlaylist(c *gin.Context, item, sessionID, base string, body []byte) ([]byte, error) {
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, errors.New("HLS 播放列表地址无效")
	}
	rewriteURI := func(raw string) (string, error) {
		ref, parseErr := url.Parse(strings.TrimSpace(raw))
		if parseErr != nil {
			return "", parseErr
		}
		target := baseURL.ResolveReference(ref)
		if target.Scheme != "http" && target.Scheme != "https" {
			return raw, nil
		}
		resourceID, addErr := a.addHLSProxyResource(item, sessionID, target.String())
		if addErr != nil {
			return "", addErr
		}
		prefix := ""
		if strings.HasPrefix(strings.ToLower(c.Request.URL.Path), "/emby/") {
			prefix = "/emby"
		}
		return prefix + "/Videos/" + url.PathEscape(item) + "/hls-proxy/" + sessionID + "/" + resourceID, nil
	}
	return rewriteHLSPlaylistURIs(body, rewriteURI)
}

func rewriteHLSPlaylistURIs(body []byte, rewriteURI func(string) (string, error)) ([]byte, error) {
	content := strings.TrimPrefix(string(body), "\uFEFF")
	playlist := strings.TrimSpace(content)
	header, _, _ := strings.Cut(playlist, "\n")
	if strings.TrimSpace(header) != "#EXTM3U" {
		return nil, errors.New("响应不是有效的 HLS 播放列表")
	}
	var out strings.Builder
	for _, chunk := range strings.SplitAfter(content, "\n") {
		if chunk == "" {
			continue
		}
		line, ending := splitHLSLineEnding(chunk)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if err := validateHLSURIAttributes(trimmed); err != nil {
				return nil, err
			}
			var rewriteErr error
			line = hlsURIAttribute.ReplaceAllStringFunc(line, func(attribute string) string {
				if rewriteErr != nil {
					return attribute
				}
				match := hlsURIAttribute.FindStringSubmatch(attribute)
				rewritten, uriErr := rewriteURI(match[1])
				if uriErr != nil {
					rewriteErr = uriErr
					return attribute
				}
				return `URI="` + rewritten + `"`
			})
			if rewriteErr != nil {
				return nil, rewriteErr
			}
		} else if trimmed != "" {
			leading := len(line) - len(strings.TrimLeft(line, " \t"))
			trailing := line[len(strings.TrimRight(line, " \t")):]
			rewritten, rewriteErr := rewriteURI(trimmed)
			if rewriteErr != nil {
				return nil, rewriteErr
			}
			line = line[:leading] + rewritten
			line += trailing
		}
		out.WriteString(line)
		out.WriteString(ending)
	}
	return []byte(out.String()), nil
}

// 浏览器会容忍未加引号的 URI，代理必须拒绝它，防止密钥绕过改写直连。
func validateHLSURIAttributes(line string) error {
	_, attributes, found := strings.Cut(line, ":")
	if !found {
		return nil
	}
	validate := func(attribute string) error {
		trim := func(value string) string {
			return strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' })
		}
		attribute = trim(attribute)
		name, _, found := strings.Cut(attribute, "=")
		if !found || !strings.EqualFold(trim(name), "URI") {
			return nil
		}
		if !strings.HasPrefix(attribute, `URI="`) || !strings.HasSuffix(attribute, `"`) || strings.Count(attribute, `"`) != 2 {
			return errors.New("HLS URI 必须使用规范的双引号属性")
		}
		return nil
	}
	start, quoted := 0, false
	for i := range len(attributes) {
		switch attributes[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				if err := validate(attributes[start:i]); err != nil {
					return err
				}
				start = i + 1
			}
		}
	}
	if quoted {
		return errors.New("HLS 属性引号不完整")
	}
	return validate(attributes[start:])
}

func splitHLSLineEnding(chunk string) (string, string) {
	if !strings.HasSuffix(chunk, "\n") {
		return chunk, ""
	}
	line := strings.TrimSuffix(chunk, "\n")
	if strings.HasSuffix(line, "\r") {
		return strings.TrimSuffix(line, "\r"), "\r\n"
	}
	return line, "\n"
}

func isHLSResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "mpegurl") {
		return true
	}
	return resp.Request != nil && resp.Request.URL != nil && strings.EqualFold(path.Ext(resp.Request.URL.Path), ".m3u8")
}

func (a *App) hlsProxyResource(c *gin.Context) {
	item, sessionID, resourceID := c.Param("id"), c.Param("session"), c.Param("resource")
	target, userID, ok := a.hlsProxyTarget(item, sessionID, resourceID)
	if !ok {
		c.Status(404)
		return
	}
	user, err := a.db.User(userID)
	if err != nil || user.Disabled {
		c.Status(404)
		return
	}
	owner := &App{appState: a.appState, db: a.db.ForUser(userID)}
	if !owner.canAccessItem(item) {
		c.Status(404)
		return
	}
	owner.proxyRemoteWithHLSSession(c, target, sessionID)
}

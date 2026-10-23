package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"emby-go/internal/scanner"
	"emby-go/internal/store"

	"github.com/gin-gonic/gin"
)

var sourceRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func libraryPlaybackURL(c *gin.Context, raw string, library store.Library) (string, error) {
	if !library.ResolveRedirects {
		return raw, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	current := raw
	for range 5 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", errors.New("无效的直链解析地址")
		}
		// Some download services only redirect GET. Do not consume the media body.
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("Accept-Encoding", "identity")
		if ua := c.GetHeader("User-Agent"); ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		resp, err := sourceRedirectClient.Do(req)
		if err != nil {
			return "", errors.New("获取上游直链失败或超时")
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			location, err := resp.Location()
			if err != nil || !scanner.ValidHTTP(location.String()) {
				return "", errors.New("上游返回了无效的直链重定向")
			}
			previous, _ := url.Parse(current)
			// AList's cross-origin Location is handed to the player. Never fetch
			// the cloud video merely to discover its URL or a further CDN redirect.
			if location.Scheme != previous.Scheme || location.Host != previous.Host {
				return location.String(), nil
			}
			current = location.String()
		default:
			if resp.StatusCode >= 200 && resp.StatusCode < 300 && current != raw {
				return current, nil
			}
			return "", errors.New("上游未返回可用的直链重定向，请确认已开启 302")
		}
	}
	return "", errors.New("上游直链重定向次数过多")
}

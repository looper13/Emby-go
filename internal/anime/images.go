package anime

import (
	"bytes"
	"context"
	"errors"
	_ "github.com/deepteams/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"
)

func (c *Client) Cover(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, "", errors.New("invalid_cover_url")
	}
	base, _ := url.Parse(c.MikanURL)
	allowed := u.Hostname() == "lain.bgm.tv" && (strings.HasPrefix(u.Path, "/pic/cover/") || strings.HasPrefix(u.Path, "/r/"))
	allowed = allowed || (base != nil && u.Host == base.Host && strings.HasPrefix(strings.ToLower(u.Path), "/images/bangumi/"))
	if !allowed {
		return nil, "", errors.New("cover_host_not_allowed")
	}
	data, err := c.requestBounded(ctx, "GET", u.String(), nil, 2<<20)
	if err != nil {
		return nil, "", err
	}
	if len(data) > 2<<20 {
		return nil, "", errors.New("cover_too_large")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 24_000_000 {
		return nil, "", errors.New("invalid_cover_image")
	}
	if format != "jpeg" && format != "png" && format != "webp" && format != "gif" {
		return nil, "", errors.New("unsupported_cover_image")
	}
	return data, "image/" + format, nil
}

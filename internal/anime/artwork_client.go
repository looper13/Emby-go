package anime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"emby-go/internal/anime/artwork"
)

// Archive contains identities, not image URLs. Enrich only the requested owner.
func (c *Client) BangumiImage(ctx context.Context, kind string, id int64, size string) ([]byte, string, error) {
	paths := map[string]string{"subject": "subjects", "character": "characters", "person": "persons"}
	path, ok := paths[kind]
	if !ok || id <= 0 {
		return nil, "", errors.New("invalid_image_owner")
	}
	if size != "large" && size != "common" && size != "medium" && size != "small" && size != "grid" {
		return nil, "", errors.New("invalid_image_size")
	}
	raw, err := c.request(ctx, "GET", fmt.Sprintf("%s/v0/%s/%d", c.BangumiURL, path, id), nil)
	if err != nil {
		if err.Error() == "source_http_404" {
			err = artwork.ErrMissing
		}
		return nil, "", err
	}
	var v struct {
		ID     int64             `json:"id"`
		Images map[string]string `json:"images"`
	}
	if err = json.Unmarshal(raw, &v); err != nil || v.ID != id {
		return nil, "", errors.New("invalid_image_response")
	}
	imageURL := v.Images[size]
	if imageURL == "" {
		for _, fallback := range []string{"large", "medium", "common", "small", "grid"} {
			if v.Images[fallback] != "" {
				imageURL = v.Images[fallback]
				break
			}
		}
	}
	if imageURL == "" {
		return nil, "", artwork.ErrMissing
	}
	data, err := c.Artwork(ctx, imageURL)
	return data, imageURL, err
}
func (c *Client) Artwork(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid_artwork_url")
	}
	allowed := u.Host == "lain.bgm.tv" && (strings.HasPrefix(u.Path, "/pic/cover/") || strings.HasPrefix(u.Path, "/pic/crt/") || strings.HasPrefix(u.Path, "/pic/prsn/") || strings.HasPrefix(u.Path, "/r/"))
	allowed = allowed || u.Host == "image.tmdb.org" && strings.HasPrefix(u.Path, "/t/p/")
	if !allowed {
		return nil, errors.New("artwork_host_not_allowed")
	}
	return c.requestBounded(ctx, "GET", u.String(), nil, 4<<20)
}

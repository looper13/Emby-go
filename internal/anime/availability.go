package anime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const AvailabilityTTL = 10 * time.Minute

func SourceVersion(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// CheckMedia deliberately allows private network media already registered by
// the administrator. Metadata/image fetching uses a separate public-only policy.
// No signed URL or response body is retained in errors or persistent metadata.
func CheckMedia(ctx context.Context, source string) error {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("invalid_source")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("redirect_limit")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("invalid_redirect")
		}
		return nil
	}}
	// A bounded Range GET also works when HEAD is unsupported and verifies the
	// body so HTML login pages with a video Content-Type cannot pass a HEAD check.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return errors.New("invalid_source")
	}
	req.Header.Set("Range", "bytes=0-65535")
	req.Header.Set("User-Agent", "Emby-go/AnimeMediaCheck")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("source_request_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return errors.New("source_http_error")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return errors.New("source_read_failed")
	}
	if len(body) == 0 {
		return errors.New("empty_source")
	}
	trim := bytes.TrimSpace(body)
	lower := bytes.ToLower(trim)
	if bytes.HasPrefix(lower, []byte("<!doctype html")) || bytes.HasPrefix(lower, []byte("<html")) || bytes.HasPrefix(lower, []byte("<?xml")) || bytes.HasPrefix(lower, []byte("<form")) {
		return errors.New("source_is_not_media")
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "text/html" || strings.Contains(ct, "bittorrent") || ct == "application/json" {
		return errors.New("source_is_not_media")
	}
	// Accept recognizable containers/playlists. Content-Type alone is not proof.
	if len(body) >= 12 && string(body[4:8]) == "ftyp" {
		return nil
	}
	if len(body) >= 4 && bytes.Equal(body[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return nil
	}
	if len(body) >= 377 && body[0] == 0x47 && body[188] == 0x47 && body[376] == 0x47 {
		return nil
	}
	if bytes.HasPrefix(trim, []byte("#EXTM3U")) && (bytes.Contains(trim, []byte("#EXTINF:")) || bytes.Contains(trim, []byte("#EXT-X-STREAM-INF:"))) {
		return nil
	}
	return errors.New("unrecognized_media")
}

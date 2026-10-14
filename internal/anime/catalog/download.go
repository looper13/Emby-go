// Package catalog owns replaceable Bangumi Archive data, never playback state.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/anime"
)

const ManifestURL = "https://raw.githubusercontent.com/bangumi/Archive/master/aux/latest.json"
const MaxDownloadBytes int64 = 1 << 30

type Manifest struct {
	Name      string `json:"name"`
	URL       string `json:"browser_download_url"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
	CreatedAt string `json:"created_at"`
}

func (m Manifest) Validate() error {
	u, err := url.Parse(m.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/bangumi/Archive/releases/download/archive/") {
		return errors.New("invalid_archive_download_url")
	}
	if filepath.Base(m.Name) != m.Name || !strings.HasPrefix(m.Name, "dump-") || !strings.HasSuffix(m.Name, ".zip") || strings.ContainsAny(m.Name, `/\\`) || !strings.HasSuffix(u.Path, "/"+m.Name) || m.Size <= 0 || m.Size > MaxDownloadBytes {
		return errors.New("invalid_archive_manifest")
	}
	hash, err := hex.DecodeString(strings.TrimPrefix(m.Digest, "sha256:"))
	if err != nil || len(hash) != 32 || !strings.HasPrefix(m.Digest, "sha256:") {
		return errors.New("archive_sha256_required")
	}
	if _, err = time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return errors.New("invalid_archive_date")
	}
	return nil
}

type Progress struct {
	Phase      string `json:"phase"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"total_bytes"`
	Rows       int64  `json:"rows"`
}

// The data client permits only GitHub's release redirects, retaining the shared
// DNS/IP checks. Metadata credentials are never forwarded to another service.
func NewHTTPClient() *http.Client {
	c := anime.PublicClient()
	c.Timeout = 30 * time.Minute
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || r.URL.Scheme != "https" || r.URL.User != nil {
			return errors.New("invalid_archive_redirect")
		}
		switch r.URL.Host {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "raw.githubusercontent.com":
			return nil
		default:
			return errors.New("invalid_archive_redirect_host")
		}
	}
	return c
}

func Latest(ctx context.Context, client *http.Client) (Manifest, error) {
	var m Manifest
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, ManifestURL, nil)
	if err != nil {
		return m, err
	}
	r.Header.Set("User-Agent", "Emby-go/AnimeArchive")
	resp, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return m, ctx.Err()
		}
		return m, errors.New("archive_manifest_request_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("archive_manifest_http_%d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if ctx.Err() != nil {
		return m, ctx.Err()
	}
	if err != nil || len(b) > 64<<10 {
		return m, errors.New("archive_manifest_size")
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, errors.New("archive_manifest_invalid_json")
	}
	return m, m.Validate()
}

type progressWriter struct {
	total  int64
	notify func(Progress)
	last   time.Time
	size   int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	if w.notify != nil && time.Since(w.last) >= 250*time.Millisecond {
		w.notify(Progress{Phase: "download", Bytes: w.total, TotalBytes: w.size})
		w.last = time.Now()
	}
	return len(p), nil
}

// Download returns a verified temporary ZIP owned by the caller. Failed partial
// downloads are removed; an existing active catalog is never touched.
func Download(ctx context.Context, client *http.Client, dir string, m Manifest, notify func(Progress)) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	if notify != nil {
		notify(Progress{Phase: "download", TotalBytes: m.Size})
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "download-*.zip")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = os.Remove(f.Name())
		}
	}()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return "", err
	}
	r.Header.Set("User-Agent", "Emby-go/AnimeArchive")
	resp, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("archive_download_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("archive_download_http_%d", resp.StatusCode)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != m.Size {
		return "", errors.New("archive_size_mismatch")
	}
	h := sha256.New()
	p := &progressWriter{notify: notify, size: m.Size}
	n, err := io.Copy(io.MultiWriter(f, h, p), io.LimitReader(resp.Body, m.Size+1))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", errors.New("archive_download_incomplete")
	}
	if n != m.Size || "sha256:"+hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.Digest) {
		return "", errors.New("archive_digest_mismatch")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if notify != nil {
		notify(Progress{Phase: "download", Bytes: n, TotalBytes: m.Size})
	}
	ok = true
	return f.Name(), nil
}

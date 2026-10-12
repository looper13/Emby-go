package gfriends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emby-go/internal/actorassets"
)

type Config struct {
	IndexURL     string `json:"index_url"`
	BaseURL      string `json:"base_url"`
	RefreshHours int    `json:"refresh_hours"`
}

func (c Config) Defaults() Config {
	if c.IndexURL == "" {
		c.IndexURL = DefaultIndexURL
	}
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.RefreshHours <= 0 {
		c.RefreshHours = 24
	}
	return c
}
func (c Config) Validate() error {
	c = c.Defaults()
	if err := ValidateURL(c.IndexURL); err != nil {
		return err
	}
	return ValidateURL(c.BaseURL)
}

type Status struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Groups    int       `json:"groups"`
	Entries   int       `json:"entries"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
}
type metadata struct {
	ETag     string
	Modified string
	Status   Status
}
type entry struct {
	mu      sync.Mutex
	flight  chan struct{}
	index   *Index
	meta    metadata
	lastErr error
}

var entries sync.Map

type Client struct {
	cfg   Config
	dir   string
	http  *http.Client
	state *entry
}

func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return errors.New("unsafe redirect")
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return nil
	}}
}
func New(root string, cfg Config, timeout time.Duration) *Client {
	cfg = cfg.Defaults()
	key := actorassets.Hash([]byte(cfg.IndexURL + "\n" + strings.TrimRight(cfg.BaseURL, "/")))
	dir := filepath.Join(root, "cache", "gfriends", key)
	state, _ := entries.LoadOrStore(dir, &entry{})
	return &Client{cfg: cfg, dir: dir, http: HTTPClient(timeout), state: state.(*entry)}
}
func (c *Client) Status() Status {
	e := c.state
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.meta.Status
}
func (c *Client) Snapshot(ctx context.Context, force bool) (*Index, Status, error) {
	if err := c.cfg.Validate(); err != nil {
		return nil, Status{}, err
	}
	e := c.state
	e.mu.Lock()
	if ch := e.flight; ch != nil {
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, Status{}, ctx.Err()
		case <-ch:
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.index, e.meta.Status, e.lastErr
	}
	if !force && e.index != nil && time.Since(e.meta.Status.UpdatedAt) < time.Duration(c.cfg.RefreshHours)*time.Hour {
		i, s := e.index, e.meta.Status
		e.mu.Unlock()
		return i, s, nil
	}
	e.flight = make(chan struct{})
	old, meta := e.index, e.meta
	e.mu.Unlock()
	// Disk and network work deliberately happen outside the publication lock.
	if old == nil {
		if b, err := actorassets.ReadBounded(c.dir, "Filetree.json", MaxIndexBytes); err == nil {
			old, _ = Parse(b)
			if h, err := actorassets.ReadBounded(c.dir, "http.json", 16384); err == nil {
				_ = json.Unmarshal(h, &meta)
			}
			if old != nil && meta.Status.Version != old.Version {
				meta = metadata{}
			}
		}
	}
	var next *Index
	var err error
	if !force && old != nil && !meta.Status.UpdatedAt.IsZero() && time.Since(meta.Status.UpdatedAt) < time.Duration(c.cfg.RefreshHours)*time.Hour {
		next = old
	} else {
		next, meta, err = c.refresh(ctx, old, meta)
	}
	if err != nil && old != nil && ctx.Err() == nil {
		next = old
		meta.Status.Stale = true
		meta.Status.Error = err.Error()
		err = nil
	}
	e.mu.Lock()
	if next != nil {
		e.index = next
		e.meta = meta
	}
	e.lastErr = err
	ch := e.flight
	e.flight = nil
	close(ch)
	status := e.meta.Status
	e.mu.Unlock()
	return next, status, err
}
func (c *Client) refresh(ctx context.Context, old *Index, meta metadata) (*Index, metadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.IndexURL, nil)
	if err != nil {
		return nil, meta, err
	}
	if old != nil {
		req.Header.Set("If-None-Match", meta.ETag)
		req.Header.Set("If-Modified-Since", meta.Modified)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, meta, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && old != nil {
		meta.Status.UpdatedAt = time.Now().UTC()
		meta.Status.Stale = false
		meta.Status.Error = ""
		err = actorassets.WriteJSON(c.dir, "http.json", meta)
		return old, meta, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, meta, fmt.Errorf("GFriends index HTTP %d", resp.StatusCode)
	}
	b, err := ReadLimit(resp.Body, MaxIndexBytes)
	if err != nil {
		return nil, meta, err
	}
	i, err := Parse(b)
	if err != nil {
		return nil, meta, err
	}
	next := metadata{ETag: resp.Header.Get("ETag"), Modified: resp.Header.Get("Last-Modified"), Status: Status{Version: i.Version, UpdatedAt: time.Now().UTC(), Groups: i.Groups, Entries: i.Entries}}
	if err := actorassets.AtomicWrite(c.dir, "Filetree.json", b); err != nil {
		return nil, meta, err
	}
	if err := actorassets.WriteJSON(c.dir, "http.json", next); err != nil {
		return nil, meta, err
	}
	return i, next, nil
}
func ReadLimit(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("response exceeds byte limit")
	}
	return b, err
}

// Download is deliberately unauthenticated, including when called for an existing NFO URL.
func (c *Client) Download(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid portrait URL")
	}
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			var requestErr *url.Error
			if errors.As(err, &requestErr) {
				return nil, fmt.Errorf("portrait request failed: %w", requestErr.Err)
			}
			return nil, err
		}
		if resp.StatusCode == http.StatusOK {
			b, err := ReadLimit(resp.Body, actorassets.MaxImageBytes)
			resp.Body.Close()
			return b, err
		}
		resp.Body.Close()
		last = fmt.Errorf("portrait HTTP %d", resp.StatusCode)
		if resp.StatusCode < 500 {
			return nil, last
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil, last
}

// DiskStatus inspects only small persisted metadata; status GET never refreshes the network.
func (c *Client) DiskStatus() Status {
	if s := c.Status(); s.Version != "" {
		return s
	}
	b, err := actorassets.ReadBounded(c.dir, "http.json", 16384)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}
	}
	if err != nil {
		return Status{Error: err.Error()}
	}
	var m metadata
	if json.Unmarshal(b, &m) != nil {
		return Status{Error: "invalid index metadata"}
	}
	return m.Status
}

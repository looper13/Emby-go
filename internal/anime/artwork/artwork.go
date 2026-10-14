// Package artwork stores replaceable image metadata separately from media state.
package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	_ "github.com/deepteams/webp"
	_ "modernc.org/sqlite"
)

var ErrMissing = errors.New("artwork_missing")
var ErrBusy = errors.New("artwork_busy")
var variantPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

type Key struct {
	Kind    string
	ID      int64
	Variant string
}

func (k Key) Valid() bool {
	return (k.Kind == "subject" || k.Kind == "character" || k.Kind == "person" || k.Kind == "episode") && k.ID > 0 && variantPattern.MatchString(k.Variant)
}
func (k Key) String() string { return fmt.Sprintf("%s:%d:%s", k.Kind, k.ID, k.Variant) }

type Asset struct {
	State     string `json:"state"`
	Source    string `json:"source"`
	Hash      string `json:"hash"`
	MIME      string `json:"mime"`
	Bytes     int64  `json:"bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	UpdatedAt int64  `json:"updated_at"`
	RetryAt   int64  `json:"retry_at"`
	Error     string `json:"error"`
	Path      string `json:"-"`
}
type Fetch func(context.Context) (data []byte, source string, err error)
type flight struct {
	done  chan struct{}
	asset Asset
	err   error
}
type Store struct {
	db      *sql.DB
	dir     string
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	flights map[string]*flight
	slots   chan struct{}
	wg      sync.WaitGroup
}

func Open(parent context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(filepath.Join(dir, "images.db"))+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS images(
		key TEXT PRIMARY KEY,state TEXT NOT NULL,source TEXT NOT NULL,hash TEXT NOT NULL,mime TEXT NOT NULL,
		bytes INTEGER NOT NULL,width INTEGER NOT NULL,height INTEGER NOT NULL,updated_at INTEGER NOT NULL,retry_at INTEGER NOT NULL,error TEXT NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	return &Store{db: db, dir: dir, ctx: ctx, cancel: cancel, flights: map[string]*flight{}, slots: make(chan struct{}, 4)}, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	return s.db.Close()
}
func (s *Store) Lookup(ctx context.Context, k Key) (Asset, error) {
	var a Asset
	if !k.Valid() {
		return a, errors.New("invalid_artwork_key")
	}
	err := s.db.QueryRowContext(ctx, `SELECT state,source,hash,mime,bytes,width,height,updated_at,retry_at,error FROM images WHERE key=?`, k.String()).Scan(&a.State, &a.Source, &a.Hash, &a.MIME, &a.Bytes, &a.Width, &a.Height, &a.UpdatedAt, &a.RetryAt, &a.Error)
	if err != nil {
		return a, err
	}
	if len(a.Hash) == 64 {
		if _, err := hex.DecodeString(a.Hash); err == nil {
			a.Path = filepath.Join(s.dir, "files", k.Kind, fmt.Sprint(k.ID), a.Hash)
			if st, err := os.Stat(a.Path); err != nil || !st.Mode().IsRegular() || st.Size() != a.Bytes {
				a.Path = ""
			}
		}
	}
	return a, nil
}

// Fetches belong to the store lifetime, not to one reader. A leaving page cannot
// cancel another reader. Both the queue and active network work are bounded.
func (s *Store) Ensure(ctx context.Context, k Key, retry bool, fetch Fetch) (Asset, error) {
	if !k.Valid() {
		return Asset{}, errors.New("invalid_artwork_key")
	}
	old, err := s.Lookup(ctx, k)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Asset{}, err
	}
	now := time.Now().Unix()
	if old.Path != "" && old.UpdatedAt > now-int64((7*24*time.Hour)/time.Second) {
		return old, nil
	}
	if !retry && old.RetryAt > now {
		if old.Path != "" {
			return old, nil
		}
		if old.State == "missing" {
			return old, ErrMissing
		}
		return old, errors.New("artwork_retry_later")
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		return old, context.Canceled
	}
	f := s.flights[k.String()]
	if f == nil {
		if len(s.flights) >= 128 {
			s.mu.Unlock()
			return old, ErrBusy
		}
		f = &flight{done: make(chan struct{})}
		s.flights[k.String()] = f
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			request, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
			defer cancel()
			select {
			case s.slots <- struct{}{}:
				f.asset, f.err = s.fetch(request, k, old, fetch)
				<-s.slots
			case <-request.Done():
				f.asset, f.err = old, request.Err()
			}
			s.mu.Lock()
			delete(s.flights, k.String())
			close(f.done)
			s.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return old, ctx.Err()
	case <-f.done:
		return f.asset, f.err
	}
}

func (s *Store) fetch(ctx context.Context, k Key, old Asset, fetch Fetch) (Asset, error) {
	data, source, err := fetch(ctx)
	a := old
	if err == nil {
		a, err = s.persist(k, data, source)
	}
	if ctx.Err() != nil {
		return old, ctx.Err()
	}
	if err != nil {
		a = old
		a.Error = "artwork_fetch_failed"
		a.State = "failed"
		a.RetryAt = time.Now().Add(5 * time.Minute).Unix()
		if errors.Is(err, ErrMissing) {
			a.State = "missing"
			a.Error = ""
			a.RetryAt = time.Now().Add(24 * time.Hour).Unix()
		}
	}
	_, saveErr := s.db.ExecContext(ctx, `INSERT INTO images VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET state=excluded.state,source=excluded.source,hash=excluded.hash,mime=excluded.mime,bytes=excluded.bytes,width=excluded.width,height=excluded.height,updated_at=excluded.updated_at,retry_at=excluded.retry_at,error=excluded.error`, k.String(), a.State, a.Source, a.Hash, a.MIME, a.Bytes, a.Width, a.Height, a.UpdatedAt, a.RetryAt, a.Error)
	if saveErr != nil {
		return old, saveErr
	}
	if a.Path != "" {
		return a, nil
	}
	return a, err
}
func (s *Store) persist(k Key, data []byte, source string) (Asset, error) {
	if len(data) == 0 || len(data) > 4<<20 {
		return Asset{}, errors.New("artwork_size_limit")
	}
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 24_000_000 {
		return Asset{}, errors.New("invalid_artwork")
	}
	if format != "jpeg" && format != "png" && format != "webp" && format != "gif" {
		return Asset{}, errors.New("invalid_artwork_format")
	}
	h := fmt.Sprintf("%x", sha256.Sum256(data))
	dir := filepath.Join(s.dir, "files", k.Kind, fmt.Sprint(k.ID))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Asset{}, err
	}
	p := filepath.Join(dir, h)
	if st, e := os.Stat(p); e != nil || st.Size() != int64(len(data)) {
		f, e := os.CreateTemp(dir, "image-*")
		if e != nil {
			return Asset{}, e
		}
		defer os.Remove(f.Name())
		_, e = f.Write(data)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			e = os.Rename(f.Name(), p)
		}
		if e != nil {
			return Asset{}, e
		}
	}
	return Asset{State: "ready", Source: source, Hash: h, MIME: "image/" + format, Bytes: int64(len(data)), Width: c.Width, Height: c.Height, UpdatedAt: time.Now().Unix(), Path: p}, nil
}

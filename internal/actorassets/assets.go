// Package actorassets owns portable actor assets. Names retain their exact local
// identity; matching normalization belongs to providers, not this package.
package actorassets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"emby-go/internal/imageutil"
	"github.com/google/uuid"
)

const MaxImageBytes = 32 << 20
const MaxPixels = 24_000_000
const maxManifestBytes = 1 << 20

type Avatar struct {
	File           string     `json:"file"`
	SHA256         string     `json:"sha256"`
	Width          int        `json:"width"`
	Height         int        `json:"height"`
	Origin         string     `json:"origin"`
	SourceURL      string     `json:"source_url,omitempty"`
	Group          string     `json:"group,omitempty"`
	SourceFile     string     `json:"source_file,omitempty"`
	SourceVersion  string     `json:"source_version,omitempty"`
	IndexVersion   string     `json:"index_version,omitempty"`
	IndexTimestamp string     `json:"index_timestamp,omitempty"`
	DownloadedAt   *time.Time `json:"downloaded_at,omitempty"`
}

type Alias struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Early manifests stored bare strings. Retain that evidence without attributing
// it to a provider that the old record did not identify.
func (a *Alias) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		*a = Alias{Name: name, Source: "legacy_manifest"}
		return nil
	}
	type record Alias
	var value record
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*a = Alias(value)
	return nil
}

type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	Name          string          `json:"name"`
	Aliases       []Alias         `json:"aliases,omitempty"`
	NFOConflicts  []ThumbConflict `json:"nfo_conflicts,omitempty"`
	Avatar        *Avatar         `json:"avatar,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type ThumbConflict struct {
	SourceKey string `json:"source_key"`
	URL       string `json:"url,omitempty"`
}

// Pending contains identities, never trusted filesystem paths or database IDs.
type Pending struct {
	OperationID      string    `json:"operation_id"`
	SchemaVersion    int       `json:"schema_version"`
	Manifest         Manifest  `json:"manifest"`
	PreviousHash     string    `json:"previous_hash,omitempty"`
	Stage            string    `json:"stage"`
	RemainingSources []string  `json:"remaining_sources,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Store struct{ Dir string }

var writers sync.Map

// Acquire serializes tasks in this process, including direct scraper callers.
// A root remains exclusive to one deployed instance; cross-process locking is not supported.
func (s *Store) Acquire(ctx context.Context) (func(), error) {
	key, err := filepath.Abs(s.Dir)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	v, _ := writers.LoadOrStore(key, make(chan struct{}, 1))
	sem := v.(chan struct{})
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func New(dir string) *Store        { return &Store{Dir: dir} }
func Hash(data []byte) string      { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func Key(name string) string       { return Hash([]byte(strings.TrimSpace(name))) }
func PersonDir(name string) string { k := Key(name); return filepath.Join("people", k[:2], k) }
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	// Signed/private URLs cannot be made public by removing their query. Keep the
	// portrait but omit a repeatable source URL rather than inventing a broken one.
	for key := range u.Query() {
		if key != "t" {
			return ""
		}
	}
	q := url.Values{}
	if v := u.Query().Get("t"); v != "" {
		q.Set("t", v)
	}
	u.RawQuery, u.Fragment = q.Encode(), ""
	return u.String()
}
func validate(m Manifest, name string) error {
	if m.SchemaVersion != 1 || m.Name == "" || m.Name != strings.TrimSpace(name) {
		return errors.New("invalid actor manifest identity/version")
	}
	for _, alias := range m.Aliases {
		if strings.TrimSpace(alias.Name) == "" || strings.TrimSpace(alias.Source) == "" {
			return errors.New("actor alias requires a name and source")
		}
	}
	if a := m.Avatar; a != nil {
		digest, err := hex.DecodeString(a.SHA256)
		if err != nil || len(digest) != 32 || strings.ToLower(a.SHA256) != a.SHA256 || a.File != "images/"+a.SHA256+".webp" {
			return errors.New("invalid actor image path/hash")
		}
		if a.Width <= 0 || a.Height <= 0 || int64(a.Width)*int64(a.Height) > MaxPixels {
			return errors.New("invalid actor image dimensions")
		}
		if a.Origin != "gfriends" && a.Origin != "metatube" && a.Origin != "legacy_local" && a.Origin != "nfo" {
			return errors.New("invalid actor image origin")
		}
		if a.SourceURL != "" && SafeURL(a.SourceURL) != a.SourceURL {
			return errors.New("unsafe actor source URL")
		}
	}
	return nil
}

// ReadBounded uses Root to reject paths/symlinks that leave the configured root.
func ReadBounded(dir, rel string, limit int64) ([]byte, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readBounded(r, rel, limit)
}

func readBounded(r *os.Root, rel string, limit int64) ([]byte, error) {
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("file exceeds size limit")
	}
	return b, err
}

// AtomicWrite stages beside the destination so publication stays on one filesystem.
func AtomicWrite(dir, rel string, data []byte) error {
	if !filepath.IsLocal(rel) {
		return errors.New("unsafe relative path")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return err
	}
	tmp := rel + "." + uuid.NewString() + ".tmp"
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return r.Rename(tmp, rel)
}
func WriteJSON(dir, rel string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(dir, rel, append(b, '\n'))
}
func (s *Store) init() error {
	b, err := ReadBounded(s.Dir, "layout.json", 4096)
	if errors.Is(err, os.ErrNotExist) {
		return WriteJSON(s.Dir, "layout.json", map[string]int{"schema_version": 1})
	}
	if err != nil {
		return err
	}
	var layout struct {
		Version int `json:"schema_version"`
	}
	if json.Unmarshal(b, &layout) != nil || layout.Version != 1 {
		return errors.New("unsupported actor layout")
	}
	return nil
}
func (s *Store) Load(name string) (Manifest, error) {
	r, err := os.OpenRoot(s.Dir)
	if err != nil {
		return Manifest{}, err
	}
	defer r.Close()
	if err := validateLayout(r); err != nil {
		return Manifest{}, err
	}
	return loadManifest(r, name)
}

func validateLayout(r *os.Root) error {
	if b, err := readBounded(r, "layout.json", 4096); err == nil {
		var layout struct {
			Version int `json:"schema_version"`
		}
		if json.Unmarshal(b, &layout) != nil || layout.Version != 1 {
			return errors.New("unsupported actor layout")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func loadManifest(r *os.Root, name string) (Manifest, error) {
	var m Manifest
	b, err := readBounded(r, filepath.Join(PersonDir(name), "actor.json"), maxManifestBytes)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, validate(m, name)
}
func (s *Store) Resolve(name string) (string, string, error) {
	r, err := os.OpenRoot(s.Dir)
	if err != nil {
		return "", "", err
	}
	defer r.Close()
	if err := validateLayout(r); err != nil {
		return "", "", err
	}
	return s.resolve(r, name)
}

func (s *Store) resolve(r *os.Root, name string) (string, string, error) {
	m, err := loadManifest(r, name)
	if err != nil {
		return "", "", err
	}
	if m.Avatar == nil {
		return "", "", os.ErrNotExist
	}
	rel := filepath.Join(PersonDir(name), filepath.FromSlash(m.Avatar.File))
	b, err := readBounded(r, rel, MaxImageBytes)
	if err != nil {
		return "", "", err
	}
	if Hash(b) != m.Avatar.SHA256 {
		return "", "", errors.New("actor image hash mismatch")
	}
	if err := validateImage(b, m.Avatar); err != nil {
		return "", "", err
	}
	return filepath.Join(s.Dir, rel), m.Avatar.SHA256[:16], nil
}

func validateImage(data []byte, a *Avatar) error {
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if format != "webp" || c.Width != a.Width || c.Height != a.Height {
		return errors.New("actor image format/dimensions mismatch")
	}
	return nil
}

// Prepare validates before decode to bound memory, then hashes the final WebP.
func Prepare(name string, data []byte, source Avatar, aliases []Alias) (Manifest, []byte, error) {
	var m Manifest
	if strings.TrimSpace(name) == "" || len(data) > MaxImageBytes {
		return m, nil, errors.New("invalid actor/image size")
	}
	c, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return m, nil, err
	}
	if c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > MaxPixels {
		return m, nil, errors.New("actor image pixel limit exceeded")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return m, nil, err
	}
	b, err := imageutil.EncodeWebPBuffer(img, 82)
	if err != nil {
		return m, nil, err
	}
	source.SHA256, source.Width, source.Height = Hash(b), c.Width, c.Height
	source.File = "images/" + source.SHA256 + ".webp"
	source.SourceURL = SafeURL(source.SourceURL)
	now := time.Now().UTC()
	if source.Origin != "legacy_local" {
		source.DownloadedAt = &now
	}
	if source.Origin == "gfriends" && source.SourceURL != "" {
		u, _ := url.Parse(source.SourceURL)
		source.SourceVersion = u.Query().Get("t")
	}
	m = Manifest{SchemaVersion: 1, Name: strings.TrimSpace(name), Aliases: append([]Alias(nil), aliases...), Avatar: &source, CreatedAt: now, UpdatedAt: now}
	return m, b, validate(m, name)
}

// Stage makes the immutable bytes durable before journalling their selection.
func (s *Store) Stage(m Manifest, data []byte, previous string) (Pending, error) {
	p := Pending{OperationID: uuid.NewString(), SchemaVersion: 1, Manifest: m, PreviousHash: previous, Stage: "prepared"}
	if err := validate(m, m.Name); err != nil {
		return p, err
	}
	if m.Avatar == nil || Hash(data) != m.Avatar.SHA256 {
		return p, errors.New("image does not match manifest")
	}
	if err := s.init(); err != nil {
		return p, err
	}
	rel := filepath.Join(PersonDir(m.Name), filepath.FromSlash(m.Avatar.File))
	if err := AtomicWrite(s.Dir, rel, data); err != nil {
		return p, err
	}
	return p, s.SavePending(&p)
}
func (s *Store) Publish(p *Pending) error {
	m := p.Manifest
	if err := validate(m, m.Name); err != nil {
		return err
	}
	if m.Avatar == nil {
		return errors.New("pending avatar missing")
	}
	b, err := ReadBounded(s.Dir, filepath.Join(PersonDir(m.Name), filepath.FromSlash(m.Avatar.File)), MaxImageBytes)
	if err != nil {
		return err
	}
	if Hash(b) != m.Avatar.SHA256 {
		return errors.New("prepared image hash mismatch")
	}
	if err := validateImage(b, m.Avatar); err != nil {
		return err
	}
	current, err := s.Load(m.Name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if current.Avatar != nil && current.Avatar.SHA256 != p.PreviousHash && current.Avatar.SHA256 != m.Avatar.SHA256 {
		return errors.New("actor selection changed while preparing")
	}
	if err := WriteJSON(s.Dir, filepath.Join(PersonDir(m.Name), "actor.json"), m); err != nil {
		return err
	}
	p.Stage = "asset_published"
	return s.SavePending(p)
}
func (s *Store) SavePending(p *Pending) error {
	p.UpdatedAt = time.Now().UTC()
	return WriteJSON(s.Dir, filepath.Join(PersonDir(p.Manifest.Name), "pending.json"), p)
}

func (s *Store) RecordConflicts(p *Pending, conflicts []ThumbConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	for _, c := range conflicts {
		if c.URL != "" && SafeURL(c.URL) != c.URL {
			return errors.New("unsafe NFO conflict URL")
		}
	}
	p.Manifest.NFOConflicts = conflicts
	p.Manifest.UpdatedAt = time.Now().UTC()
	if err := validate(p.Manifest, p.Manifest.Name); err != nil {
		return err
	}
	if err := WriteJSON(s.Dir, filepath.Join(PersonDir(p.Manifest.Name), "actor.json"), p.Manifest); err != nil {
		return err
	}
	return s.SavePending(p)
}
func (s *Store) Pending(name string) (*Pending, error) {
	b, err := ReadBounded(s.Dir, filepath.Join(PersonDir(name), "pending.json"), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	var p Pending
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.SchemaVersion != 1 || p.Manifest.Avatar == nil {
		return nil, errors.New("unsupported actor journal")
	}
	if err := validate(p.Manifest, name); err != nil {
		return nil, err
	}
	switch p.Stage {
	case "prepared", "asset_published", "pending_nfo", "pending_rescan", "complete":
	default:
		return nil, fmt.Errorf("unknown actor stage %q", p.Stage)
	}
	return &p, nil
}
func (s *Store) Complete(name string) error {
	r, err := os.OpenRoot(s.Dir)
	if err != nil {
		return err
	}
	defer r.Close()
	err = r.Remove(filepath.Join(PersonDir(name), "pending.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

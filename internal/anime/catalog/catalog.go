package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"emby-go/internal/anime"
	_ "modernc.org/sqlite"
)

var ErrNotReady = errors.New("anime_archive_not_imported")
var ErrBusy = errors.New("anime_archive_busy")

type Snapshot struct {
	Manifest   Manifest `json:"manifest"`
	File       string   `json:"file"`
	ImportedAt string   `json:"imported_at"`
	Subjects   int      `json:"subjects"`
	Episodes   int      `json:"episodes"`
	Characters int      `json:"characters"`
	Persons    int      `json:"persons"`
}

type Record struct {
	anime.Subject
	NSFW    bool   `json:"nsfw"`
	Year    int    `json:"year"`
	Season  int    `json:"season"`
	Infobox string `json:"infobox,omitempty"`
}

type Person struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Role    int    `json:"role"`
	Order   int    `json:"order"`
}

type Filter struct {
	Search                      string
	Year, Season, Limit, Offset int
}

type Catalog struct {
	dir      string
	mu       sync.RWMutex
	importMu sync.Mutex
	db       *sql.DB
	snapshot Snapshot
}

func Open(dir string) (*Catalog, error) {
	c := &Catalog{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "current.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var snap Snapshot
	if err = json.Unmarshal(b, &snap); err != nil {
		return c, err
	}
	if filepath.Base(snap.File) != snap.File || !strings.HasPrefix(snap.File, "catalog-") || !strings.HasSuffix(snap.File, ".db") || strings.ContainsAny(snap.File, `/\\`) {
		return c, errors.New("invalid_archive_pointer")
	}
	if err = snap.Manifest.Validate(); err != nil {
		return c, err
	}
	db, err := openSnapshot(filepath.Join(dir, snap.File))
	if err != nil {
		return c, err
	}
	c.db, c.snapshot = db, snap
	return c, nil
}

func openSnapshot(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(path)+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func (c *Catalog) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
func (c *Catalog) Snapshot() Snapshot { c.mu.RLock(); defer c.mu.RUnlock(); return c.snapshot }

func (c *Catalog) Search(ctx context.Context, f Filter) ([]Record, int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return []Record{}, 0, ErrNotReady
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where, args := " WHERE 1=1", []any{}
	if f.Year > 0 {
		where += " AND year=?"
		args = append(args, f.Year)
	}
	if f.Season > 0 {
		where += " AND season=?"
		args = append(args, f.Season)
	}
	if term := strings.TrimSpace(f.Search); term != "" {
		where += ` AND (name LIKE ? ESCAPE '\' OR original_name LIKE ? ESCAPE '\')`
		term = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(term) + "%"
		args = append(args, term, term)
	}
	var total int
	if err := c.db.QueryRowContext(ctx, "SELECT count(*) FROM subjects"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := c.db.QueryContext(ctx, "SELECT metadata FROM subjects"+where+" ORDER BY date DESC,id DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var raw string
		var r Record
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (c *Catalog) Detail(ctx context.Context, id int64) (Record, []anime.Episode, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.detail(ctx, id)
}

func (c *Catalog) ReadSubject(ctx context.Context, id int64) (Record, []anime.Episode, Snapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, eps, err := c.detail(ctx, id)
	return r, eps, c.snapshot, err
}

func (c *Catalog) detail(ctx context.Context, id int64) (Record, []anime.Episode, error) {
	var r Record
	if c.db == nil {
		return r, nil, ErrNotReady
	}
	var raw string
	if err := c.db.QueryRowContext(ctx, "SELECT metadata FROM subjects WHERE id=?", id).Scan(&raw); err != nil {
		return r, nil, err
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return r, nil, err
	}
	rows, err := c.db.QueryContext(ctx, "SELECT metadata FROM episodes WHERE subject_id=? ORDER BY sort,id", id)
	if err != nil {
		return r, nil, err
	}
	defer rows.Close()
	eps := []anime.Episode{}
	for rows.Next() {
		var e anime.Episode
		if err = rows.Scan(&raw); err != nil {
			return r, nil, err
		}
		if err = json.Unmarshal([]byte(raw), &e); err != nil {
			return r, nil, err
		}
		eps = append(eps, e)
	}
	r.Episodes = len(eps)
	return r, eps, rows.Err()
}

func (c *Catalog) People(ctx context.Context, id int64) ([]Person, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return nil, ErrNotReady
	}
	rows, err := c.db.QueryContext(ctx, `SELECT p.id,p.kind,p.name,p.summary,r.role,r.ordinal FROM people p JOIN credits r ON r.person_id=p.id AND r.kind=p.kind WHERE r.subject_id=? ORDER BY r.kind,r.role,r.ordinal,p.id LIMIT 500`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err = rows.Scan(&p.ID, &p.Kind, &p.Name, &p.Summary, &p.Role, &p.Order); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (c *Catalog) PersonExists(ctx context.Context, kind string, id int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return false
	}
	var n int
	return c.db.QueryRowContext(ctx, "SELECT 1 FROM people WHERE kind=? AND id=?", kind, id).Scan(&n) == nil
}

func (c *Catalog) SubjectExists(ctx context.Context, id int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return false
	}
	var n int
	return c.db.QueryRowContext(ctx, "SELECT 1 FROM subjects WHERE id=?", id).Scan(&n) == nil
}

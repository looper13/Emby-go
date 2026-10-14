package catalog

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emby-go/internal/anime"
)

const schema = `
CREATE TABLE subjects(id INTEGER PRIMARY KEY,name TEXT NOT NULL,original_name TEXT NOT NULL,date TEXT NOT NULL,year INTEGER NOT NULL,season INTEGER NOT NULL,metadata TEXT NOT NULL);
CREATE TABLE episodes(id INTEGER PRIMARY KEY,subject_id INTEGER NOT NULL,sort REAL NOT NULL,metadata TEXT NOT NULL);
CREATE TABLE people(kind TEXT NOT NULL,id INTEGER NOT NULL,name TEXT NOT NULL,summary TEXT NOT NULL,PRIMARY KEY(kind,id));
CREATE TABLE credits(subject_id INTEGER NOT NULL,kind TEXT NOT NULL,person_id INTEGER NOT NULL,role INTEGER NOT NULL,ordinal INTEGER NOT NULL,PRIMARY KEY(subject_id,kind,person_id,role));
CREATE TABLE voices(subject_id INTEGER NOT NULL,person_id INTEGER NOT NULL,character_id INTEGER NOT NULL,summary TEXT NOT NULL,PRIMARY KEY(subject_id,person_id,character_id));
CREATE TABLE relations(subject_id INTEGER NOT NULL,related_id INTEGER NOT NULL,kind INTEGER NOT NULL,ordinal INTEGER NOT NULL,PRIMARY KEY(subject_id,related_id,kind));
`

var requiredFiles = []string{"subject.jsonlines", "episode.jsonlines", "person.jsonlines", "character.jsonlines", "subject-characters.jsonlines", "subject-persons.jsonlines", "person-characters.jsonlines", "subject-relations.jsonlines"}

// Import builds an isolated database. The optional commit gate lets the app
// serialize activation with enable/config changes, without locking during I/O.
func (c *Catalog) Import(ctx context.Context, path string, m Manifest, notify func(Progress), commit func(func() error) error) (Snapshot, error) {
	if !c.importMu.TryLock() {
		return Snapshot{}, ErrBusy
	}
	defer c.importMu.Unlock()
	if err := m.Validate(); err != nil {
		return Snapshot{}, err
	}
	if notify != nil {
		notify(Progress{Phase: "verify", TotalBytes: m.Size})
	}
	if err := verifyFile(ctx, path, m); err != nil {
		return Snapshot{}, err
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer z.Close()
	files := map[string]*zip.File{}
	var unpacked uint64
	if len(z.File) > 32 {
		return Snapshot{}, errors.New("archive_member_limit")
	}
	for _, f := range z.File {
		if filepath.Base(f.Name) != f.Name || strings.ContainsAny(f.Name, `/\\`) || f.FileInfo().IsDir() {
			return Snapshot{}, errors.New("archive_invalid_member")
		}
		if _, exists := files[f.Name]; exists {
			return Snapshot{}, errors.New("archive_duplicate_member")
		}
		if f.UncompressedSize64 > 8<<30 {
			return Snapshot{}, errors.New("archive_member_too_large")
		}
		unpacked += f.UncompressedSize64
		if unpacked > 16<<30 {
			return Snapshot{}, errors.New("archive_unpacked_limit")
		}
		files[f.Name] = f
	}
	for _, name := range requiredFiles {
		if f := files[name]; f == nil || f.UncompressedSize64 == 0 {
			return Snapshot{}, fmt.Errorf("archive_missing_member: %s", name)
		}
	}
	if err = os.MkdirAll(c.dir, 0700); err != nil {
		return Snapshot{}, err
	}
	temp, err := os.CreateTemp(c.dir, "catalog-*.db")
	if err != nil {
		return Snapshot{}, err
	}
	dbPath := temp.Name()
	temp.Close()
	activated := false
	defer func() {
		if !activated {
			_ = os.Remove(dbPath)
		}
	}()
	db, err := sql.Open("sqlite", filepath.ToSlash(dbPath))
	if err != nil {
		return Snapshot{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;"+schema); err != nil {
		return Snapshot{}, err
	}
	b := builder{db: db, files: files, subjects: map[int64]bool{}, people: map[string]map[int64]bool{"character": {}, "person": {}}, notify: notify}
	if err = b.build(ctx); err != nil {
		return Snapshot{}, err
	}
	if notify != nil {
		notify(Progress{Phase: "indexes", Rows: b.rows})
	}
	_, err = db.ExecContext(ctx, `CREATE INDEX subjects_season ON subjects(year,season,date);CREATE INDEX episodes_subject ON episodes(subject_id,sort);CREATE INDEX credits_subject ON credits(subject_id,kind,role,ordinal);CREATE INDEX voices_subject ON voices(subject_id,character_id);`)
	if err != nil {
		return Snapshot{}, err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return Snapshot{}, errors.New("archive_integrity_failed")
	}
	snap := Snapshot{Manifest: m, File: filepath.Base(dbPath), ImportedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, item := range []struct {
		query string
		dest  *int
	}{{"SELECT count(*) FROM subjects", &snap.Subjects}, {"SELECT count(*) FROM episodes", &snap.Episodes}, {"SELECT count(*) FROM people WHERE kind='character'", &snap.Characters}, {"SELECT count(*) FROM people WHERE kind='person'", &snap.Persons}} {
		if err = db.QueryRowContext(ctx, item.query).Scan(item.dest); err != nil {
			return Snapshot{}, err
		}
	}
	if snap.Subjects == 0 {
		return Snapshot{}, errors.New("archive_has_no_animation")
	}
	if err = db.Close(); err != nil {
		return Snapshot{}, err
	}
	if commit == nil {
		commit = func(fn func() error) error { return fn() }
	}
	err = commit(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if notify != nil {
			notify(Progress{Phase: "activate", Rows: b.rows})
		}
		newDB, err := openSnapshot(dbPath)
		if err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err = writePointer(c.dir, snap); err != nil {
			newDB.Close()
			return err
		}
		old := c.db
		c.db, c.snapshot = newDB, snap
		activated = true
		if old != nil {
			_ = old.Close()
		}
		return nil
	})
	return snap, err
}

func verifyFile(ctx context.Context, path string, m Manifest) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != m.Size {
		return errors.New("archive_size_mismatch")
	}
	h := sha256.New()
	buf := make([]byte, 128<<10)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, e := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if "sha256:"+hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.Digest) {
		return errors.New("archive_digest_mismatch")
	}
	return nil
}

func writePointer(dir string, s Snapshot) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "pointer-*.tmp")
	if err != nil {
		return err
	}
	defer func() { f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "current.json"))
}

type builder struct {
	db       *sql.DB
	files    map[string]*zip.File
	subjects map[int64]bool
	people   map[string]map[int64]bool
	notify   func(Progress)
	rows     int64
}

// Each source is decoded a line at a time. Only the compact animation ID sets
// stay in memory; large raw records and episode arrays never accumulate.
func (b *builder) lines(ctx context.Context, name string, fn func(*sql.Stmt, []byte) error, query string) error {
	if b.notify != nil {
		b.notify(Progress{Phase: name, Rows: b.rows})
	}
	f, err := b.files[name].Open()
	if err != nil {
		return err
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, int64(b.files[name].UncompressedSize64)+1))
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer st.Close()
	var count int64
	for scan.Scan() {
		if err = ctx.Err(); err != nil {
			return err
		}
		if len(strings.TrimSpace(scan.Text())) == 0 {
			continue
		}
		if err = fn(st, scan.Bytes()); err != nil {
			return fmt.Errorf("%s row %d: %w", name, count+1, err)
		}
		count++
		b.rows++
		if count%10000 == 0 && b.notify != nil {
			b.notify(Progress{Phase: name, Rows: b.rows})
		}
	}
	if err = scan.Err(); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("archive_empty_member: %s", name)
	}
	return tx.Commit()
}

func (b *builder) build(ctx context.Context) error {
	err := b.lines(ctx, "subject.jsonlines", func(st *sql.Stmt, data []byte) error {
		var raw struct {
			ID       int64   `json:"id"`
			Type     int     `json:"type"`
			Name     string  `json:"name"`
			NameCN   string  `json:"name_cn"`
			Summary  string  `json:"summary"`
			Date     string  `json:"date"`
			Platform int     `json:"platform"`
			Infobox  string  `json:"infobox"`
			Score    float64 `json:"score"`
			NSFW     bool    `json:"nsfw"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if raw.Type != 2 {
			return nil
		}
		if raw.ID <= 0 || strings.TrimSpace(raw.Name) == "" {
			return errors.New("invalid_archive_subject")
		}
		name := raw.NameCN
		if name == "" {
			name = raw.Name
		}
		r := Record{Subject: anime.Subject{ID: raw.ID, BangumiID: raw.ID, Name: name, OriginalName: raw.Name, Summary: raw.Summary, Date: raw.Date, Rating: raw.Score, Platform: platform(raw.Platform)}, NSFW: raw.NSFW, Infobox: raw.Infobox}
		if date, err := time.Parse("2006-01-02", raw.Date); err == nil {
			r.Year = date.Year()
			r.Season = (int(date.Month())-1)/3 + 1
		}
		meta, _ := json.Marshal(r)
		if _, err := st.ExecContext(ctx, raw.ID, name, raw.Name, raw.Date, r.Year, r.Season, string(meta)); err != nil {
			return err
		}
		b.subjects[raw.ID] = true
		return nil
	}, "INSERT INTO subjects VALUES(?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	for _, kind := range []string{"character", "person"} {
		file := "subject-characters.jsonlines"
		if kind == "person" {
			file = "subject-persons.jsonlines"
		}
		err = b.lines(ctx, file, func(st *sql.Stmt, data []byte) error {
			var r struct {
				SubjectID   int64 `json:"subject_id"`
				CharacterID int64 `json:"character_id"`
				PersonID    int64 `json:"person_id"`
				Type        int   `json:"type"`
				Position    int   `json:"position"`
				Order       int   `json:"order"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return err
			}
			if !b.subjects[r.SubjectID] {
				return nil
			}
			id, role := r.CharacterID, r.Type
			if kind == "person" {
				id, role = r.PersonID, r.Position
			}
			if id <= 0 {
				return errors.New("invalid_credit")
			}
			if _, err := st.ExecContext(ctx, r.SubjectID, kind, id, role, r.Order); err != nil {
				return err
			}
			b.people[kind][id] = true
			return nil
		}, "INSERT OR IGNORE INTO credits VALUES(?,?,?,?,?)")
		if err != nil {
			return err
		}
	}
	err = b.lines(ctx, "person-characters.jsonlines", func(st *sql.Stmt, data []byte) error {
		var r struct {
			SubjectID   int64  `json:"subject_id"`
			PersonID    int64  `json:"person_id"`
			CharacterID int64  `json:"character_id"`
			Summary     string `json:"summary"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		if !b.subjects[r.SubjectID] {
			return nil
		}
		if r.PersonID <= 0 || r.CharacterID <= 0 {
			return errors.New("invalid_voice")
		}
		b.people["person"][r.PersonID] = true
		b.people["character"][r.CharacterID] = true
		_, err := st.ExecContext(ctx, r.SubjectID, r.PersonID, r.CharacterID, r.Summary)
		return err
	}, "INSERT OR IGNORE INTO voices VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	for _, kind := range []string{"character", "person"} {
		err = b.lines(ctx, kind+".jsonlines", func(st *sql.Stmt, data []byte) error {
			var r struct {
				ID      int64  `json:"id"`
				Name    string `json:"name"`
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(data, &r); err != nil {
				return err
			}
			if !b.people[kind][r.ID] {
				return nil
			}
			if r.Name == "" {
				return errors.New("invalid_archive_person")
			}
			_, err := st.ExecContext(ctx, kind, r.ID, r.Name, r.Summary)
			return err
		}, "INSERT INTO people VALUES(?,?,?,?)")
		if err != nil {
			return err
		}
	}
	err = b.lines(ctx, "episode.jsonlines", func(st *sql.Stmt, data []byte) error {
		var r struct {
			ID          int64       `json:"id"`
			SubjectID   int64       `json:"subject_id"`
			Name        string      `json:"name"`
			NameCN      string      `json:"name_cn"`
			Description string      `json:"description"`
			Airdate     string      `json:"airdate"`
			Sort        json.Number `json:"sort"`
			Type        int         `json:"type"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		if !b.subjects[r.SubjectID] {
			return nil
		}
		n, err := r.Sort.Float64()
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || r.ID <= 0 {
			return errors.New("invalid_archive_episode")
		}
		season := 0
		if r.Type == 0 && n == math.Trunc(n) {
			season = 1
		}
		name := r.NameCN
		if name == "" {
			name = r.Name
		}
		e := anime.Episode{ID: r.ID, BangumiID: r.ID, SubjectID: r.SubjectID, Name: name, OriginalName: r.Name, Summary: r.Description, Sort: r.Sort, Number: r.Sort, Active: true, Type: r.Type, Season: season, AirDate: r.Airdate}
		meta, _ := json.Marshal(e)
		_, err = st.ExecContext(ctx, r.ID, r.SubjectID, n, string(meta))
		return err
	}, "INSERT INTO episodes VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	return b.lines(ctx, "subject-relations.jsonlines", func(st *sql.Stmt, data []byte) error {
		var r struct {
			SubjectID int64 `json:"subject_id"`
			RelatedID int64 `json:"related_subject_id"`
			Type      int   `json:"relation_type"`
			Order     int   `json:"order"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		if !b.subjects[r.SubjectID] || !b.subjects[r.RelatedID] {
			return nil
		}
		_, err := st.ExecContext(ctx, r.SubjectID, r.RelatedID, r.Type, r.Order)
		return err
	}, "INSERT OR IGNORE INTO relations VALUES(?,?,?,?)")
}

// Numeric meanings are from bangumi/common subject_platforms.yml, not v0's
// string platform field. Unknown types remain unknown rather than becoming TV.
func platform(n int) string {
	switch n {
	case 1:
		return "TV"
	case 2:
		return "OVA"
	case 3:
		return "剧场版"
	case 4:
		return "短片"
	case 5:
		return "WEB"
	case 2006:
		return "动态漫画"
	default:
		return "其他"
	}
}

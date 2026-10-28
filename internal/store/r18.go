package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"emby-go/internal/r18"
	"github.com/google/uuid"
)

const SourceR18 = "postgres_r18"

func (s *Store) migrateR18Libraries() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var schema string
	if err = conn.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='libraries'").Scan(&schema); err != nil {
		return err
	}
	if !strings.Contains(schema, SourceR18) {
		// Rebuild the parent's CHECK constraint on one reserved startup connection.
		// Disable cascades only here, preserve IDs/views/sequence, and verify all FKs before publishing.
		if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF; PRAGMA legacy_alter_table=ON"); err != nil {
			return err
		}
		defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA legacy_alter_table=OFF")
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var sequence int64
		_ = tx.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='libraries'").Scan(&sequence)
		rows, e := tx.Query("SELECT sql FROM sqlite_master WHERE tbl_name='libraries' AND type IN ('index','trigger') AND sql IS NOT NULL")
		if e != nil {
			return e
		}
		definitions := []string{}
		for rows.Next() {
			var d string
			if e = rows.Scan(&d); e != nil {
				rows.Close()
				return e
			}
			definitions = append(definitions, d)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		_, e = tx.Exec(`CREATE TABLE libraries_r18_new (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, path TEXT UNIQUE NOT NULL CHECK(length(trim(path))>0), type TEXT NOT NULL DEFAULT 'movies', enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)), source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type IN ('nfo_directory','postgres_r18')), revision INTEGER NOT NULL DEFAULT 1, proxy_enabled INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0,1)), resolve_redirects INTEGER NOT NULL DEFAULT 0 CHECK(resolve_redirects IN (0,1)));
INSERT INTO libraries_r18_new(id,name,path,type,enabled,source_type,revision,proxy_enabled,resolve_redirects) SELECT id,name,path,type,enabled,source_type,revision,COALESCE(proxy_enabled,0),resolve_redirects FROM libraries;
DROP TABLE libraries;
ALTER TABLE libraries_r18_new RENAME TO libraries;`)
		if e != nil {
			return fmt.Errorf("upgrade R18 library sources: %w", e)
		}
		if _, e = tx.Exec("UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name='libraries'", sequence); e != nil {
			return e
		}
		for _, d := range definitions {
			if _, e = tx.Exec(d); e != nil {
				return e
			}
		}
		check, e := tx.Query("PRAGMA foreign_key_check")
		if e != nil {
			return e
		}
		invalid := check.Next()
		e = check.Err()
		check.Close()
		if e != nil {
			return e
		}
		if invalid {
			return errors.New("R18 library migration would break foreign keys")
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS r18_library_connections(library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE, host TEXT NOT NULL, port INTEGER NOT NULL, database_name TEXT NOT NULL, username TEXT NOT NULL, password TEXT NOT NULL, ssl_mode TEXT NOT NULL)`)
	return err
}

func (s *Store) CreateR18Library(name string, c r18.Config) (Library, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return Library{}, errors.New("media library name is required (max 120 characters)")
	}
	if err := c.Validate(); err != nil {
		return Library{}, err
	}
	tx, e := s.db.Begin()
	if e != nil {
		return Library{}, e
	}
	defer tx.Rollback()
	var id int64
	if e = tx.QueryRow("INSERT INTO libraries(name,path,type,source_type) VALUES(?,?,?,?) RETURNING id", name, "r18://"+uuid.NewString(), LibraryMovies, SourceR18).Scan(&id); e != nil {
		return Library{}, e
	}
	if _, e = tx.Exec("INSERT INTO r18_library_connections VALUES(?,?,?,?,?,?,?)", id, c.Host, c.Port, c.Database, c.Username, c.Password, c.SSLMode); e != nil {
		return Library{}, e
	}
	if e = tx.Commit(); e != nil {
		return Library{}, e
	}
	_ = s.BumpVersion(id)
	return s.Library(id)
}

func (s *Store) R18Connection(id int64) (r18.Config, error) {
	var c r18.Config
	err := s.db.QueryRow("SELECT host,port,database_name,username,password,ssl_mode FROM r18_library_connections WHERE library_id=?", id).Scan(&c.Host, &c.Port, &c.Database, &c.Username, &c.Password, &c.SSLMode)
	return c, err
}

func (s *Store) UpdateR18Connection(id, revision int64, name string, c r18.Config) (Library, error) {
	if e := c.Validate(); e != nil {
		return Library{}, e
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return Library{}, errors.New("media library name is required")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return Library{}, e
	}
	defer tx.Rollback()
	result, e := tx.Exec("UPDATE libraries SET name=?,revision=revision+1 WHERE id=? AND source_type=? AND revision=?", name, id, SourceR18, revision)
	if e != nil {
		return Library{}, e
	}
	if e = affectedOne(result, nil); e != nil {
		return Library{}, e
	}
	result, e = tx.Exec("UPDATE r18_library_connections SET host=?,port=?,database_name=?,username=?,password=?,ssl_mode=? WHERE library_id=?", c.Host, c.Port, c.Database, c.Username, c.Password, c.SSLMode, id)
	if e != nil {
		return Library{}, e
	}
	if e = affectedOne(result, nil); e != nil {
		return Library{}, e
	}
	if e = tx.Commit(); e != nil {
		return Library{}, e
	}
	_ = s.BumpVersion(id)
	return s.Library(id)
}

// MarshalR18Connection never exposes the password through management responses.
func MarshalR18Connection(c r18.Config) map[string]any {
	b, _ := json.Marshal(c)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	out["has_password"] = c.Password != ""
	return out
}

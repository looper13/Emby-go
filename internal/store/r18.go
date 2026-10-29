package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"emby-go/internal/r18"
	"github.com/google/uuid"
)

const SourceR18 = "avdb_javdb"

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
		// 仅在启动连接上重建来源约束，保留外键、授权、视图和自增序列。
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
		_, e = tx.Exec(`CREATE TABLE libraries_r18_new (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, path TEXT UNIQUE NOT NULL CHECK(length(trim(path))>0), type TEXT NOT NULL DEFAULT 'movies', enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)), source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type IN ('nfo_directory','avdb_javdb')), revision INTEGER NOT NULL DEFAULT 1, proxy_enabled INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0,1)), resolve_redirects INTEGER NOT NULL DEFAULT 0 CHECK(resolve_redirects IN (0,1)));
INSERT INTO libraries_r18_new(id,name,path,type,enabled,source_type,revision,proxy_enabled,resolve_redirects) SELECT id,name,path,type,enabled,CASE WHEN source_type='postgres_r18' THEN 'avdb_javdb' ELSE source_type END,revision+CASE WHEN source_type='postgres_r18' THEN 1 ELSE 0 END,COALESCE(proxy_enabled,0),resolve_redirects FROM libraries;
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
	// 旧 PostgreSQL 连接记录仅保留历史；运行时只读取新的 API 配置。
	_, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS r18_api_connections(library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE, base_url TEXT NOT NULL, api_key TEXT NOT NULL)`)
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
	if _, e = tx.Exec("INSERT INTO r18_api_connections(library_id,base_url,api_key) VALUES(?,?,?)", id, c.BaseURL, c.APIKey); e != nil {
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
	err := s.db.QueryRow("SELECT base_url,api_key FROM r18_api_connections WHERE library_id=?", id).Scan(&c.BaseURL, &c.APIKey)
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
	// 已迁移的旧资料库没有 API 配置，首次保存时创建而不是读取旧密码。
	if _, e = tx.Exec("INSERT INTO r18_api_connections(library_id,base_url,api_key) VALUES(?,?,?) ON CONFLICT(library_id) DO UPDATE SET base_url=excluded.base_url,api_key=excluded.api_key", id, c.BaseURL, c.APIKey); e != nil {
		return Library{}, e
	}
	if e = tx.Commit(); e != nil {
		return Library{}, e
	}
	_ = s.BumpVersion(id)
	return s.Library(id)
}

// 管理接口仅返回密钥是否配置，绝不回显密钥或旧 PostgreSQL 凭据。
func MarshalR18Connection(c r18.Config) map[string]any {
	return map[string]any{"base_url": c.BaseURL, "has_api_key": c.APIKey != ""}
}

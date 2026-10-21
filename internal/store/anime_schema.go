package store

import (
	"context"
	"fmt"
)

// migrateAnime runs before background readers start. SQLite requires foreign_keys
// to be changed outside a transaction on the SAME connection when rebuilding a
// referenced table. Renaming the old table would rewrite child FK definitions.
func (s *Store) migrateAnime() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var done int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=1`).Scan(&done); err != nil || done != 0 {
		return err
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var librarySequence int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='libraries'),0)`).Scan(&librarySequence); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
CREATE TABLE libraries_anime_v1 (
 id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, path TEXT UNIQUE,
 type TEXT NOT NULL DEFAULT 'movies', enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type IN ('nfo_directory','anime_catalog')),
 system_key TEXT UNIQUE, revision INTEGER NOT NULL DEFAULT 1,
 proxy_enabled INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0,1)),
 CHECK((source_type='nfo_directory' AND path IS NOT NULL AND length(trim(path))>0) OR (source_type='anime_catalog' AND path IS NULL))
);
INSERT INTO libraries_anime_v1(id,name,path,type,enabled) SELECT id,name,path,COALESCE(type,'movies'),COALESCE(enabled,1) FROM libraries;
DROP TABLE libraries;
ALTER TABLE libraries_anime_v1 RENAME TO libraries;
CREATE TABLE anime_subjects (
 id INTEGER PRIMARY KEY AUTOINCREMENT, bangumi_id INTEGER NOT NULL UNIQUE CHECK(bangumi_id>0),
 name TEXT NOT NULL, platform TEXT NOT NULL, metadata TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE anime_episodes (
 id INTEGER PRIMARY KEY AUTOINCREMENT, subject_id INTEGER NOT NULL REFERENCES anime_subjects(id) ON DELETE CASCADE,
 bangumi_id INTEGER NOT NULL UNIQUE CHECK(bangumi_id>0), sort TEXT NOT NULL, season INTEGER NOT NULL CHECK(season IN (0,1)),
 emby_index INTEGER NOT NULL CHECK(emby_index>0), metadata TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1,
 UNIQUE(subject_id,season,emby_index)
);
CREATE INDEX anime_episodes_subject ON anime_episodes(subject_id,season,emby_index);
CREATE TABLE anime_mikan_entries (
 id INTEGER PRIMARY KEY CHECK(id>0), subject_id INTEGER REFERENCES anime_subjects(id),
 name TEXT NOT NULL, year INTEGER NOT NULL, season INTEGER NOT NULL, weekday INTEGER NOT NULL,
 metadata TEXT NOT NULL, manual INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL,
 last_opened_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX anime_entries_season ON anime_mikan_entries(year,season,weekday);
CREATE TABLE anime_catalog_entries (year INTEGER NOT NULL,season INTEGER NOT NULL,mikan_id INTEGER NOT NULL REFERENCES anime_mikan_entries(id) ON DELETE CASCADE,weekday INTEGER NOT NULL,PRIMARY KEY(year,season,mikan_id));
CREATE TABLE anime_releases (
 id TEXT PRIMARY KEY, mikan_id INTEGER NOT NULL REFERENCES anime_mikan_entries(id) ON DELETE CASCADE,
 metadata TEXT NOT NULL, published_at TEXT NOT NULL
);
CREATE INDEX anime_releases_entry ON anime_releases(mikan_id,published_at);
CREATE TABLE anime_release_episodes (
 release_id TEXT NOT NULL REFERENCES anime_releases(id) ON DELETE CASCADE,
 episode_id INTEGER NOT NULL REFERENCES anime_episodes(id) ON DELETE CASCADE, PRIMARY KEY(release_id,episode_id)
);
CREATE TABLE anime_media_bindings (
 id INTEGER PRIMARY KEY AUTOINCREMENT, subject_id INTEGER NOT NULL REFERENCES anime_subjects(id) ON DELETE CASCADE,
 episode_id INTEGER REFERENCES anime_episodes(id) ON DELETE CASCADE,
 movie_id INTEGER UNIQUE REFERENCES movies(id) ON DELETE SET NULL,
 revision INTEGER NOT NULL DEFAULT 1, status TEXT NOT NULL DEFAULT 'unchecked' CHECK(status IN ('unbound','unchecked','checking','playable','unavailable')),
 source_version TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX anime_binding_episode ON anime_media_bindings(episode_id) WHERE episode_id IS NOT NULL;
CREATE UNIQUE INDEX anime_binding_film ON anime_media_bindings(subject_id) WHERE episode_id IS NULL;
CREATE TABLE anime_sync_state (scope TEXT PRIMARY KEY, generation INTEGER NOT NULL, status TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', last_success_at TEXT NOT NULL DEFAULT '');
INSERT INTO schema_migrations(version) VALUES(1);
`)
	if err != nil {
		return fmt.Errorf("anime schema migration: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name='libraries'`, librarySequence); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	bad := rows.Next()
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return readErr
	}
	if bad {
		return fmt.Errorf("anime schema migration: foreign key check failed")
	}
	var integrity string
	if err = tx.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("anime schema migration: integrity check failed: %s", integrity)
	}
	return tx.Commit()
}

// migrateLibraryProxy adds the per-library stream proxy switch to databases
// created before the option existed. New databases already contain the column
// in the version-1 schema above.
func (s *Store) migrateLibraryProxy() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var done int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=2`).Scan(&done); err != nil {
		return err
	}
	if done != 0 {
		return nil
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(libraries)`)
	if err != nil {
		return err
	}
	hasColumn := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "proxy_enabled" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasColumn {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE libraries ADD COLUMN proxy_enabled INTEGER NOT NULL DEFAULT 0 CHECK(proxy_enabled IN (0,1))`); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(2)`)
	return err
}

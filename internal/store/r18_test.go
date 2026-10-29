package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/r18"
)

func TestR18LibraryMigrationPersistenceAndBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	old, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = old.Exec(`PRAGMA foreign_keys=ON;
CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT NOT NULL DEFAULT 'movies',enabled INTEGER NOT NULL DEFAULT 1,source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type='nfo_directory'),revision INTEGER NOT NULL DEFAULT 1,proxy_enabled INTEGER NOT NULL DEFAULT 0,resolve_redirects INTEGER NOT NULL DEFAULT 0);
INSERT INTO libraries(id,name,path) VALUES(7,'Existing','/existing');
INSERT INTO libraries(id,name,path) VALUES(500,'Deleted','/deleted'); DELETE FROM libraries WHERE id=500;
CREATE TABLE grant_probe(library_id INTEGER REFERENCES libraries(id) ON DELETE CASCADE); INSERT INTO grant_probe VALUES(7);
CREATE VIEW source_probe AS SELECT id,name FROM libraries;
CREATE INDEX custom_library_name ON libraries(name);
CREATE TABLE trigger_probe(name TEXT);
CREATE TRIGGER custom_library_insert AFTER INSERT ON libraries BEGIN INSERT INTO trigger_probe VALUES(NEW.name); END;`)
	old.Close()
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	var grants int
	if e = s.db.QueryRow("SELECT count(*) FROM grant_probe").Scan(&grants); e != nil || grants != 1 {
		t.Fatal("cascade erased grant", e)
	}
	var name string
	if e = s.db.QueryRow("SELECT name FROM source_probe WHERE id=7").Scan(&name); e != nil || name != "Existing" {
		t.Fatal("view lost", e)
	}
	id, e := s.UpsertMovie(Movie{LibraryID: 7, SourcePath: "/existing/a.strm", Status: "success", Title: "Kept"}, 0, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetFavorite(id, true); e != nil {
		t.Fatal(e)
	}
	config := r18.Config{BaseURL: "https://api.example.invalid", APIKey: "private-fixture"}
	lib, e := s.CreateR18Library("R18", config)
	if e != nil || lib.ID <= 500 || lib.SourceType != SourceR18 || lib.IsDirectory() {
		t.Fatal("source creation/sequence", lib, e)
	}
	if e = s.RequireDirectory(lib.ID); e == nil {
		t.Fatal("external source gained filesystem capabilities")
	}
	if e = s.db.QueryRow("SELECT name FROM trigger_probe").Scan(&name); e != nil || name != "R18" {
		t.Fatal("custom trigger lost", name, e)
	}
	if _, e = s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: "fake.strm"}, 0, time.Now()); e == nil {
		t.Fatal("external source entered directory index")
	}
	if e = s.migrateR18Libraries(); e != nil {
		t.Fatal("idempotence", e)
	}
	s.waitFeaturesReady()
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := s.R18Connection(lib.ID)
	if e != nil || restored != config {
		t.Fatal("connection persistence", e)
	}
	if d, e := s.Data(id); e != nil || !d.IsFavorite {
		t.Fatal("old user state changed", e)
	}
	redacted, _ := json.Marshal(MarshalR18Connection(restored))
	if strings.Contains(string(redacted), config.APIKey) || strings.Contains(string(redacted), `"api_key"`) || !strings.Contains(string(redacted), `"has_api_key":true`) {
		t.Fatal("API key exposed or configuration flag missing", string(redacted))
	}
	config.BaseURL = "https://new.example.invalid"
	if _, e = s.UpdateR18Connection(lib.ID, lib.Revision, "Renamed", config); e != nil {
		t.Fatal("update", e)
	}
	if _, e = s.UpdateR18Connection(lib.ID, lib.Revision, "Stale", config); e != ErrConflict {
		t.Fatal("stale update allowed", e)
	}
	if e = s.DeleteLibrary(lib.ID); e != nil {
		t.Fatal("delete", e)
	}
	if _, e = s.R18Connection(lib.ID); e != sql.ErrNoRows {
		t.Fatal("connection not removed", e)
	}
	if _, e = s.Movie(id); e != nil {
		t.Fatal("deleting external library changed local movies", e)
	}
}

func TestR18PostgreSQLMigrationPreservesHistoryAndNeedsAPIConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`PRAGMA foreign_keys=ON;
CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT NOT NULL DEFAULT 'movies',enabled INTEGER NOT NULL DEFAULT 1,source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type IN ('nfo_directory','postgres_r18')),revision INTEGER NOT NULL DEFAULT 1,proxy_enabled INTEGER NOT NULL DEFAULT 0,resolve_redirects INTEGER NOT NULL DEFAULT 0);
INSERT INTO libraries(id,name,path,source_type,revision) VALUES(7,'Directory','/existing','nfo_directory',3),(9,'Legacy R18','r18://legacy','postgres_r18',4),(500,'Deleted','/deleted','nfo_directory',1);
DELETE FROM libraries WHERE id=500;
CREATE TABLE r18_library_connections(library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE,host TEXT NOT NULL,port INTEGER NOT NULL,database_name TEXT NOT NULL,username TEXT NOT NULL,password TEXT NOT NULL,ssl_mode TEXT NOT NULL);
INSERT INTO r18_library_connections VALUES(9,'legacy.invalid',5432,'legacy','reader','legacy-fixture-secret','prefer');
CREATE TABLE users(id INTEGER PRIMARY KEY AUTOINCREMENT,username TEXT NOT NULL COLLATE NOCASE UNIQUE,password_hash TEXT NOT NULL,is_administrator INTEGER NOT NULL DEFAULT 0,is_disabled INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL);
INSERT INTO users(id,username,password_hash,created_at) VALUES(2,'ordinary','fixture-hash','2026-01-01T00:00:00Z');
CREATE TABLE user_policy(user_id INTEGER PRIMARY KEY,enable_all_folders INTEGER NOT NULL DEFAULT 0); INSERT INTO user_policy VALUES(2,0);
CREATE TABLE user_library_grants(user_id INTEGER NOT NULL,library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,PRIMARY KEY(user_id,library_id)); INSERT INTO user_library_grants VALUES(2,9);
CREATE TABLE offline_tasks(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,info_hash TEXT NOT NULL,status TEXT NOT NULL,next_poll INTEGER NOT NULL,version INTEGER NOT NULL,data TEXT NOT NULL,UNIQUE(account_id,info_hash));
INSERT INTO offline_tasks VALUES('old-task','fixture-account','old-hash','ready',0,1,'{"id":"old-task","account_id":"fixture-account","status":"ready"}');
CREATE TABLE r18_offline_bindings(library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,library_revision INTEGER NOT NULL,video_id TEXT NOT NULL,resource_id TEXT NOT NULL,task_id TEXT NOT NULL REFERENCES offline_tasks(id),file_id TEXT NOT NULL DEFAULT '',is_default INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(library_id,library_revision,video_id,task_id));
INSERT INTO r18_offline_bindings VALUES(9,4,'old-video','old-resource','old-task','old-file',1);
CREATE VIEW source_probe AS SELECT id,name,source_type,revision FROM libraries;
CREATE INDEX custom_library_name ON libraries(name);
CREATE TABLE trigger_probe(name TEXT);
CREATE TRIGGER custom_library_insert AFTER INSERT ON libraries BEGIN INSERT INTO trigger_probe VALUES(NEW.name); END;`)
	old.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	lib, err := s.Library(9)
	if err != nil || lib.SourceType != SourceR18 || lib.Revision != 5 || lib.Path != "r18://legacy" {
		t.Fatal("legacy source identity/revision changed unexpectedly", lib, err)
	}
	directory, err := s.Library(7)
	if err != nil || !directory.IsDirectory() || directory.Revision != 3 {
		t.Fatal("directory source changed", directory, err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM user_library_grants WHERE user_id=2 AND library_id=9").Scan(&count); err != nil || count != 1 || !s.ForUser(2).CanAccessLibrary(9) {
		t.Fatal("library grant lost", count, err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM r18_offline_bindings WHERE library_id=9 AND library_revision=4 AND video_id='old-video' AND task_id='old-task' AND resource_id='old-resource' AND file_id='old-file' AND is_default=1").Scan(&count); err != nil || count != 1 {
		t.Fatal("playback history lost or rebound across sources", count, err)
	}
	historyLib := lib
	historyLib.Revision = 4
	history, err := s.OfflineBindings(historyLib, "old-video", "fixture-account")
	if err != nil || len(history) != 1 || history[0].Task.ID != "old-task" || history[0].FileID != "old-file" {
		t.Fatal("old offline task is no longer readable", err)
	}
	current, err := s.OfflineBindings(lib, "old-video", "fixture-account")
	if err != nil || len(current) != 0 {
		t.Fatal("old offline task was rebound to the API source", err)
	}
	var secret, source string
	if err = s.db.QueryRow("SELECT password FROM r18_library_connections WHERE library_id=9").Scan(&secret); err != nil || secret != "legacy-fixture-secret" {
		t.Fatal("legacy connection history lost", err)
	}
	if err = s.db.QueryRow("SELECT source_type FROM source_probe WHERE id=9").Scan(&source); err != nil || source != SourceR18 {
		t.Fatal("source view lost", source, err)
	}
	if _, err = s.R18Connection(9); err != sql.ErrNoRows {
		t.Fatal("legacy PostgreSQL credentials were treated as API configuration", err)
	}
	if err = s.migrateR18Libraries(); err != nil {
		t.Fatal("migration is not idempotent", err)
	}
	lib, err = s.Library(9)
	if err != nil || lib.Revision != 5 {
		t.Fatal("repeated migration changed revision", lib, err)
	}
	if _, err = s.db.Exec("INSERT INTO libraries(name,path,source_type) VALUES('Disallowed','/disallowed','postgres_r18')"); err == nil {
		t.Fatal("legacy runtime source is still allowed")
	}
	config := r18.Config{BaseURL: "https://api.example.invalid", APIKey: "new-api-fixture"}
	updated, err := s.UpdateR18Connection(9, lib.Revision, "JavDB", config)
	if err != nil || updated.ID != 9 || updated.Revision != 6 {
		t.Fatal("first API configuration failed", updated, err)
	}
	stored, err := s.R18Connection(9)
	if err != nil || stored != config {
		t.Fatal("API configuration not persisted", err)
	}
	config.APIKey = "stale-fixture"
	if _, err = s.UpdateR18Connection(9, lib.Revision, "Stale", config); err != ErrConflict {
		t.Fatal("stale API update allowed", err)
	}
	stored, err = s.R18Connection(9)
	if err != nil || stored.APIKey != "new-api-fixture" {
		t.Fatal("stale update overwrote API key", err)
	}
	newLib, err := s.CreateR18Library("New JavDB", stored)
	if err != nil || newLib.ID <= 500 {
		t.Fatal("library sequence not preserved", newLib, err)
	}
	var name string
	if err = s.db.QueryRow("SELECT name FROM trigger_probe ORDER BY rowid DESC LIMIT 1").Scan(&name); err != nil || name != "New JavDB" {
		t.Fatal("library trigger not restored", name, err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='custom_library_name'").Scan(&count); err != nil || count != 1 {
		t.Fatal("library index not restored", count, err)
	}
}

func TestR18ConnectionMarshallingDoesNotExposeSecrets(t *testing.T) {
	for _, cfg := range []r18.Config{{}, {BaseURL: "https://api.example.invalid", APIKey: "api-fixture-secret"}} {
		out := MarshalR18Connection(cfg)
		if len(out) != 2 || out["base_url"] != cfg.BaseURL || out["has_api_key"] != (cfg.APIKey != "") {
			t.Fatal("unexpected management configuration", out)
		}
		if _, ok := out["api_key"]; ok {
			t.Fatal("management configuration exposes key field")
		}
		raw, err := json.Marshal(cfg)
		if err != nil || strings.Contains(string(raw), "api_key") || cfg.APIKey != "" && strings.Contains(string(raw), cfg.APIKey) {
			t.Fatal("configuration JSON exposes API key", err)
		}
	}
}

func TestR18MigrationRollsBackWhenForeignKeysAreInvalid(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=OFF;
CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT NOT NULL DEFAULT 'movies',enabled INTEGER NOT NULL DEFAULT 1,source_type TEXT NOT NULL DEFAULT 'nfo_directory' CHECK(source_type IN ('nfo_directory','postgres_r18')),revision INTEGER NOT NULL DEFAULT 1,proxy_enabled INTEGER NOT NULL DEFAULT 0,resolve_redirects INTEGER NOT NULL DEFAULT 0);
INSERT INTO libraries(id,name,path,source_type,revision) VALUES(9,'Legacy','r18://legacy','postgres_r18',4);
CREATE TABLE grant_probe(library_id INTEGER REFERENCES libraries(id) ON DELETE CASCADE);
INSERT INTO grant_probe VALUES(9),(404);`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{storeState: &storeState{db: db}}
	if err = s.migrateR18Libraries(); err == nil {
		t.Fatal("migration committed invalid foreign keys")
	}
	var source string
	var revision, enabled, temporary int
	if err = db.QueryRow("SELECT source_type,revision FROM libraries WHERE id=9").Scan(&source, &revision); err != nil || source != "postgres_r18" || revision != 4 {
		t.Fatal("failed migration changed source data", source, revision, err)
	}
	if err = db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatal("failed migration left foreign key enforcement disabled", enabled, err)
	}
	if err = db.QueryRow("PRAGMA legacy_alter_table").Scan(&enabled); err != nil || enabled != 0 {
		t.Fatal("failed migration left legacy alteration enabled", enabled, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name IN ('libraries_r18_new','r18_api_connections')").Scan(&temporary); err != nil || temporary != 0 {
		t.Fatal("failed migration left partial tables", temporary, err)
	}
}

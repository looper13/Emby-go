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
	config := r18.Config{Host: "db.example.invalid", Port: 5432, Database: "catalog", Username: "reader", Password: "private-fixture", SSLMode: "prefer"}
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
	if strings.Contains(string(redacted), config.Password) || strings.Contains(string(redacted), `"password"`) {
		t.Fatal("password exposed", string(redacted))
	}
	config.Host = "new.example.invalid"
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

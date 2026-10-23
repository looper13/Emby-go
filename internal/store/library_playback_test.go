package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLibraryResolveRedirectPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playback.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := s.AddLibraryWithPlaybackSettings("Resolve", "/resolve", false, true)
	if err != nil || !lib.ResolveRedirects {
		t.Fatalf("create: %+v %v", lib, err)
	}
	lib, err = s.AddLibrary("Resolve", "/resolve")
	if err != nil || !lib.ResolveRedirects {
		t.Fatalf("startup registration lost setting: %+v %v", lib, err)
	}
	lib, err = s.UpdateLibraryProxy(lib.ID, false, lib.Revision)
	if err != nil || !lib.ResolveRedirects {
		t.Fatalf("legacy update lost setting: %+v %v", lib, err)
	}
	expectedLibrary := lib
	movieID, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: "/resolve/movie.strm", Title: "Retained", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlayback(movieID, 12345, 2, "2026-10-23T07:00:00Z", 12345); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFavorite(movieID, true); err != nil {
		t.Fatal(err)
	}
	expectedData, err := s.Data(movieID)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a database containing the retired IP mapping configuration.
	if _, err := s.db.Exec(`ALTER TABLE libraries ADD COLUMN source_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE libraries ADD COLUMN target_ip TEXT NOT NULL DEFAULT '';
UPDATE libraries SET source_ip='192.168.1.4',target_ip='10.126.126.2'`); err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, err = s.Library(lib.ID)
	if err != nil || lib != expectedLibrary {
		t.Fatalf("reopen lost setting: %+v %v", lib, err)
	}
	var retiredColumns int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('libraries') WHERE name IN ('source_ip','target_ip')`).Scan(&retiredColumns); err != nil || retiredColumns != 0 {
		t.Fatalf("retired columns remain: count=%d err=%v", retiredColumns, err)
	}
	if movie, err := s.Movie(movieID); err != nil || movie.LibraryID != lib.ID || movie.Title != "Retained" {
		t.Fatalf("migration changed movie: %+v %v", movie, err)
	}
	if data, err := s.Data(movieID); err != nil || data != expectedData {
		t.Fatalf("migration changed playback/favorite: %+v %v", data, err)
	}
	if policy, err := s.LibraryAccessPolicy(); err != nil || policy.EnableAllFolders || len(policy.LibraryIDs) != 1 || policy.LibraryIDs[0] != lib.ID {
		t.Fatalf("migration changed library grants: %+v %v", policy, err)
	}
	if err := s.migrateDirectoryLibraries(); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	lib, err = s.AddLibrary("Resolve", "/resolve")
	if err != nil || !lib.ResolveRedirects {
		t.Fatalf("registration with retired columns failed: %+v %v", lib, err)
	}
	encoded, err := json.Marshal(lib)
	if err != nil || strings.Contains(string(encoded), "SourceIP") || strings.Contains(string(encoded), "TargetIP") {
		t.Fatalf("retired fields exposed: %s %v", encoded, err)
	}
}

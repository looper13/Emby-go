package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountPolicyPersistenceAndQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	one, err := s.AddLibrary("Allowed", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.AddLibrary("Denied", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, lib := range []Library{one, two} {
		id, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "film.strm"), Title: lib.Name, Status: "success", Tags: []string{lib.Name}, Collection: lib.Name}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SavePlayback(id, 10000000, 0, "", -1); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := s.LibraryAccessPolicy()
	if err != nil || !policy.EnableAllFolders || !s.CanAccessLibrary(two.ID) {
		t.Fatal("existing installations must initially retain access", policy, err)
	}
	version := s.AccessVersion()
	if err := s.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{one.ID, one.ID}}); err != nil {
		t.Fatal(err)
	}
	if version == s.AccessVersion() {
		t.Fatal("policy update did not invalidate cached browsing")
	}
	version = s.AccessVersion()
	if err := s.SaveLibraryAccessPolicy(LibraryAccessPolicy{EnableAllFolders: true, LibraryIDs: []int64{two.ID, -1}}); !errors.Is(err, ErrInvalidLibrary) {
		t.Fatal(err)
	}
	if version != s.AccessVersion() || s.CanAccessLibrary(two.ID) {
		t.Fatal("invalid grants were not rolled back")
	}
	items, count, err := s.SearchFiltered(0, "", "", "", false, "SortName", false, 1, 0)
	if err != nil || count != 1 || len(items) != 1 || items[0].LibraryID != one.ID {
		t.Fatal("search leaked denied library", items, count, err)
	}
	items, count, err = s.Resumed(1, 0)
	if err != nil || count != 1 || len(items) != 1 || items[0].LibraryID != one.ID {
		t.Fatal("resume leaked denied library", items, count, err)
	}
	collections, err := s.Collections(1)
	if err != nil || len(collections) != 1 || collections[0] != "Allowed" {
		t.Fatal(collections, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = reopened
	policy, err = s.LibraryAccessPolicy()
	if err != nil || policy.EnableAllFolders || len(policy.LibraryIDs) != 1 || policy.LibraryIDs[0] != one.ID || s.CanAccessLibrary(two.ID) {
		t.Fatal("grants did not survive restart", policy, err)
	}
	if err := s.DeleteLibrary(one.ID); err != nil {
		t.Fatal(err)
	}
	policy, err = s.LibraryAccessPolicy()
	if err != nil || len(policy.LibraryIDs) != 0 || s.CanAccessLibrary(one.ID) {
		t.Fatal("deleted library retained a grant", policy, err)
	}
	if err := s.SaveLibraryAccessPolicy(LibraryAccessPolicy{EnableAllFolders: true}); err != nil || !s.CanAccessLibrary(two.ID) {
		t.Fatal("all-folders access was not restored", err)
	}
}

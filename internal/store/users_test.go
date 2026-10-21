package store

import (
	"path/filepath"
	"testing"
)

func TestUserLegacyGrantsMigrateOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	lib, err := s.AddLibrary("Legacy", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the account tables left by the single-user release.
	_, err = s.db.Exec(`DROP TABLE users; DROP TABLE user_policy; DROP TABLE user_library_grants;
UPDATE account_policy SET enable_all_folders=0 WHERE id=1;
INSERT INTO account_libraries(library_id) VALUES(?)`, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.migrateUsers(); err != nil {
		t.Fatal(err)
	}
	if !s.CanAccessLibrary(lib.ID) {
		t.Fatal("legacy grant was not migrated")
	}
	if err = s.SaveLibraryAccessPolicy(LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessLibrary(lib.ID) {
		t.Fatal("revocation failed")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.CanAccessLibrary(lib.ID) {
		t.Fatal("revoked legacy grant restored on restart")
	}
}

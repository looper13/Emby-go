package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupPreservesExistingAndExcludesItsOutput(t *testing.T) {
	db, lib, _ := fixture(t)
	out := filepath.Join(lib, "backups")
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	options := Options{DBPath: db, OutDir: out, Now: now, NFORoot: true, Retain: 1}
	first, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(options); err == nil {
		t.Fatal("duplicate timestamp replaced backup")
	}
	if err := checkSnapshot(first.Database); err != nil {
		t.Fatalf("previous backup damaged: %v", err)
	}
	// A timestamp alone does not give the retention policy ownership of a directory.
	foreign := filepath.Join(out, "20000101T000000Z")
	if err := os.MkdirAll(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "keep.nfo"), []byte("manual"), 0600); err != nil {
		t.Fatal(err)
	}
	options.Now = now.Add(time.Hour)
	second, err := Run(options)
	if err != nil {
		t.Fatal(err)
	}
	if second.NFOCount != 2 {
		t.Fatalf("recursively copied output: %+v", second)
	}
	if len(second.Pruned) != 1 || second.Pruned[0] != filepath.Base(first.Dir) {
		t.Fatalf("pruned=%v", second.Pruned)
	}
	if _, err := os.Stat(filepath.Join(foreign, "keep.nfo")); err != nil {
		t.Fatalf("foreign backup removed: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 2 {
		t.Fatalf("staging leftovers: %v %v", entries, err)
	}
}

func TestBackupSupportsLinkedLibraryRoot(t *testing.T) {
	db, lib, out := fixture(t)
	linked := filepath.Join(t.TempDir(), "linked-library")
	if err := os.Symlink(lib, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	connection, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec("UPDATE libraries SET path=?", linked); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	connection.Close()
	result, err := Run(Options{DBPath: db, OutDir: out, NFORoot: true})
	if err != nil || result.NFOCount != 2 {
		t.Fatalf("linked library: %+v %v", result, err)
	}
}

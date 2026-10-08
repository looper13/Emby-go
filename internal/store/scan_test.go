package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestScanFingerprintLifecycle(t *testing.T) {
	database, libraryID := newFeatureStore(t)
	movie := Movie{LibraryID: libraryID, SourcePath: "/tmp/av/a.strm", Status: "success", Title: "A"}
	upsertFixtureMovie(t, database, libraryID, movie)
	assertFingerprint := func(want string, exists bool) {
		t.Helper()
		fingerprints, err := database.ScanFingerprints(libraryID)
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, found := fingerprints[movie.SourcePath]
		if fingerprint != want || found != exists {
			t.Fatalf("fingerprints = %+v, want %q, exists=%v", fingerprints, want, exists)
		}
	}
	assertFingerprint("", true)
	if err := database.SaveScanFingerprint(libraryID, movie.SourcePath, "v1:first"); err != nil {
		t.Fatal(err)
	}
	assertFingerprint("v1:first", true)
	if err := database.SaveScanFingerprint(libraryID, movie.SourcePath, "v1:second"); err != nil {
		t.Fatal(err)
	}
	assertFingerprint("v1:second", true)
	if err := database.SaveScanFingerprint(libraryID+1, movie.SourcePath, "wrong-library"); err != nil {
		t.Fatal(err)
	}
	assertFingerprint("v1:second", true)
	upsertFixtureMovie(t, database, libraryID, movie)
	assertFingerprint("", true)
	if err := database.SaveScanFingerprint(libraryID, movie.SourcePath, "v1:restored"); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteMissingSources(libraryID, nil); err != nil {
		t.Fatal(err)
	}
	assertFingerprint("", false)
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM scan_fingerprints").Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan fingerprints = %d, %v", count, err)
	}
}

func TestOpenMigratesScanFingerprints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	library, err := database.AddLibrary("legacy", "/legacy")
	if err != nil {
		t.Fatal(err)
	}
	movie := Movie{LibraryID: library.ID, SourcePath: "/legacy/a.strm", Status: "success", Title: "Legacy"}
	id, err := database.UpsertMovie(movie, 0, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	<-database.featuresReady
	if _, err := database.db.Exec("DROP TABLE scan_fingerprints"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fingerprints, err := reopened.ScanFingerprints(library.ID)
	if err != nil || len(fingerprints) != 1 || fingerprints[movie.SourcePath] != "" {
		t.Fatalf("legacy migration = %+v, %v", fingerprints, err)
	}
	if restored, err := reopened.Movie(id); err != nil || restored.Title != movie.Title {
		t.Fatalf("migration damaged existing movie: %+v %v", restored, err)
	}
}

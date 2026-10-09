package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSourcePrefixFingerprintsEscapeLiteralNames(t *testing.T) {
	database, libraryID := newFeatureStore(t)
	directory := filepath.Clean("/tmp/av")
	for _, name := range []string{"a_b", "axb", "a%b", "azb", "bang!a", "unrelated"} {
		path := filepath.Join(directory, name+".strm")
		upsertFixtureMovie(t, database, libraryID, Movie{LibraryID: libraryID, SourcePath: path, OutputDir: directory, Status: "success"})
		if err := database.SaveScanFingerprint(libraryID, path, "v1:"+name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a_b", "a%b", "bang!a"} {
		fingerprints, err := database.SourcePrefixScanFingerprints(libraryID, directory, []string{filepath.Join(directory, name)})
		if err != nil || len(fingerprints) != 1 || fingerprints[filepath.Join(directory, name+".strm")] != "v1:"+name {
			t.Fatalf("literal prefix %q: %v %v", name, fingerprints, err)
		}
	}
}

func TestMoviesByIDsBatchesAndPreservesPendingMovies(t *testing.T) {
	database, libraryID := newFeatureStore(t)
	entries := make([]ScannedMovie, 505)
	for index := range entries {
		entries[index].Movie = Movie{LibraryID: libraryID, SourcePath: fmt.Sprintf("/tmp/av/batch-%d.strm", index), Status: "pending"}
	}
	if err := database.SaveScannedMovies(entries); err != nil {
		t.Fatal(err)
	}
	rows, err := database.db.Query("SELECT id FROM movies")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, ids[0], 9999999)
	movies, err := database.MoviesByIDs(ids)
	if err != nil || len(movies) != len(entries) {
		t.Fatalf("batched read: %d movies, %v", len(movies), err)
	}
	for _, movie := range movies {
		if movie.Status != "pending" {
			t.Fatal("pending movie was omitted or changed")
		}
	}
}

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

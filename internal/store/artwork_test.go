package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestArtworkMigrationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "art.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.waitFeaturesReady()
	lib, err := db.AddLibrary("Movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: "movie.strm", Status: "success", BackdropPath: "fanart.jpg"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-upgrade schema without the new column.
	if _, err := db.db.Exec("ALTER TABLE movies DROP COLUMN backdrop_paths"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.waitFeaturesReady()
	movie, err := db.Movie(id)
	if err != nil || !reflect.DeepEqual(movie.Backdrops(), []string{"fanart.jpg"}) {
		t.Fatalf("legacy fallback: %+v %v", movie, err)
	}
	movie.BackdropPaths = []string{"fanart.jpg", "extrafanart/fanart1.jpg", "extrafanart/fanart2.jpg"}
	if _, err := db.UpsertMovie(movie, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.waitFeaturesReady()
	saved, err := db.Movie(id)
	if err != nil || !reflect.DeepEqual(saved.Backdrops(), movie.BackdropPaths) {
		t.Fatalf("artwork lost after reopen: %+v %v", saved, err)
	}
}

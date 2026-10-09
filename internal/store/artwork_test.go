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

// TestTrailerAndCoverRoundTrip 预告片地址与远程封面随影片持久化：详情页要用它们播预告片、
// 并在本地没有图时用远程封面兜底。
func TestTrailerAndCoverRoundTrip(t *testing.T) {
	database, libraryID := newFeatureStore(t)
	movie := Movie{
		LibraryID: libraryID, SourcePath: "/tmp/av/HMN-145.strm", Status: "success", Title: "HMN-145 标题",
		TrailerURL: "https://cc3001.dmm.co.jp/pv/hmn00145_mhb_w.mp4",
		CoverURL:   "https://awsimgsrc.dmm.com/dig/mono/movie/hmn145/hmn145pl.jpg",
	}
	id := upsertFixtureMovie(t, database, libraryID, movie)
	read, err := database.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	if read.TrailerURL != movie.TrailerURL || read.CoverURL != movie.CoverURL {
		t.Fatalf("往返后 TrailerURL=%q CoverURL=%q，期望 %q / %q", read.TrailerURL, read.CoverURL, movie.TrailerURL, movie.CoverURL)
	}
	// 覆盖更新：预告片换地址后必须跟着变，不能被旧的 UPSERT 列写丢。
	movie.TrailerURL, movie.CoverURL = "https://cdn.test/new.mp4", ""
	upsertFixtureMovie(t, database, libraryID, movie)
	read, err = database.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	if read.TrailerURL != "https://cdn.test/new.mp4" || read.CoverURL != "" {
		t.Fatalf("更新后 TrailerURL=%q CoverURL=%q", read.TrailerURL, read.CoverURL)
	}
}

package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestItemFavoritesUpgradePersistenceAndMediaIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	library, err := s.AddLibrary("Library", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.UpsertMovie(Movie{LibraryID: library.ID, SourcePath: "movie.strm", Status: "success", Title: "Film"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetFavorite(id, true); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePlayback(id, 1234, 2, "2026-10-19T00:00:00Z", 1234); err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	if _, err = s.db.Exec("DROP TABLE item_favorites"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.ItemFavoriteVersion()
	for _, name := range []string{"person:Alice", "genre:Drama"} {
		if err = s.SetItemFavorite(name, true); err != nil {
			t.Fatal(err)
		}
		if err = s.SetItemFavorite(name, true); err != nil {
			t.Fatal(err)
		}
	}
	if s.ItemFavoriteVersion() == before || len(s.ItemFavoriteIDs()) != 2 {
		t.Fatal("favorite version or idempotency")
	}
	s.waitFeaturesReady()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.ItemFavorite("person:Alice") || !s.ItemFavorite("genre:Drama") {
		t.Fatal("favorites lost after reopen")
	}
	if err = s.SetItemFavorite("person:Alice", false); err != nil {
		t.Fatal(err)
	}
	if s.ItemFavorite("person:Alice") {
		t.Fatal("unfavorite failed")
	}
	d, err := s.Data(id)
	if err != nil || !d.IsFavorite || d.PositionTicks != 1234 || d.PlayCount != 2 {
		t.Fatal("existing media state changed", d, err)
	}
	no := false
	rows, total, err := s.SearchScoped(0, "", "", "", "", "", "", "", false, &no, "title", false, 1, 0)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal(rows, total, err)
	}
}

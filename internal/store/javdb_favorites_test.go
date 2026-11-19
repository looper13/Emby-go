package store

import (
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/r18"
)

func TestJavdbListFavoritesArePersistentAndUserScoped(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "favorites.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.InitializeAdministrator("admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	library, err := s.CreateR18Library("JavDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.AddLibrary("Local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	movieID, err := s.UpsertMovie(Movie{LibraryID: local.ID, SourcePath: filepath.Join(local.Path, "movie.strm"), Status: "success", Number: "TEST-001"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	count := 24
	item := JavdbListFavorite{LibraryID: library.ID, MovieID: movieID, ListID: "list1", RelatedPage: 2, Name: "精选清单", MovieCount: &count}
	if err := s.SaveJavdbListFavorite(item); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.CreateUser("list-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if s.ForUser(viewer.ID).IsJavdbListFavorite(library.ID, "list1") {
		t.Fatal("收藏泄露给其他用户")
	}
	items, total, err := s.JavdbListFavorites("精选", 0, 48)
	if err != nil || total != 1 || len(items) != 1 || items[0].MovieCount == nil || *items[0].MovieCount != count {
		t.Fatal("收藏记录查询失败", items, total, err)
	}
	if err := s.SaveJavdbListFavorite(JavdbListFavorite{LibraryID: library.ID, MovieID: movieID, ListID: "list1", RelatedPage: 3, Name: "精选清单更新"}); err != nil {
		t.Fatal(err)
	}
	items, total, err = s.JavdbListFavorites("", 0, 48)
	if err != nil || total != 1 || len(items) != 1 || items[0].RelatedPage != 3 || items[0].Name != "精选清单更新" || items[0].MovieCount != nil {
		t.Fatal("重复收藏未更新单条引用", items, total, err)
	}
	if err := s.ForUser(viewer.ID).SaveJavdbListFavorite(item); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(viewer.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := s.db.QueryRow("SELECT count(*) FROM javdb_list_favorites WHERE user_id=?", viewer.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("删除用户未清除清单收藏", remaining, err)
	}
}

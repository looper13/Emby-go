package scraper

import (
	"os"
	"path/filepath"
	"testing"

	"emby-go/internal/store"
)

func TestActorMovieAcceptsSecondaryLibraryRootAndRejectsOutside(t *testing.T) {
	db, _, _ := actorFixture(t, `<actor><name>Alice</name></actor>`)
	library, err := db.Library(1)
	if err != nil {
		t.Fatal(err)
	}
	second := t.TempDir()
	library, err = db.UpdateDirectoryLibrary(library.ID, library.Name, append(library.DirectoryPaths(), second), library.ProxyEnabled, library.ResolveRedirects, library.ProxyHeaders, library.Revision)
	if err != nil {
		t.Fatal(err)
	}
	movie := store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(second, "movie.strm"), NFOPath: filepath.Join(second, "movie.nfo")}
	for _, file := range []string{movie.SourcePath, movie.NFOPath} {
		if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scraper := &Scraper{store: db}
	if err := scraper.validateActorMovie(movie); err != nil {
		t.Fatalf("次目录影片应允许读取演员信息: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "private.nfo")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	movie.NFOPath = outside
	if err := scraper.validateActorMovie(movie); err == nil {
		t.Fatal("不应读取媒体库外的演员 NFO")
	}
	t.Run("符号链接越界", func(t *testing.T) {
		link := filepath.Join(second, "link.nfo")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("环境无法创建符号链接: %v", err)
		}
		movie.NFOPath = link
		if err := scraper.validateActorMovie(movie); err == nil {
			t.Fatal("符号链接不应越过媒体库根目录")
		}
	})
}

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"emby-go/internal/store"
)

func TestMultipleRootsScanAndPartialRefreshKeepOtherIndexes(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	database := newStore(t, first)
	library, err := database.AddDirectoryLibrary("多目录", []string{first, second}, store.LibraryMovies, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, first, "a.strm", "https://media.test/a.mp4")
	writeScanFile(t, first, "a.nfo", "<movie><title>A</title></movie>")
	writeScanFile(t, second, "b.strm", "https://media.test/b.mp4")
	writeScanFile(t, second, "b.nfo", "<movie><title>B</title></movie>")
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	requireScan(t, database, library, Result{Skipped: 2})
	bID, err := database.MovieIDByPath(filepath.Join(second, "b.strm"))
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, second, "b.nfo", "<movie><title>修改B</title></movie>")
	result, err := RefreshFiles(context.Background(), database, library, []string{filepath.Join(second, "b.nfo")}, nil)
	if err != nil || result.Updated != 1 || result.Deleted != 0 {
		t.Fatalf("次目录文件刷新失败: %+v, %v", result, err)
	}
	if movie, err := database.Movie(bID); err != nil || movie.Title != "修改B" {
		t.Fatalf("次目录元数据未更新: %+v, %v", movie, err)
	}
	if err = os.Remove(filepath.Join(first, "a.strm")); err != nil {
		t.Fatal(err)
	}
	result, err = RefreshDirectory(context.Background(), database, library, first, true, nil)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("首目录清理失败: %+v, %v", result, err)
	}
	if _, err = database.Movie(bID); err != nil {
		t.Fatalf("首目录刷新误删次目录索引: %v", err)
	}
}

func TestMultipleRootsUnavailableRootKeepsIndexesAndScansAvailableRoot(t *testing.T) {
	parent := t.TempDir()
	first, second := filepath.Join(parent, "first"), filepath.Join(parent, "second")
	writeScanFile(t, first, "a.strm", "https://media.test/a.mp4")
	writeScanFile(t, first, "a.nfo", "<movie><title>A</title></movie>")
	writeScanFile(t, second, "b.strm", "https://media.test/b.mp4")
	writeScanFile(t, second, "b.nfo", "<movie><title>B</title></movie>")
	database := newStore(t, parent)
	library, err := database.AddDirectoryLibrary("离线盘", []string{first, second}, store.LibraryMovies, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	aID, err := database.MovieIDByPath(filepath.Join(first, "a.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(first, filepath.Join(parent, "offline")); err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, second, "b.nfo", "<movie><title>可用盘更新</title></movie>")
	result, err := Scan(context.Background(), database, library)
	if err == nil || result.Updated != 1 || result.Deleted != 0 {
		t.Fatalf("离线盘阻止可用盘扫描或误删索引: %+v, %v", result, err)
	}
	if movie, err := database.Movie(aID); err != nil || movie.Title != "A" {
		t.Fatalf("离线盘索引丢失: %+v, %v", movie, err)
	}
	if movies := visible(t, database); len(movies) != 2 {
		t.Fatalf("多目录索引数量不正确: %+v", movies)
	}
}

func TestMultipleRootsNestedDirectoriesTraverseOnceAndRespectTVBoundary(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "nested")
	writeScanFile(t, parent, "tvshow.nfo", "<tvshow><title>父目录节目</title></tvshow>")
	writeScanFile(t, child, "Episode.S01E01.strm", "https://media.test/episode.mp4")
	writeScanFile(t, child, "Episode.S01E01.nfo", "<episodedetails><title>单集</title></episodedetails>")
	database := newStore(t, parent)
	library, err := database.AddDirectoryLibrary("嵌套根", []string{parent, child}, store.LibraryTVShows, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	episodes, err := database.TVEpisodes(library.ID, "")
	if err != nil || len(episodes) != 1 || episodes[0].Television.SeriesPath != child || episodes[0].Television.Title == "父目录节目" {
		t.Fatalf("次根目录节目读取了根外元数据: %+v, %v", episodes, err)
	}
}

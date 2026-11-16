package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDirectoryLibraryPathsPersistAndLegacyRegistrationPreservesSettings(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "paths.db")
	s, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	first, second := t.TempDir(), t.TempDir()
	paths := []string{first, second}
	lib, err := s.AddDirectoryLibrary("多目录", append(paths, first+string(filepath.Separator)+"."), LibraryMovies, true, true, map[string]string{"referer": "https://player.example/", "Origin": "https://player.example"})
	if err != nil || lib.Path != first || !reflect.DeepEqual(lib.Paths, paths) || lib.ProxyHeaders["Referer"] != "https://player.example/" {
		t.Fatalf("创建配置错误: %+v, %v", lib, err)
	}
	lib, err = s.AddLibrary("重注册", second)
	if err != nil || !reflect.DeepEqual(lib.Paths, paths) || lib.Path != first || !lib.ProxyEnabled || !lib.ResolveRedirects || len(lib.ProxyHeaders) != 2 {
		t.Fatalf("旧入口覆盖多目录设置: %+v, %v", lib, err)
	}
	lib, err = s.AddLibraryWithPlaybackSettings("旧播放设置", first, false, false)
	if err != nil || !reflect.DeepEqual(lib.Paths, paths) || len(lib.ProxyHeaders) != 2 {
		t.Fatalf("旧播放入口覆盖目录或请求头: %+v, %v", lib, err)
	}
	expected := lib
	s.waitFeaturesReady()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	lib, err = s.Library(expected.ID)
	if err != nil || !reflect.DeepEqual(lib, expected) {
		t.Fatalf("重启后配置改变: %+v, %v", lib, err)
	}
	if err = s.migrateLibraryPaths(); err != nil {
		t.Fatalf("迁移重复执行失败: %v", err)
	}
	lib, err = s.Library(expected.ID)
	if err != nil || !reflect.DeepEqual(lib, expected) {
		t.Fatalf("重复迁移覆盖配置: %+v, %v", lib, err)
	}
}

func TestDirectoryLibraryLegacyMigrationPopulatesPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE libraries(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,path TEXT UNIQUE NOT NULL,type TEXT DEFAULT 'movies',enabled INTEGER DEFAULT 1);
INSERT INTO libraries VALUES(7,'历史目录','/original','movies',1);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	lib, err := s.Library(7)
	if err != nil || lib.Path != "/original" || !reflect.DeepEqual(lib.Paths, []string{"/original"}) || len(lib.ProxyHeaders) != 0 {
		t.Fatalf("历史单路径迁移错误: %+v, %v", lib, err)
	}
}

func TestDirectoryLibraryRootsChooseLongestAndTraverseOnce(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "nested")
	other := t.TempDir()
	lib := Library{Path: parent, Paths: []string{parent, child, other}}
	if root := lib.RootForPath(filepath.Join(child, "movie.strm")); root != child {
		t.Fatalf("应选择最长根: %q", root)
	}
	if root := lib.RootForPath(parent + "-sibling" + string(filepath.Separator) + "movie.strm"); root != "" {
		t.Fatalf("相邻前缀目录被错误包含: %q", root)
	}
	if !reflect.DeepEqual(lib.TraversalPaths(), []string{parent, other}) {
		t.Fatalf("嵌套根被重复遍历: %v", lib.TraversalPaths())
	}
	if !reflect.DeepEqual((Library{Path: parent}).DirectoryPaths(), []string{parent}) {
		t.Fatal("历史 Path 构造方式不可用")
	}
	if roots := (Library{Path: "r18://fixture", Paths: []string{parent}, SourceType: SourceR18}).DirectoryPaths(); len(roots) != 0 {
		t.Fatalf("远端库不应提供目录能力: %v", roots)
	}
}

func TestDirectoryLibraryUpdateRemovesOnlyUncoveredIndexesAndRollsBack(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	first, second := t.TempDir(), t.TempDir()
	lib, err := s.AddDirectoryLibrary("原库", []string{first, second}, LibraryMovies, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, 2)
	for _, root := range []string{first, second} {
		source := filepath.Join(root, "movie.strm")
		if err := os.WriteFile(source, []byte("https://media.example/movie.mp4"), 0644); err != nil {
			t.Fatal(err)
		}
		id, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: source, OutputDir: root, Status: "success", Title: root}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if err = s.SavePlayback(id, 123, 1, "", 123); err != nil {
			t.Fatal(err)
		}
		if err = s.SaveMovieDeletionState(id, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	version := s.Version("g:version")
	retainedFavorite, removedFavorite := TVSeriesID(lib.ID, first), TVSeriesID(lib.ID, second)
	for _, itemID := range []string{retainedFavorite, removedFavorite} {
		if err = s.SetItemFavorite(itemID, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.UpdateDirectoryLibrary(lib.ID, "过期", []string{first}, false, false, nil, lib.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("过期修订未被拒绝: %v", err)
	}
	if _, err = s.db.Exec(fmt.Sprintf(`CREATE TRIGGER fixture_reject_removal BEFORE DELETE ON movies WHEN OLD.id=%d BEGIN SELECT RAISE(ABORT,'fixture rollback'); END`, ids[1])); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateDirectoryLibrary(lib.ID, "失败更新", []string{first}, true, true, map[string]string{"Origin": "https://player.example"}, lib.Revision); err == nil {
		t.Fatal("删除索引失败时必须回滚配置")
	}
	unchanged, err := s.Library(lib.ID)
	if err != nil || !reflect.DeepEqual(unchanged, lib) || s.Version("g:version") != version || !s.ItemFavorite(removedFavorite) {
		t.Fatalf("失败事务改变配置或版本: %+v, %v", unchanged, err)
	}
	if _, err = s.Movie(ids[1]); err != nil {
		t.Fatalf("失败事务丢失影片: %v", err)
	}
	if _, err = s.db.Exec("DROP TRIGGER fixture_reject_removal"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateDirectoryLibrary(lib.ID, "新库", []string{first}, true, true, map[string]string{"Origin": "https://player.example"}, lib.Revision)
	if err != nil || updated.Revision != lib.Revision+1 {
		t.Fatalf("更新失败: %+v, %v", updated, err)
	}
	if movie, err := s.Movie(ids[0]); err != nil || movie.LibraryID != lib.ID {
		t.Fatalf("保留根的索引丢失: %+v, %v", movie, err)
	}
	if _, err = s.Movie(ids[1]); !NotFound(err) {
		t.Fatalf("移除根的索引仍存在: %v", err)
	}
	for _, table := range []string{"userdata", "movie_deletions", "scan_fingerprints", "movie_features"} {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE movie_id=?", ids[1]).Scan(&count); err != nil || count != 0 {
			t.Fatalf("索引关联记录未清理: %s count=%d, %v", table, count, err)
		}
	}
	if _, err = os.Stat(filepath.Join(second, "movie.strm")); err != nil {
		t.Fatalf("移除根不应删除磁盘文件: %v", err)
	}
	if s.ItemFavorite(removedFavorite) || !s.ItemFavorite(retainedFavorite) {
		t.Fatal("虚拟节目收藏应按保留目录范围清理")
	}
}

func TestDirectoryLibraryRejectsCrossLibraryOverlap(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "overlap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.waitFeaturesReady(); s.Close() }()
	root := t.TempDir()
	lib, err := s.AddDirectoryLibrary("已有库", []string{root}, LibraryMovies, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, filepath.Join(root, "nested"), filepath.Dir(root)} {
		if _, err := s.AddDirectoryLibrary("冲突库", []string{path}, LibraryMovies, false, false, nil); err == nil || !strings.Contains(err.Error(), "重叠") {
			t.Fatalf("跨库重叠应被拒绝: %q, %v", path, err)
		}
	}
	other := t.TempDir()
	if _, err := s.AddDirectoryLibrary("独立库", []string{other}, LibraryMovies, false, false, nil); err != nil {
		t.Fatalf("独立目录被错误拒绝: %v", err)
	}
	if _, err := s.UpdateDirectoryLibrary(lib.ID, lib.Name, []string{root, filepath.Join(other, "nested")}, false, false, nil, lib.Revision); err == nil {
		t.Fatal("编辑时未检查其他库的目录")
	}
}

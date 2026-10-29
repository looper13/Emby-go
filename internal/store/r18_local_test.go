package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func r18LocalStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "r18-local.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func r18LocalLibrary(t *testing.T, s *Store, name string) Library {
	t.Helper()
	library, err := s.AddLibrary(name, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func r18LocalMovie(t *testing.T, s *Store, library Library, name, number, status string, television *Television) int64 {
	t.Helper()
	movie := Movie{LibraryID: library.ID, SourcePath: filepath.Join(library.Path, name+".strm"),
		Number: number, Status: status, Title: "影片标题", Television: television}
	id, err := s.UpsertMovie(movie, 0, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func r18LocalIDs(t *testing.T, s *Store, numbers []string) []int64 {
	t.Helper()
	movies, err := s.LocalSTRMMovies(context.Background(), numbers)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(movies))
	for _, movie := range movies {
		ids = append(ids, movie.ID)
	}
	return ids
}

func TestR18LocalSTRMMoviesAuthorizationAndLibraryState(t *testing.T) {
	s := r18LocalStore(t)
	a, b, external := r18LocalLibrary(t, s, "A"), r18LocalLibrary(t, s, "B"), r18LocalLibrary(t, s, "非目录来源")
	first := r18LocalMovie(t, s, a, "first", "TEST-001", "success", nil)
	second := r18LocalMovie(t, s, b, "second", "TEST001", "manual", nil)
	r18LocalMovie(t, s, external, "external", "TEST-001", "success", nil)
	// 仅变更临时夹具，验证即便存在遗留影片行也不会读取非目录来源。
	if _, err := s.db.Exec("UPDATE libraries SET source_type=? WHERE id=?", SourceR18, external.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.InitializeAdministrator("local-admin", "long-fixture-password"); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.CreateUser("local-viewer", "long-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	user := s.ForUser(viewer.ID)
	query := []string{"test_001"}
	assertIDs := func(want []int64) {
		t.Helper()
		if got := r18LocalIDs(t, user, query); !reflect.DeepEqual(got, want) {
			t.Fatalf("本地影片授权或启用状态错误：got=%v want=%v", got, want)
		}
	}
	assertIDs([]int64{})
	if err := user.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{a.ID}}); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{first})
	if err := user.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{a.ID, b.ID}}); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{first, second})
	if _, err := s.db.Exec("UPDATE libraries SET enabled=0 WHERE id=?", b.ID); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{first})
	if _, err := s.db.Exec("UPDATE libraries SET enabled=0 WHERE id=?", a.ID); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{})
	if _, err := s.db.Exec("UPDATE libraries SET enabled=1 WHERE id IN (?,?)", a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{first, second})
	if err := user.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{b.ID}}); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{second})
	if got := r18LocalIDs(t, s, query); !reflect.DeepEqual(got, []int64{first, second}) {
		t.Fatal("用户查询改变了基础 Store 身份", got)
	}
	if err := user.SaveLibraryAccessPolicy(LibraryAccessPolicy{EnableAllFolders: true}); err != nil {
		t.Fatal(err)
	}
	assertIDs([]int64{first, second})
}

func TestR18LocalSTRMMoviesConservativeNumbersAndVisibility(t *testing.T) {
	s := r18LocalStore(t)
	library := r18LocalLibrary(t, s, "番号匹配")
	first := r18LocalMovie(t, s, library, "a", " \tabf._-001 \r\n", "success", nil)
	second := r18LocalMovie(t, s, library, "b", "ABF001", "manual", nil)
	suffix := r18LocalMovie(t, s, library, "suffix", "ABF-001-C", "success", nil)
	for i, entry := range []struct {
		number, status string
		tv             *Television
	}{
		{"ABF-0001", "success", nil}, {"ABF-01", "success", nil}, {"ABF-0012", "success", nil},
		{"ABF-001", "pending", nil}, {"ABF-001", "incompatible", nil}, {"ABF-001", "failed", nil},
		{"ABF-001", "success", &Television{SeriesPath: "show", Season: 1, Episode: 1}},
		{"OTHER-001", "success", nil}, {"", "success", nil}, {" -_.\t\r\n", "success", nil},
	} {
		id := r18LocalMovie(t, s, library, fmt.Sprintf("ABF-001-title-%d", i), entry.number, entry.status, entry.tv)
		if _, err := s.db.Exec("UPDATE movies SET title=?,original_title=? WHERE id=?", "ABF-001", "ABF001", id); err != nil {
			t.Fatal(err)
		}
	}
	if got := r18LocalIDs(t, s, []string{" abf_001 ", "ABF.001", "", "-_ .", "' OR 1=1 --"}); !reflect.DeepEqual(got, []int64{first, second}) {
		t.Fatal("番号匹配忽略了前导零、版本后缀或影片可见性边界，或错误猜测标题", got)
	}
	if got := r18LocalIDs(t, s, []string{"abf001c"}); !reflect.DeepEqual(got, []int64{suffix}) {
		t.Fatal("完整版本后缀不能独立匹配", got)
	}
}

func TestR18LocalSTRMMoviesBatchOrderAndCompleteMetadata(t *testing.T) {
	s := r18LocalStore(t)
	a, b := r18LocalLibrary(t, s, "A"), r18LocalLibrary(t, s, "B")
	last := r18LocalMovie(t, s, b, "z", "THIRD-003", "success", nil)
	second := r18LocalMovie(t, s, a, "z", "SECOND-002", "success", nil)
	movie := Movie{LibraryID: a.ID, SourcePath: filepath.Join(a.Path, "a.strm"), Status: "manual", Number: "FIRST-001",
		SourceProtocol: "https", SourceContainer: "mkv", NFOPath: "fixture.nfo", OutputDir: a.Path, Title: "完整标题",
		OriginalTitle: "原名", Plot: "简介", Year: 2025, Premiere: "2025-01-01", Rating: 4.19, Director: "导演",
		Series: "系列", Maker: "制作方", Label: "发行方", Collection: "合集", Genres: []string{"类型"}, Tags: []string{"标签"},
		Studios: []string{"厂牌"}, Taglines: []string{"标语"}, OfficialRating: "R18", SortName: "排序名", ProviderID: "fixture-provider",
		PosterPath: "poster.jpg", BackdropPath: "backdrop.jpg", BackdropPaths: []string{"backdrop.jpg", "backdrop2.jpg"},
		LandscapePath: "landscape.jpg", TrailerURL: "https://fixture.invalid/trailer.mp4", CoverURL: "https://fixture.invalid/cover.jpg",
		RuntimeSeconds: 7200, AdditionalParts: []string{filepath.Join(a.Path, "a-CD2.strm")}}
	first, err := s.UpsertMovie(movie, 123, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE movies SET last_scrape_at=?,last_scrape_error=? WHERE id=?", "2025-01-02", "fixture-note", first); err != nil {
		t.Fatal(err)
	}
	got, err := s.LocalSTRMMovies(context.Background(), []string{"THIRD003", "SECOND002", "FIRST001", "FIRST-001"})
	if err != nil || len(got) != 3 || got[0].ID != first || got[1].ID != second || got[2].ID != last {
		t.Fatal("批量查询未按媒体库、路径、ID稳定排序或重复返回影片", got, err)
	}
	want, err := s.Movie(first)
	if err != nil || !reflect.DeepEqual(got[0], want) {
		t.Fatal("批量查询没有返回完整 Movie 元数据", err)
	}
	if _, err := s.db.Exec("UPDATE movies SET number=? WHERE id=?", "CHANGED-001", first); err != nil {
		t.Fatal(err)
	}
	if got := r18LocalIDs(t, s, []string{"FIRST-001"}); len(got) != 0 {
		t.Fatal("本地匹配缓存了已经变化的番号", got)
	}
}

func TestR18LocalSTRMMoviesEmptyCancellationAndInputLimit(t *testing.T) {
	s := r18LocalStore(t)
	library := r18LocalLibrary(t, s, "查询限制")
	id := r18LocalMovie(t, s, library, "movie", "TEST-001", "success", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.LocalSTRMMovies(ctx, []string{"TEST-001"}); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("本地匹配未响应取消", got, err)
	}
	numbers := make([]string, r18LocalNumberLimit)
	for i := range numbers {
		numbers[i] = fmt.Sprintf("QUERY-%04d", i)
	}
	numbers[len(numbers)-1] = "TEST-001"
	if got := r18LocalIDs(t, s, numbers); !reflect.DeepEqual(got, []int64{id}) {
		t.Fatal("1000 个番号的批量查询没有正确匹配", got)
	}
	numbers = append(numbers, "TEST-001")
	if got, err := s.LocalSTRMMovies(context.Background(), numbers); err == nil || got != nil || !strings.Contains(err.Error(), "1000") {
		t.Fatal("番号超限未明确报错", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// 关闭临时数据库后仍应成功，证明空列表和全部空番号不会发起 SQL 查询。
	for _, numbers := range [][]string{nil, {}, {"", " -_.\t\r\n"}} {
		got, err := s.LocalSTRMMovies(context.Background(), numbers)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatal("空番号查询访问了数据库或未返回空列表", got, err)
		}
	}
}

func TestR18LocalSTRMMoviesCandidateLimit(t *testing.T) {
	s := r18LocalStore(t)
	library := r18LocalLibrary(t, s, "候选限制")
	batch := make([]ScannedMovie, r18LocalMovieLimit)
	for i := range batch {
		batch[i] = ScannedMovie{Movie: Movie{LibraryID: library.ID, SourcePath: filepath.Join(library.Path, fmt.Sprintf("%04d.strm", i)),
			Status: "success", Number: "TEST-001", Title: "同番号版本"}, ModTime: time.Unix(100, 0)}
	}
	if err := s.SaveScannedMovies(batch); err != nil {
		t.Fatal(err)
	}
	got, err := s.LocalSTRMMovies(context.Background(), []string{"TEST-001"})
	if err != nil || len(got) != r18LocalMovieLimit {
		t.Fatal("2000 个候选不应被拒绝", len(got), err)
	}
	r18LocalMovie(t, s, library, "overflow", "TEST-001", "success", nil)
	got, err = s.LocalSTRMMovies(context.Background(), []string{"TEST-001"})
	if err == nil || got != nil || !strings.Contains(err.Error(), "2000") {
		t.Fatal("候选超限被静默截断或返回了不完整匹配", len(got), err)
	}
}

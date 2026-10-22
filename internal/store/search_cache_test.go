package store

import (
	"fmt"
	"testing"
)

func searchCacheSize(s *Store) int {
	count := 0
	if s.searchCounts.values != nil {
		s.searchCounts.values.DeleteMatching(func(string) bool { count++; return false })
	}
	return count
}

func TestSearchCountsReusePagesAndFollowMetadata(t *testing.T) {
	s, libID := newFeatureStore(t)
	for i := 0; i < 3; i++ {
		upsertFixtureMovie(t, s, libID, Movie{LibraryID: libID, SourcePath: fmt.Sprintf("/tmp/av/count-%d.strm", i), Title: fmt.Sprintf("Movie %d", i), Status: "success", Genres: []string{"Drama"}, Tags: []string{"Tag"}, Studios: []string{"Studio"}})
	}
	q := AdminQuery{LibraryID: libID, Genre: "Drama", Tag: "Tag", Studio: "Studio", Limit: 1}
	for i := 0; i < 20; i++ {
		q.Offset, q.SortBy, q.Desc = i%3, []string{"title", "datecreated", "year", "random"}[i%4], i%2 == 0
		q.RandomSeed = uint32(i + 1)
		movies, total, err := s.SearchAdmin(q)
		if err != nil || len(movies) != 1 || total != 3 {
			t.Fatalf("page=%d movies=%d total=%d err=%v", i, len(movies), total, err)
		}
	}
	if size := searchCacheSize(s); size != 1 {
		t.Fatalf("page/sort changes created %d count entries, want 1", size)
	}
	q.Tag = "missing"
	if _, total, err := s.SearchAdmin(q); err != nil || total != 0 {
		t.Fatalf("empty filter total=%d err=%v", total, err)
	}
	entry := ScannedMovie{Movie: Movie{LibraryID: libID, SourcePath: "/tmp/av/new-count.strm", Status: "success", Genres: []string{"Drama"}, Tags: []string{"missing"}, Studios: []string{"Studio"}}}
	if err := s.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.SearchAdmin(q); err != nil || total != 1 {
		t.Fatalf("insert retained stale zero count: total=%d err=%v", total, err)
	}
	if _, err := s.DeleteScannedSources(libID, []string{entry.Movie.SourcePath}); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.SearchAdmin(q); err != nil || total != 0 {
		t.Fatalf("deletion retained count: total=%d err=%v", total, err)
	}
}

func TestSearchCountsUserFiltersAndAccess(t *testing.T) {
	s, libID := newFeatureStore(t)
	if err := s.InitializeAdministrator("Count admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := upsertFixtureMovie(t, s, libID, Movie{LibraryID: libID, SourcePath: "/tmp/av/user-count.strm", Status: "success"})
	upsertFixtureMovie(t, s, other.ID, Movie{LibraryID: other.ID, SourcePath: "/tmp/other-count.strm", Status: "success"})
	user, err := s.CreateUser("Count reader", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	reader := s.ForUser(user.ID)
	if err := reader.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{libID}}); err != nil {
		t.Fatal(err)
	}
	check := func(view *Store, favorite *bool, want int) {
		t.Helper()
		if _, total, err := view.SearchAdmin(AdminQuery{Limit: 10, Favorite: favorite}); err != nil || total != want {
			t.Fatalf("user=%d total=%d want=%d err=%v", view.UserID(), total, want, err)
		}
	}
	favorite := true
	check(s, nil, 2)
	check(reader, nil, 1)
	if _, total, err := reader.SearchAdmin(AdminQuery{LibraryID: other.ID, Limit: 10}); err != nil || total != 0 {
		t.Fatalf("hidden library count=%d err=%v", total, err)
	}
	check(s, &favorite, 0)
	check(reader, &favorite, 0)
	if err := reader.SetFavorite(id, true); err != nil {
		t.Fatal(err)
	}
	check(reader, &favorite, 1)
	check(s, &favorite, 0)
	before := searchCacheSize(s)
	if err := reader.SavePlayback(id, 123, 1, "", 0); err != nil {
		t.Fatal(err)
	}
	check(reader, &favorite, 1)
	check(reader, nil, 1)
	if searchCacheSize(s) != before {
		t.Fatal("playback heartbeat invalidated list counts")
	}
	readUnplayed := func(want int) {
		t.Helper()
		_, total, err := reader.SearchScoped(0, "", "", "", "", "", "", "", true, nil, "title", false, 10, 0)
		if err != nil || total != want {
			t.Fatalf("unplayed=%d want=%d err=%v", total, want, err)
		}
	}
	readUnplayed(1)
	if err := reader.SetPlayed(id, true); err != nil {
		t.Fatal(err)
	}
	readUnplayed(0)
	if err := reader.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{other.ID}}); err != nil {
		t.Fatal(err)
	}
	if movies, total, err := reader.SearchAdmin(AdminQuery{LibraryID: libID, Limit: 10}); err != nil || total != 0 || len(movies) != 0 {
		t.Fatalf("revoked library movies=%d count=%d err=%v", len(movies), total, err)
	}
	check(reader, &favorite, 0)
	if err := reader.SaveLibraryAccessPolicy(LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	check(reader, nil, 0)
}

func TestSearchCountHitAvoidsDatabaseAndErrorsAreRetried(t *testing.T) {
	s, libID := newFeatureStore(t)
	upsertFixtureMovie(t, s, libID, Movie{LibraryID: libID, SourcePath: "/tmp/av/hot-count.strm", Status: "success"})
	clause := " WHERE status='success'"
	if _, err := s.cachedSearchCount(" WHERE broken_column=1", nil, false); err == nil {
		t.Fatal("invalid count accepted")
	}
	if searchCacheSize(s) != 0 {
		t.Fatal("failed query cached")
	}
	if total, err := s.cachedSearchCount(clause, nil, false); err != nil || total != 1 {
		t.Fatalf("cold count=%d err=%v", total, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if total, err := s.cachedSearchCount(clause, nil, false); err != nil || total != 1 {
		t.Fatalf("cache hit touched database: count=%d err=%v", total, err)
	}
	s.TouchMovie(1)
	if _, err := s.cachedSearchCount(clause, nil, false); err == nil {
		t.Fatal("metadata change reused old count")
	}
}

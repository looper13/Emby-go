package store

import (
	"fmt"
	"reflect"
	"testing"
)

func movieIDs(movies []Movie) []int64 {
	ids := make([]int64, len(movies))
	for i, movie := range movies {
		ids[i] = movie.ID
	}
	return ids
}

func TestAdminRandomSortStablePagination(t *testing.T) {
	s, libraryID := newFeatureStore(t)
	for i := 0; i < 215; i++ {
		upsertFixtureMovie(t, s, libraryID, Movie{LibraryID: libraryID, SourcePath: fmt.Sprintf("/tmp/av/%03d.strm", i), Title: fmt.Sprintf("Movie %03d", i), Status: "success", Tags: []string{"keep"}})
	}
	upsertFixtureMovie(t, s, libraryID, Movie{LibraryID: libraryID, SourcePath: "/tmp/av/hidden.strm", Title: "hidden", Status: "incompatible", Tags: []string{"keep"}})
	query := AdminQuery{LibraryID: libraryID, Tag: "keep", SortBy: "random", RandomSeed: 12345, Limit: 1000}
	read := func(q AdminQuery) []int64 {
		t.Helper()
		movies, total, err := s.SearchAdmin(q)
		if err != nil {
			t.Fatal(err)
		}
		if total != 215 {
			t.Fatalf("total=%d, want 215", total)
		}
		return movieIDs(movies)
	}
	all := read(query)
	if !reflect.DeepEqual(all, read(query)) {
		t.Fatal("same seed changed order")
	}
	var paged []int64
	for offset := 0; offset < 215; offset += 73 {
		page := query
		page.Offset = offset
		page.Limit = 73
		paged = append(paged, read(page)...)
	}
	if !reflect.DeepEqual(paged, all) {
		t.Fatal("pages differ from full result: duplicates or omissions")
	}
	seen := map[int64]bool{}
	for _, id := range all {
		if seen[id] {
			t.Fatalf("duplicate %d", id)
		}
		seen[id] = true
	}
	query.RandomSeed = 98765
	if reflect.DeepEqual(all, read(query)) {
		t.Fatal("different seed did not reshuffle")
	}
	query.RandomSeed = 0
	if !reflect.DeepEqual(read(query), read(query)) {
		t.Fatal("missing seed is not stable")
	}
	query.SortBy = "title"
	if reflect.DeepEqual(all, read(query)) {
		t.Fatal("random still uses title order")
	}
}

func TestAdminPremiereSort(t *testing.T) {
	s, libraryID := newFeatureStore(t)
	for i, date := range []string{"", "2025-01-02", "2026-01-01", "2025-01-02", "2024-12-31", ""} {
		upsertFixtureMovie(t, s, libraryID, Movie{LibraryID: libraryID, SourcePath: fmt.Sprintf("/tmp/av/%d.strm", i), Title: fmt.Sprintf("Movie %d", i), Status: "success", Premiere: date})
	}
	for _, test := range []struct {
		desc bool
		want []int64
	}{
		{true, []int64{3, 2, 4, 5, 1, 6}},
		{false, []int64{5, 2, 4, 3, 1, 6}},
	} {
		movies, total, err := s.SearchAdmin(AdminQuery{LibraryID: libraryID, SortBy: "date", Desc: test.desc, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if total != 6 || !reflect.DeepEqual(movieIDs(movies), test.want) {
			t.Fatalf("desc=%v: ids=%v total=%d, want %v", test.desc, movieIDs(movies), total, test.want)
		}
	}
}

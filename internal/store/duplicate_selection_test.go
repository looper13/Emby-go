package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDuplicateSelectionRulesAndStableTies(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, _ := s.AddLibrary("Selection", t.TempDir())
	ids := []int64{}
	for i, entry := range []struct{ path, added string }{
		{"long-original-copy.strm", "2020-01-01T00:00:00Z"},
		{"a.strm", "2024-01-01T00:00:00Z"},
		{"中.strm", "2023-01-01T00:00:00Z"},
		{"unknown-added.strm", ""},
	} {
		id, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, entry.path), Number: "ALDN-001", Status: "pending", AdditionalParts: []string{"CD2.strm"}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if _, err := s.db.Exec("UPDATE movies SET created_at=? WHERE id=?", entry.added, id); err != nil {
			t.Fatal(i, err)
		}
	}
	for rule, keeper := range map[string]int64{"oldest": ids[0], "newest": ids[1], "shortest": ids[1], "longest": ids[0]} {
		items, err := s.DuplicateMovieSelection(context.Background(), 0, rule)
		if err != nil || len(items) != 3 {
			t.Fatalf("%s selection: %+v %v", rule, items, err)
		}
		for _, item := range items {
			if item.Keep.ID != keeper || item.Movie.ID == keeper || item.Key != "ALDN001" || item.Movie.PathLength != utf8.RuneCountInString(item.Movie.SourcePath) || len(item.Movie.AdditionalParts) != 1 {
				t.Fatalf("%s wrong keep/CD/path: %+v", rule, item)
			}
		}
	}
	// Equal timestamps and missing timestamps use the same deterministic ID tie.
	for _, stamp := range []string{"2024-01-01T00:00:00Z", ""} {
		if _, err := s.db.Exec("UPDATE movies SET created_at=?", stamp); err != nil {
			t.Fatal(err)
		}
		for _, rule := range []string{"oldest", "newest"} {
			items, err := s.DuplicateMovieSelection(context.Background(), lib.ID, rule)
			if err != nil || items[0].Keep.ID != ids[0] {
				t.Fatal("unstable timestamp tie", items, err)
			}
		}
	}
	if _, err := s.DuplicateMovieSelection(context.Background(), 0, "bad SQL"); !errors.Is(err, ErrDuplicateRule) {
		t.Fatal("invalid rule accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.DuplicateMovieSelection(ctx, 0, "oldest"); !errors.Is(err, context.Canceled) {
		t.Fatal("selection ignored cancellation", err)
	}
}

func TestDuplicateSelectionIsFullScopedAndExcludesSinglesTVAndEmptyNumbers(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	one, _ := s.AddLibrary("One", t.TempDir())
	two, _ := s.AddLibrary("Two", t.TempDir())
	for group := 0; group < 31; group++ {
		for copy, lib := range []Library{one, two} {
			if _, err := s.UpsertMovie(Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, fmt.Sprintf("group-%d-copy-%d.strm", group, copy)), Number: fmt.Sprintf("FULL-%03d", group), Status: "success"}, 0, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i, number := range []string{"", " _-.", "ONLY-001", "FULL-000"} {
		m := Movie{LibraryID: one.ID, SourcePath: filepath.Join(one.Path, fmt.Sprintf("excluded-%d.strm", i)), Number: number, Status: "success"}
		if i == 3 {
			m.Television = &Television{Season: 1, Episode: 1}
		}
		if _, err := s.UpsertMovie(m, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.DuplicateMovieSelection(context.Background(), 0, "oldest")
	if err != nil || len(items) != 31 {
		t.Fatalf("selection stopped at a browse page or included exclusions: %d %v", len(items), err)
	}
	local, err := s.DuplicateMovieSelection(context.Background(), one.ID, "oldest")
	if err != nil || len(local) != 0 {
		t.Fatal("single-library scope selected cross-library copies", local, err)
	}
	if err := s.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{one.ID}}); err != nil {
		t.Fatal(err)
	}
	restricted, err := s.DuplicateMovieSelection(context.Background(), 0, "oldest")
	if err != nil || len(restricted) != 0 {
		t.Fatal("full selection leaked inaccessible library", restricted, err)
	}
}

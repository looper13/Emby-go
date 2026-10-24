package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateMoviesNumberScopeAndPagination(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "duplicates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.AddLibrary("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AddLibrary("B", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for index, entry := range []struct {
		number string
		lib    Library
		tv     bool
		parts  []string
		status string
	}{
		{" abf-018 ", a, false, []string{"CD2.strm"}, "success"}, {"ABF018", a, false, nil, "pending"}, {"abf_018", b, false, nil, "manual"}, {"ABF.018", b, false, nil, "success"},
		{"XYZ-001", a, false, nil, "success"}, {"xyz 001", b, false, nil, "success"},
		{"ABF-0182", a, false, nil, "success"}, {"ABF-018X", a, false, nil, "success"}, {"ABF-18", a, false, nil, "success"}, {"ABF-018-C", a, false, nil, "success"},
		{"", a, false, nil, "success"}, {" \t-_.", a, false, nil, "success"}, {"ABF-018", a, true, nil, "success"},
	} {
		m := Movie{LibraryID: entry.lib.ID, SourcePath: filepath.Join(entry.lib.Path, fmt.Sprintf("movie-%d.strm", index)), Title: "same title", Number: entry.number, Status: entry.status, AdditionalParts: entry.parts}
		if entry.tv {
			m.Television = &Television{SeriesPath: "show", Season: 1, Episode: 1}
		}
		if _, err := s.UpsertMovie(m, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	groups, err := s.DuplicateMovieGroups(ctx, 0, 1, 0)
	if err != nil || groups.Total != 2 || groups.TotalMovies != 6 || groups.ExtraCopies != 4 || len(groups.Items) != 1 || groups.Items[0].Key != "ABF018" || groups.Items[0].Count != 4 {
		t.Fatalf("incorrect normalized groups: %+v %v", groups, err)
	}
	next, err := s.DuplicateMovieGroups(ctx, 0, 1, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].Key != "XYZ001" {
		t.Fatalf("group pagination: %+v %v", next, err)
	}
	end, err := s.DuplicateMovieGroups(ctx, 0, 1, 100)
	if err != nil || len(end.Items) != 0 || end.Total != 2 {
		t.Fatalf("end page: %+v %v", end, err)
	}
	members, err := s.DuplicateMovieMembers(ctx, 0, "ABF018", 1, 0)
	if err != nil || members.Total != 4 || len(members.Items) != 1 || len(members.Items[0].AdditionalParts) != 1 {
		t.Fatalf("member page / CD counted twice: %+v %v", members, err)
	}
	second, err := s.DuplicateMovieMembers(ctx, 0, "ABF018", 1, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == members.Items[0].ID {
		t.Fatalf("member paging: %+v %v", second, err)
	}
	local, err := s.DuplicateMovieGroups(ctx, a.ID, 20, 0)
	if err != nil || local.Total != 1 || local.TotalMovies != 2 {
		t.Fatalf("library filter: %+v %v", local, err)
	}
	if err := s.InitializeAdministrator("duplicate-admin", "long-password"); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.CreateUser("duplicate-viewer", "long-password")
	if err != nil {
		t.Fatal(err)
	}
	user := s.ForUser(viewer.ID)
	if err := user.SaveLibraryAccessPolicy(LibraryAccessPolicy{LibraryIDs: []int64{a.ID}}); err != nil {
		t.Fatal(err)
	}
	restricted, err := user.DuplicateMovieGroups(ctx, 0, 20, 0)
	if err != nil || restricted.Total != 1 || restricted.TotalMovies != 2 {
		t.Fatalf("scope leak: %+v %v", restricted, err)
	}
	hidden, err := user.DuplicateMovieMembers(ctx, b.ID, "ABF018", 20, 0)
	if err != nil || hidden.Total != 0 || len(hidden.Items) != 0 {
		t.Fatalf("hidden library leak: %+v %v", hidden, err)
	}
	// Explicit index edits must be visible immediately, without a stale report cache.
	if _, err = s.db.Exec("DELETE FROM movies WHERE library_id=?", b.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.DuplicateMovieGroups(ctx, 0, 20, 0)
	if err != nil || after.Total != 1 || after.TotalMovies != 2 {
		t.Fatalf("stale report: %+v %v", after, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.DuplicateMovieGroups(cancelled, 0, 20, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

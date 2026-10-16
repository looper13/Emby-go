package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestResumedExcludesPlayedBeforePagination(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.AddLibrary("Mixed", "/mixed", LibraryMixed)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary("Other", "/other", LibraryTVShows)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, libraryID int64, episode, played, hidden bool, position int64) int64 {
		t.Helper()
		m := Movie{LibraryID: libraryID, SourcePath: "/" + title + ".strm", Title: title, Status: "success"}
		if episode {
			m.Television = &Television{SeriesPath: "/show", Title: "Show", Season: 1, Episode: 1}
		}
		id, err := s.UpsertMovie(m, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, err := range []error{s.SavePlayback(id, position, 1, "", -1), s.SetPlayed(id, played), s.SetHideFromResume(id, hidden)} {
			if err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	playedMovie := add("A played movie", l.ID, false, true, false, 100)
	playedEpisode := add("B played episode", l.ID, true, true, false, 100)
	movie := add("C movie", l.ID, false, false, false, 100)
	episode := add("D episode", l.ID, true, false, false, 100)
	add("E hidden", l.ID, true, false, true, 100)
	add("F no progress", l.ID, true, false, false, 0)
	otherEpisode := add("G other", other.ID, true, false, false, 100)
	for _, tc := range []struct {
		name    string
		library int64
		types   []string
		offset  int
		total   int
		id      int64
	}{
		{"global", 0, nil, 0, 3, movie},
		{"global second page", 0, nil, 1, 3, episode},
		{"library", l.ID, nil, 0, 2, movie},
		{"library second page", l.ID, nil, 1, 2, episode},
		{"movies", l.ID, []string{"Movie"}, 0, 1, movie},
		{"episodes", l.ID, []string{"Episode"}, 0, 1, episode},
		{"video", l.ID, []string{"Video"}, 1, 2, episode},
		{"other library", other.ID, []string{"Episode"}, 0, 1, otherEpisode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := s.ResumedScoped(tc.library, tc.types, 1, tc.offset)
			if err != nil || total != tc.total || len(items) != 1 || items[0].ID != tc.id {
				t.Fatalf("items=%v total=%d err=%v", items, total, err)
			}
		})
	}
	for _, id := range []int64{playedMovie, playedEpisode} {
		if err := s.SetPlayed(id, false); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.Resumed(10, 0)
	if err != nil || total != 5 || len(items) != 5 || items[0].ID != playedMovie || items[1].ID != playedEpisode {
		t.Fatalf("unplayed items did not return to resume: items=%v total=%d err=%v", items, total, err)
	}
	d, err := s.Data(playedEpisode)
	if err != nil || d.PositionTicks != 100 || d.PlayCount != 1 {
		t.Fatal("resume filtering changed playback history", d, err)
	}
}

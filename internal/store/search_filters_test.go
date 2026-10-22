package store

import "testing"

func TestFeatureFiltersKeepCaseAndCombinationRules(t *testing.T) {
	s, libID := newFeatureStore(t)
	upsertFixtureMovie(t, s, libID, Movie{LibraryID: libID, SourcePath: "/tmp/av/drama.strm", Title: "Drama", Status: "success", Genres: []string{"Drama", "剧情", "Ä"}, Tags: []string{"Tag A"}, Studios: []string{"Studio X"}})
	upsertFixtureMovie(t, s, libID, Movie{LibraryID: libID, SourcePath: "/tmp/av/comedy.strm", Title: "Comedy", Status: "success", Genres: []string{"Comedy"}, Tags: []string{"Tag B"}, Studios: []string{"Studio Y"}})
	for _, tc := range []struct {
		name, genre, tag, studio string
		want                     int
	}{
		{"genre", "drama", "", "", 1},
		{"tag", "", "tag a", "", 1},
		{"studio", "", "", "studio x", 1},
		{"combined", "DRAMA", "tag A", "STUDIO X", 1},
		{"alternatives", "missing|drama", "missing|tag a", "missing|studio x", 1},
		{"multiple", "drama|comedy", "tag a|tag b", "studio x|studio y", 2},
		{"conjunction", "drama", "tag b", "", 0},
		{"missing", "missing", "", "", 0},
		{"unicode", "剧情", "", "", 1},
		{"non-ascii-case", "ä", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movies, total, err := s.SearchAdmin(AdminQuery{LibraryID: libID, Genre: tc.genre, Tag: tc.tag, Studio: tc.studio, Limit: 100})
			if err != nil || total != tc.want || len(movies) != tc.want {
				t.Fatalf("admin total=%d movies=%d want=%d err=%v", total, len(movies), tc.want, err)
			}
			movies, total, err = s.SearchScoped(libID, "", "", "", tc.genre, tc.tag, tc.studio, "", false, nil, "title", false, 100, 0)
			if err != nil || total != tc.want || len(movies) != tc.want {
				t.Fatalf("client total=%d movies=%d want=%d err=%v", total, len(movies), tc.want, err)
			}
		})
	}
}

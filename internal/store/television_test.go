package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLibraryContentTypesPersistAndKeepExistingTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "types.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	for _, typ := range []string{LibraryMovies, LibraryTVShows, LibraryMixed} {
		l, err := s.AddLibrary(typ, "/"+typ, typ)
		if err != nil || l.Type != typ {
			t.Fatal(l, err)
		}
		want := typ
		if typ == LibraryMixed {
			want = ""
		}
		if l.CollectionType() != want {
			t.Fatal(l)
		}
		if renamed, err := s.AddLibrary("renamed", "/"+typ); err != nil || renamed.Type != typ || renamed.ID != l.ID {
			t.Fatal(renamed, err)
		}
	}
	if _, err = s.AddLibrary("bad", "/bad", "unsupported"); err == nil {
		t.Fatal("unsupported type accepted")
	}
	if _, err = s.AddLibrary("bad", "/tvshows", LibraryMovies); err == nil {
		t.Fatal("existing television type changed")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	libs, err := s.DirectoryLibraries()
	if err != nil || len(libs) != 3 || libs[1].Type != LibraryTVShows || libs[2].Type != LibraryMixed {
		t.Fatal(libs, err)
	}
	var migrations int
	if err = s.db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version=4").Scan(&migrations); err != nil || migrations != 1 {
		t.Fatal(migrations, err)
	}
}

func TestTelevisionRoundTripFilteringAndUserData(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.AddLibrary("mixed", "/mixed", LibraryMixed)
	if err != nil {
		t.Fatal(err)
	}
	m := Movie{LibraryID: l.ID, SourcePath: "/mixed/Show/Season 2/a.strm", Title: "Pilot", Status: "success", Television: &Television{SeriesPath: filepath.Clean("/mixed/Show"), Title: "Show", Season: 2, Episode: 10, Year: 2026}}
	id, err := s.UpsertMovie(m, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertMovie(Movie{LibraryID: l.ID, SourcePath: "/mixed/film.strm", Title: "Film", Status: "success"}, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO userdata(movie_id,position_ticks,play_count,played) VALUES(?,123,2,1)", id); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Movie(id)
	if err != nil || stored.ItemType() != "Episode" || *stored.Television != *m.Television {
		t.Fatal(stored, err)
	}
	m.Title = "Updated"
	updatedID, err := s.UpsertMovie(m, 2, time.Now())
	if err != nil || updatedID != id {
		t.Fatal(updatedID, err)
	}
	d, err := s.Data(id)
	if err != nil || d.PositionTicks != 123 || !d.Played || d.PlayCount != 2 {
		t.Fatal(d, err)
	}
	for _, typ := range []string{"Movie", "Episode"} {
		ms, total, err := s.SearchAdmin(AdminQuery{LibraryID: l.ID, MediaType: typ, Limit: 1})
		if err != nil || total != 1 || len(ms) != 1 || ms[0].ItemType() != typ {
			t.Fatal(typ, ms, total, err)
		}
	}
	for _, types := range [][]string{{"Video"}, {"Movie", "Episode"}} {
		ms, total, err := s.SearchMediaTypes(types, l.ID, "", "", "", "", "", "", false, false, "SortName", false, 10, 0, "")
		if err != nil || total != 2 || len(ms) != 2 {
			t.Fatal(types, ms, total, err)
		}
	}
	ms, total, err := s.SearchAdmin(AdminQuery{Term: "Show", MediaType: "Episode", Limit: 10})
	if err != nil || total != 1 || len(ms) != 1 {
		t.Fatal(ms, total, err)
	}
	for _, raw := range []string{TVSeriesID(l.ID, m.Television.SeriesPath), TVSeasonID(l.ID, m.Television.SeriesPath, 2)} {
		_, lib, path, _, ok := ParseTVID(raw)
		if !ok || lib != l.ID || path != m.Television.SeriesPath {
			t.Fatal(raw)
		}
		if eps, err := s.TVFolderEpisodes(raw); err != nil || len(eps) != 1 {
			t.Fatal(eps, err)
		}
	}
	if _, _, _, _, ok := ParseTVID("tv-season:1:-1:YS9i"); ok {
		t.Fatal("negative season accepted")
	}
	if candidates, err := s.MoviesForScrape(l.ID, false, 10); err != nil || len(candidates) != 1 || candidates[0].ItemType() != "Movie" {
		t.Fatal(candidates, err)
	}
	if count, err := s.CountMoviesForScrape(l.ID, false); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

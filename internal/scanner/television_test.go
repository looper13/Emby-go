package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestTelevisionAndMixedLibrariesIdentifyEpisodes(t *testing.T) {
	for _, typ := range []string{store.LibraryMovies, store.LibraryTVShows, store.LibraryMixed} {
		t.Run(typ, func(t *testing.T) {
			root := t.TempDir()
			s := newStore(t, root)
			lib, err := s.AddLibrary("Library", root, typ)
			if err != nil {
				t.Fatal(err)
			}
			writeScanFile(t, root, "Show/tvshow.nfo", `<tvshow><title>Drama</title><originaltitle>Original</originaltitle><plot>Series plot</plot><year>2026</year><genre>Drama</genre></tvshow>`)
			writeScanFile(t, root, "Show/Season 1/Show.S01E01.strm", "https://media.test/one.mp4")
			writeScanFile(t, root, "Show/Season 1/Show.S01E01.nfo", `<episodedetails><title>Pilot</title><season>2</season><episode>10</episode><aired>2026-10-16</aired><runtime>45</runtime></episodedetails>`)
			writeScanFile(t, root, "Show/Season 1/Show.S01E02.strm", "https://media.test/two.mp4")
			writeScanFile(t, root, "Show/Season 1/Show.S01E02.nfo", `<episodedetails><title>Second</title></episodedetails>`)
			writeScanFile(t, root, "Show/Specials/Show.S00E01.strm", "https://media.test/special.mp4")
			writeScanFile(t, root, "Show/Specials/Show.S00E01.nfo", `<episodedetails><season>0</season><episode>1</episode></episodedetails>`)
			writeScanFile(t, root, "Film.strm", "https://media.test/film.mp4")
			writeScanFile(t, root, "Film.nfo", `<movie><title>Film</title></movie>`)
			result, err := Scan(context.Background(), s, lib)
			if err != nil {
				t.Fatal(err)
			}
			wantEpisodes, wantMovies, wantFailed := 3, 1, 0
			if typ == store.LibraryMovies {
				wantEpisodes, wantFailed = 0, 3
			}
			if typ == store.LibraryTVShows {
				wantMovies, wantFailed = 0, 1
			}
			if result.Success != wantEpisodes+wantMovies || result.Failed != wantFailed {
				t.Fatal(result)
			}
			eps, err := s.TVEpisodes(lib.ID, "")
			if err != nil || len(eps) != wantEpisodes {
				t.Fatal(eps, err)
			}
			if wantEpisodes > 0 {
				if eps[0].Television.Season != 0 || eps[0].Title != "第 1 集" || eps[2].Television.Season != 2 || eps[2].Television.Episode != 10 || eps[2].Premiere != "2026-10-16" || eps[2].RuntimeSeconds != 2700 || eps[2].Television.Title != "Drama" || eps[2].Television.SeriesPath != filepath.Join(root, "Show") {
					t.Fatal(eps)
				}
			}
		})
	}
}

func TestSharedTVShowMetadataRefreshAndFailurePreserveEpisodes(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	lib, err := s.AddLibrary("TV", root, store.LibraryTVShows)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "Show/tvshow.nfo", `<tvshow><title>Old show</title></tvshow>`)
	for _, name := range []string{"Show.S01E01", "Show.S01E02"} {
		writeScanFile(t, root, "Show/Season 1/"+name+".strm", "https://media.test/episode.mp4")
		writeScanFile(t, root, "Show/Season 1/"+name+".nfo", `<episodedetails><title>Episode</title></episodedetails>`)
	}
	requireScan(t, s, lib, Result{Success: 2, Added: 2})
	requireScan(t, s, lib, Result{Skipped: 2})
	eps, _ := s.TVEpisodes(lib.ID, "")
	id := eps[0].ID
	parent := filepath.Join(root, "Show", "tvshow.nfo")
	info, _ := os.Stat(parent)
	writeScanFile(t, root, "Show/tvshow.nfo", `<tvshow><title>New show</title></tvshow>`)
	if err = os.Chtimes(parent, info.ModTime(), info.ModTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	requireScan(t, s, lib, Result{Success: 2, Updated: 2})
	eps, _ = s.TVEpisodes(lib.ID, "")
	if eps[0].ID != id || eps[0].Television.Title != "New show" || eps[1].Television.Title != "New show" {
		t.Fatal(eps)
	}
	writeScanFile(t, root, "Show/tvshow.nfo", `<tvshow><title>Broken`)
	requireScan(t, s, lib, Result{Failed: 2})
	eps, _ = s.TVEpisodes(lib.ID, "")
	if len(eps) != 2 || eps[0].Television.Title != "New show" {
		t.Fatal("failed parent overwrote episodes", eps)
	}
	writeScanFile(t, root, "Show/tvshow.nfo", `<tvshow><title>Last one</title></tvshow>`)
	if err = os.Chtimes(parent, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	result, err := RefreshDirectory(context.Background(), s, lib, filepath.Join(root, "Show"), true, nil, true)
	if err != nil || result.Updated != 2 {
		t.Fatal(result, err)
	}
	eps, _ = s.TVEpisodes(lib.ID, "")
	if eps[0].Television.Title != "Last one" {
		t.Fatal(eps)
	}
	for _, ep := range eps {
		if err = os.Remove(ep.SourcePath); err != nil {
			t.Fatal(err)
		}
	}
	requireScan(t, s, lib, Result{Deleted: 2})
	if eps, err = s.TVEpisodes(lib.ID, ""); err != nil || len(eps) != 0 {
		t.Fatal(eps, err)
	}
}

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func benchmarkStore(b *testing.B) (*Store, Library) {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "benchmark.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	lib, err := s.AddLibrary("Benchmark", b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	return s, lib
}

func BenchmarkScannedMovieBatch(b *testing.B) {
	for _, mode := range []string{"initial", "rebuild"} {
		b.Run(mode, func(b *testing.B) {
			s, lib := benchmarkStore(b)
			entries := make([]ScannedMovie, 100)
			for i := range entries {
				entries[i] = ScannedMovie{Movie: Movie{LibraryID: lib.ID, SourcePath: fmt.Sprintf("/benchmark/%d.strm", i), Status: "success", Title: fmt.Sprintf("Movie %d", i), Genres: []string{"Drama", "Comedy"}, Tags: []string{"Tag A", "Tag B"}, Studios: []string{"Studio"}, Director: "Director", Collection: "Collection"}, Fingerprint: "nfo-v3:benchmark"}
				for actor := 0; actor < 10; actor++ {
					entries[i].Actors = append(entries[i].Actors, ActorRef{Name: fmt.Sprintf("Actor %d", actor), AvatarURL: fmt.Sprintf("https://example.test/%d.jpg", actor)})
				}
			}
			if mode == "rebuild" {
				if err := s.SaveScannedMovies(entries); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if mode == "initial" {
					for i := range entries {
						entries[i].Movie.SourcePath = fmt.Sprintf("/benchmark/%d/%d.strm", iteration, i)
					}
				}
				if err := s.SaveScannedMovies(entries); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMovieWallSearch(b *testing.B) {
	s, lib := benchmarkStore(b)
	count := 100000
	if value, err := strconv.Atoi(os.Getenv("EMBY_BENCH_MOVIES")); err == nil && value > 0 {
		count = value
	}
	// Seed the real schema without timing fixture generation or NFO filesystem I/O.
	_, err := s.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?)
INSERT INTO movies(library_id,source_path,status,title,file_mtime,source_protocol,source_container,number,nfo_path,output_dir,original_title,plot,year,premiered,rating,director,series,maker,label,genres,tags,studios,poster_path,backdrop_path,landscape_path,created_at,updated_at)
SELECT ?,printf('/benchmark/%d.strm',x),CASE WHEN x%10=0 THEN 'pending' WHEN x%7=0 THEN 'manual' ELSE 'success' END,printf('Movie %07d',(x*7919)%1000003),'','','','','','','','',2026,'',0,'','','','','[]','[]','[]','','','','2026-01-01','2026-01-01' FROM n`, count, lib.ID)
	if err != nil {
		b.Fatal(err)
	}
	if err := s.BumpVersion(lib.ID); err != nil {
		b.Fatal(err)
	}
	s.OptimizeSearchIndexes()
	for _, scope := range []string{"all", "library"} {
		for _, sortBy := range []string{"title", "datecreated"} {
			for _, mode := range []string{"cold", "pages"} {
				b.Run(scope+"/"+sortBy+"/"+mode, func(b *testing.B) {
					q := AdminQuery{SortBy: sortBy, Limit: 100}
					if scope == "library" {
						q.LibraryID = lib.ID
					}
					b.ReportAllocs()
					b.ResetTimer()
					for iteration := 0; iteration < b.N; iteration++ {
						if mode == "cold" {
							// Force the same count invalidation as a committed metadata write.
							s.TouchMovie(1)
						}
						q.Offset = (iteration % 10) * q.Limit
						movies, total, err := s.SearchAdmin(q)
						if err != nil || len(movies) != q.Limit || total != count-count/10 {
							b.Fatalf("movies=%d total=%d err=%v", len(movies), total, err)
						}
					}
				})
			}
		}
	}
}

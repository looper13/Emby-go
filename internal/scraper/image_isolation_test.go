package scraper

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"emby-go/internal/metatube"
	"emby-go/internal/store"
)

func TestFlatMovieImagesAreIsolated(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "overwrite"}[overwrite], func(t *testing.T) {
			backend := fakeBackend(t)
			dir := t.TempDir()
			s := New(Config{BaseURL: backend.URL, Token: "test", DownloadImages: true}, nil)
			var results [2]ApplyResult
			var errs [2]error
			var wg sync.WaitGroup
			for index, number := range []string{"ABF-018", "ABF-019"} {
				wg.Add(1)
				go func(index int, number string) {
					defer wg.Done()
					results[index], errs[index] = s.Apply(context.Background(), store.Movie{SourcePath: filepath.Join(dir, number+".strm"), OutputDir: dir},
						metatube.MovieInfo{Provider: "fanza", ID: number, Number: number, Title: number}, ApplyOptions{Overwrite: overwrite})
				}(index, number)
			}
			wg.Wait()
			for index, result := range results {
				if errs[index] != nil || result.ImageCount != 3 {
					t.Fatalf("movie %d: %+v, %v", index, result, errs[index])
				}
				for _, path := range result.Images {
					if _, err := os.Stat(path); err != nil {
						t.Fatal(err)
					}
				}
			}
			for index, path := range results[0].Images {
				if path == results[1].Images[index] {
					t.Fatal("independent movies share an image destination")
				}
			}
			// A later fill-missing run must preserve this movie's own images.
			again, err := s.Apply(context.Background(), store.Movie{SourcePath: filepath.Join(dir, "ABF-018.strm"), OutputDir: dir},
				metatube.MovieInfo{Provider: "fanza", ID: "a", Number: "ABF-018", Title: "A"}, ApplyOptions{})
			if err != nil || again.ImageCount != 0 {
				t.Fatalf("existing movie images not reused: %+v, %v", again, err)
			}
			leftovers, err := filepath.Glob(filepath.Join(dir, ".scrape-image-*.tmp"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary images left: %v, %v", leftovers, err)
			}
		})
	}
}

func TestDashedSeriesAliasIsConsistent(t *testing.T) {
	for _, input := range []string{"259LUXU1234", "259LUXU-1234", "259luxu_1234", "259luxu 1234"} {
		fields := buildFields(metatube.MovieInfo{Number: input}, "标题", "", true, versionMark{})
		if fields.Number != "LUXU-1234" {
			t.Errorf("%q -> %q", input, fields.Number)
		}
	}
	if title := joinNumberTitle("LUXU-1234", "259LUXU-1234 标题"); title != "LUXU-1234 标题" {
		t.Errorf("alias duplicated in title: %q", title)
	}
}

package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/store"
)

func benchmarkLibrary(benchmark *testing.B, flat bool, count int) string {
	benchmark.Helper()
	root := benchmark.TempDir()
	payload := []byte(`<movie><title>Profile movie</title><year>2024</year><plot>` + strings.Repeat("Profile synopsis. ", 100) + `</plot><director>Director</director><set>Collection</set><genre>Drama</genre><genre>Comedy</genre><tag>Tag1</tag><tag>Tag2</tag><studio>Studio</studio><actor><name>Actor1</name><thumb>https://example.test/1.jpg</thumb></actor><actor><name>Actor2</name></actor><actor><name>Actor3</name></actor><actor><name>Actor4</name></actor></movie>`)
	for index := 0; index < count; index++ {
		directory := root
		name := fmt.Sprintf("movie-%04d", index)
		if !flat {
			directory = filepath.Join(root, name)
			if err := os.Mkdir(directory, 0755); err != nil {
				benchmark.Fatal(err)
			}
			name = "movie"
		}
		if err := os.WriteFile(filepath.Join(directory, name+".strm"), []byte("https://example.test/movie.mp4\n"), 0644); err != nil {
			benchmark.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name+".nfo"), payload, 0644); err != nil {
			benchmark.Fatal(err)
		}
	}
	return root
}

func BenchmarkLibraryScan(benchmark *testing.B) {
	const count = 300
	for _, layout := range []string{"folders", "flat"} {
		for _, mode := range []string{"initial", "rebuild", "unchanged"} {
			benchmark.Run(layout+"/"+mode, func(benchmark *testing.B) {
				root := benchmarkLibrary(benchmark, layout == "flat", count)
				open := func() (*store.Store, store.Library) {
					database, err := store.Open(filepath.Join(benchmark.TempDir(), "index.db"))
					if err != nil {
						benchmark.Fatal(err)
					}
					library, err := database.AddLibrary("benchmark", root)
					if err != nil {
						benchmark.Fatal(err)
					}
					return database, library
				}
				database, library := open()
				if mode != "initial" {
					if _, err := Scan(context.Background(), database, library); err != nil {
						benchmark.Fatal(err)
					}
				}
				benchmark.ReportAllocs()
				benchmark.ResetTimer()
				for iteration := 0; iteration < benchmark.N; iteration++ {
					var result Result
					var err error
					if mode == "unchanged" {
						result, err = Scan(context.Background(), database, library)
					} else {
						result, err = RebuildWithProgress(context.Background(), database, library, nil)
					}
					if err != nil || result.Failed != 0 || result.Added+result.Updated+result.Skipped != count {
						benchmark.Fatalf("scan=%+v err=%v", result, err)
					}
					if mode == "initial" && iteration+1 < benchmark.N {
						benchmark.StopTimer()
						database.Close()
						database, library = open()
						benchmark.StartTimer()
					}
				}
				benchmark.StopTimer()
				database.Close()
				benchmark.ReportMetric(float64(benchmark.N*count)/benchmark.Elapsed().Seconds(), "movies/s")
			})
		}
	}
}

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestBatchScanBoundariesAndReadFailure(test *testing.T) {
	root := test.TempDir()
	for index := 0; index < 205; index++ {
		name := fmt.Sprintf("%03d", index)
		writeScanFile(test, root, name+".strm", "https://media.test/movie.mp4\n")
		writeScanFile(test, root, name+".nfo", "<movie><title>"+name+"</title><actor><name>Actor</name></actor></movie>")
	}
	database, library := scanLibrary(test, root)
	committedBeforeEnd := false
	var progress Progress
	result, err := ScanWithProgress(database, library, func(current Progress) {
		progress = current
		if current.Done >= 100 && current.Done < current.Total && current.Result.Added >= 100 {
			if _, err := database.MovieIDByPath(filepath.Join(root, "000.strm")); err == nil {
				committedBeforeEnd = true
			}
		}
	})
	if err != nil || result != (Result{Added: 205, Success: 205}) || !committedBeforeEnd || progress.Result != result {
		test.Fatalf("batch results=%+v progress=%+v early=%v err=%v", result, progress, committedBeforeEnd, err)
	}
	requireScan(test, database, library, Result{Skipped: 205})
	writeScanFile(test, root, "101.strm", "")
	if err := os.Remove(filepath.Join(root, "204.strm")); err != nil {
		test.Fatal(err)
	}
	result, err = RebuildWithProgress(database, library, nil)
	if err != nil || result != (Result{Updated: 203, Success: 203, Failed: 1}) {
		test.Fatalf("partial failure = %+v %v", result, err)
	}
	if fingerprints, err := database.ScanFingerprints(library.ID); err != nil || len(fingerprints) != 205 {
		test.Fatalf("failed scan removed old entries: %d %v", len(fingerprints), err)
	}
}

func TestSourceStateImageChangesAndStableAttributes(test *testing.T) {
	for _, count := range []int{0, 70} {
		test.Run(fmt.Sprintf("directory-entries-%d", count), func(test *testing.T) {
			root := test.TempDir()
			for index := 0; index < count; index++ {
				writeScanFile(test, root, fmt.Sprintf("ignored-%03d.tmp", index), "ignored")
			}
			writeScanFile(test, root, "movie.strm", "https://media.test/movie.mp4\n")
			writeScanFile(test, root, "movie.nfo", "<movie><title>Movie</title></movie>")
			path := filepath.Join(root, "movie.strm")
			directory := &imageDirectory{}
			before, err := readSourceState(path, nil, "", directory)
			if err != nil {
				test.Fatal(err)
			}
			info, err := os.Stat(root)
			if err != nil {
				test.Fatal(err)
			}
			writeScanFile(test, root, "poster.jpg", "poster")
			if err := os.Chtimes(root, info.ModTime(), info.ModTime()); err != nil {
				test.Fatal(err)
			}
			after, err := readSourceState(path, nil, "", directory)
			if err != nil || after.Fingerprint == before.Fingerprint || after.Images.Poster != filepath.Join(root, "poster.jpg") {
				test.Fatalf("new image missed with preserved directory mtime: %+v %v", after, err)
			}
			writeScanFile(test, root, "poster.webp", "preferred")
			preferred, err := readSourceState(path, nil, "", directory)
			if err != nil || preferred.Images.Poster != filepath.Join(root, "poster.webp") {
				test.Fatalf("image priority changed: %+v %v", preferred, err)
			}
			if err := os.Remove(filepath.Join(root, "poster.webp")); err != nil {
				test.Fatal(err)
			}
			restored, err := readSourceState(path, nil, "", directory)
			if err != nil || restored.Fingerprint != after.Fingerprint {
				test.Fatalf("image fallback changed: %+v %v", restored, err)
			}
		})
	}
}

func TestBatchScanDoesNotClaimRolledBackWrites(test *testing.T) {
	root := test.TempDir()
	writeScanFile(test, root, "movie.strm", "https://media.test/movie.mp4\n")
	writeScanFile(test, root, "movie.nfo", "<movie><title>Movie</title></movie>")
	database, library := scanLibrary(test, root)
	result, err := ScanWithProgress(database, library, func(progress Progress) {
		if progress.Done == 0 && progress.Total > 0 {
			database.Close()
		}
	})
	if err == nil || result.Added != 0 || result.Success != 0 {
		test.Fatalf("uncommitted batch reported success: %+v %v", result, err)
	}
}

func TestPreparedMovieRemainsIndependentOfDisk(test *testing.T) {
	root := test.TempDir()
	writeScanFile(test, root, "movie.strm", "https://media.test/movie.mp4\n")
	writeScanFile(test, root, "movie.nfo", "<movie><title>Original</title></movie>")
	path := filepath.Join(root, "movie.strm")
	state, err := readSourceState(path, nil, "", &imageDirectory{})
	if err != nil {
		test.Fatal(err)
	}
	entry, outcome := prepareCandidate(store.Library{ID: 1}, candidate{path: path, info: state.Info}, nil, "", state.Images)
	if outcome != (Result{Success: 1}) || entry.Movie.Title != "Original" {
		test.Fatalf("prepare failed: %+v %+v", entry, outcome)
	}
	writeScanFile(test, root, "movie.nfo", "<movie><title>Updated contents</title></movie>")
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(root, "movie.nfo"), stamp, stamp); err != nil {
		test.Fatal(err)
	}
	after, err := readSourceState(path, nil, "", &imageDirectory{})
	if err != nil || state.Fingerprint == after.Fingerprint || entry.Movie.Title != "Original" {
		test.Fatalf("post-read stability check failed: %+v %v", after, err)
	}
}

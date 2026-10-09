package scanner

import (
	"context"
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
	result, err := ScanWithProgress(context.Background(), database, library, func(current Progress) {
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
	writeScanFile(test, root, "101.nfo", "<movie><title>Broken")
	if err := os.Remove(filepath.Join(root, "204.strm")); err != nil {
		test.Fatal(err)
	}
	result, err = RebuildWithProgress(context.Background(), database, library, nil)
	if err != nil || result != (Result{Updated: 203, Success: 203, Failed: 1}) {
		test.Fatalf("partial failure = %+v %v", result, err)
	}
	if fingerprints, err := database.ScanFingerprints(library.ID); err != nil || len(fingerprints) != 205 {
		test.Fatalf("failed scan removed old entries: %d %v", len(fingerprints), err)
	}
}

func TestBatchScanDoesNotClaimRolledBackWrites(test *testing.T) {
	root := test.TempDir()
	writeScanFile(test, root, "movie.strm", "https://media.test/movie.mp4\n")
	writeScanFile(test, root, "movie.nfo", "<movie><title>Movie</title></movie>")
	database, library := scanLibrary(test, root)
	result, err := ScanWithProgress(context.Background(), database, library, func(progress Progress) {
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
	state, err := readMetadataState(path, "")
	if err != nil {
		test.Fatal(err)
	}
	entry, outcome := prepareCandidate(store.Library{ID: 1}, candidate{path: path}, nil, "")
	if outcome != (Result{Success: 1}) || entry.Movie.Title != "Original" {
		test.Fatalf("prepare failed: %+v %+v", entry, outcome)
	}
	writeScanFile(test, root, "movie.nfo", "<movie><title>Updated contents</title></movie>")
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(root, "movie.nfo"), stamp, stamp); err != nil {
		test.Fatal(err)
	}
	after, err := readMetadataState(path, "")
	if err != nil || state.Fingerprint == after.Fingerprint || entry.Movie.Title != "Original" {
		test.Fatalf("post-read stability check failed: %+v %v", after, err)
	}
}

// If NFO changes during parsing, the saved movie remains retryable.
func TestNFOChangeDuringPreparationRetries(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "invalid STRM is checked at playback")
	writeScanFile(t, root, "a.nfo", "<movie><title>Old</title></movie>")
	database, library := scanLibrary(t, root)
	calls := 0
	metadataStat = func(path string) (os.FileInfo, error) {
		calls++
		if calls == 2 {
			writeScanFile(t, root, "a.nfo", "<movie><title>New contents</title></movie>")
		}
		return os.Stat(path)
	}
	t.Cleanup(func() { metadataStat = os.Stat })
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	fingerprints, err := database.ScanFingerprints(library.ID)
	if err != nil || fingerprints[filepath.Join(root, "a.strm")] != "" {
		t.Fatalf("unstable NFO certified: %v %v", fingerprints, err)
	}
	metadataStat = os.Stat
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if visible(t, database)[0].Title != "New contents" {
		t.Fatal("NFO change was not retried")
	}
}

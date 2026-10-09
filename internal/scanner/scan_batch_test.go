package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	writeScanFile(test, root, "101.strm", "")
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

func TestSharedImageSelectionRequiresFreshStabilityCheck(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "https://media.test/a.mp4")
	writeScanFile(t, root, "b.strm", "https://media.test/b.mp4")
	writeScanFile(t, root, "poster.jpg", "old")
	shared := &imageDirectory{reuse: true}
	before, err := readSourceState(filepath.Join(root, "a.strm"), nil, "", shared)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "poster.webp", "new preferred image")
	cached, err := readSourceState(filepath.Join(root, "b.strm"), nil, "", shared)
	if err != nil || !reflect.DeepEqual(cached.Images, before.Images) {
		t.Fatalf("directory selection was not shared: %+v %v", cached, err)
	}
	fresh, err := readSourceState(filepath.Join(root, "a.strm"), nil, "", &imageDirectory{})
	if err != nil || fresh.Fingerprint == before.Fingerprint || fresh.Images.Poster != filepath.Join(root, "poster.webp") {
		t.Fatalf("stability check missed changed images: %+v %v", fresh, err)
	}
}

// 大目录里扫描中途新增的图片仍必须被检出：稳定性复核改为只认目录清单内的
// 名字（见 scanner.go 的 freshImages），能否发现新图片就落到「批次写入前复核
// 目录清单」这条链路上——本轮不写成功指纹，下一轮扫描补选图片。
func TestLargeDirectoryMidScanImageChangeIsDetected(test *testing.T) {
	root := test.TempDir()
	for index := 0; index < 65; index++ {
		writeScanFile(test, root, fmt.Sprintf("junk-%02d.txt", index), "unused")
	}
	writeScanFile(test, root, "a.strm", "https://media.test/a.mp4\n")
	writeScanFile(test, root, "a.nfo", "<movie><title>A</title></movie>")
	database, library := scanLibrary(test, root)
	requireScan(test, database, library, Result{Added: 1, Success: 1})
	if movie := visible(test, database)[0]; movie.PosterPath != "" {
		test.Fatalf("unexpected poster before injection: %s", movie.PosterPath)
	}

	injected := false
	result, err := RebuildWithProgress(context.Background(), database, library, func(progress Progress) {
		if !injected && progress.Phase == PhaseProcess && progress.Done > 0 {
			injected = true
			// 该片已被处理、批次尚未写入：此刻出现的图片只能靠批次级清单复核发现。
			writeScanFile(test, root, "a-poster.jpg", "poster")
		}
	})
	if err != nil || !injected || result != (Result{Updated: 1, Success: 1}) {
		test.Fatalf("中途新增图片的扫描 = %+v injected=%v err=%v", result, injected, err)
	}
	fingerprints, err := database.ScanFingerprints(library.ID)
	if err != nil || fingerprints[filepath.Join(root, "a.strm")] != "" {
		test.Fatalf("目录清单变化后仍写入了成功指纹: %+v %v", fingerprints, err)
	}
	requireScan(test, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(test, database)[0]; movie.PosterPath != filepath.Join(root, "a-poster.jpg") {
		test.Fatalf("新图片未被补选: %+v", movie)
	}
}

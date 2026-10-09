package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLargeArtworkMembershipChangesDuringBatch(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		writeScanFile(t, root, fmt.Sprintf("movie-%02d.strm", i), "https://media.test/movie.mp4")
		writeScanFile(t, root, fmt.Sprintf("movie-%02d.nfo", i), "<movie><title>Movie</title></movie>")
	}
	for i := 0; i < 65; i++ {
		writeScanFile(t, root, fmt.Sprintf("unused-%d.txt", i), "unused")
	}
	database, library := scanLibrary(t, root)
	added := false
	result, err := ScanWithProgress(context.Background(), database, library, func(progress Progress) {
		if progress.Done == 1 && !added {
			added = true
			writeScanFile(t, root, "extrafanart/fanart1.jpg", "image added during scan")
		}
	})
	if err != nil || result.Added != 10 {
		t.Fatalf("initial scan: %+v %v", result, err)
	}
	// Catalog membership changed while the batch was being prepared. It must
	// remain retryable instead of certifying the original empty artwork set.
	fingerprints, err := database.ScanFingerprints(library.ID)
	if err != nil {
		t.Fatal(err)
	}
	for path, fingerprint := range fingerprints {
		if fingerprint != "" {
			t.Fatalf("stale fingerprint saved for %s: %s", path, fingerprint)
		}
	}
	requireScan(t, database, library, Result{Updated: 10, Success: 10})
	for _, movie := range visible(t, database) {
		if len(movie.BackdropPaths) != 1 {
			t.Fatalf("retry missed artwork: %+v", movie)
		}
	}
	requireScan(t, database, library, Result{Skipped: 10})
}

func TestLocalArtworkIncrementalMembership(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "HMN-145-破解.strm", "https://media.test/movie.mp4")
	writeScanFile(t, root, "HMN-145-破解.nfo", "<movie><title>Local art</title></movie>")
	for _, name := range []string{"poster.jpg", "fanart.jpg", "thumb.jpg", "extrafanart/fanart1.jpg", "extrafanart/fanart10.jpg", "extrafanart/fanart2.jpg"} {
		writeScanFile(t, root, name, "image")
	}
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	movie := visible(t, database)[0]
	if len(movie.BackdropPaths) != 4 || filepath.Base(movie.BackdropPaths[2]) != "fanart2.jpg" || filepath.Base(movie.BackdropPaths[3]) != "fanart10.jpg" || filepath.Base(movie.LandscapePath) != "thumb.jpg" {
		t.Fatalf("incomplete artwork index: %+v", movie)
	}
	requireScan(t, database, library, Result{Skipped: 1})
	extra := filepath.Join(root, "extrafanart")
	stamp, err := os.Stat(extra)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "extrafanart/fanart3.jpg", "new image")
	if err := os.Chtimes(extra, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if len(visible(t, database)[0].BackdropPaths) != 5 {
		t.Fatal("new image not indexed")
	}
	if err := os.Remove(filepath.Join(extra, "fanart2.jpg")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if len(visible(t, database)[0].BackdropPaths) != 4 {
		t.Fatal("removed image retained")
	}
	if err := os.RemoveAll(extra); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if len(visible(t, database)[0].BackdropPaths) != 1 {
		t.Fatal("removed artwork directory retained")
	}
}

// TestScanStoresTrailerAndCover 扫库把 NFO 的预告片与远程封面写进索引：
// 详情页靠它们播预告片，并在本地没有图片时用远程封面兜底。
func TestScanStoresTrailerAndCover(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "HMN-145.strm", "https://media.test/hmn145.mp4")
	writeScanFile(t, root, "HMN-145.nfo",
		`<movie><title>HMN-145 标题</title><trailer>https://cdn.test/hmn145.mp4</trailer><cover>https://cdn.test/hmn145.jpg</cover></movie>`)
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	movies := visible(t, database)
	if len(movies) != 1 || movies[0].TrailerURL != "https://cdn.test/hmn145.mp4" || movies[0].CoverURL != "https://cdn.test/hmn145.jpg" {
		t.Fatalf("预告片/远程封面未入库: %+v", movies)
	}
}

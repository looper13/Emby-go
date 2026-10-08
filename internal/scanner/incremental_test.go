package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/store"
)

func writeScanFile(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func scanLibrary(t *testing.T, root string) (*store.Store, store.Library) {
	t.Helper()
	database := newStore(t, root)
	library, err := database.AddLibrary("incremental", root)
	if err != nil {
		t.Fatal(err)
	}
	return database, library
}

func requireScan(t *testing.T, database *store.Store, library store.Library, want Result) {
	t.Helper()
	var progress Progress
	got, err := ScanWithProgress(database, library, func(current Progress) { progress = current })
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("scan result = %+v, want %+v", got, want)
	}
	if progress.Done != progress.Total || progress.Result != got {
		t.Fatalf("final progress = %+v, result = %+v", progress, got)
	}
}

func TestIncrementalUnchangedSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title><actor><name>Actor</name></actor></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	before := visible(t, database)[0]
	version := database.Version("g:version")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	time.Sleep(1100 * time.Millisecond)
	requireScan(t, reopened, library, Result{Skipped: 1})
	after := visible(t, reopened)[0]
	if before.ID != after.ID || before.UpdatedAt != after.UpdatedAt || reopened.Version("g:version") != version {
		t.Fatalf("unchanged movie or version was rewritten: before=%+v after=%+v", before, after)
	}
	result, err := RebuildWithProgress(reopened, library, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("full rebuild = %+v, %v", result, err)
	}
	if rebuilt := visible(t, reopened)[0]; rebuilt.ID != before.ID || rebuilt.UpdatedAt == before.UpdatedAt {
		t.Fatalf("full rebuild should retain identity and refresh metadata: %+v", rebuilt)
	}
	requireScan(t, reopened, library, Result{Skipped: 1})
}

func TestIncrementalReconcilesChanges(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	writeScanFile(t, root, "b.strm", "http://media.test/b.mp4\n")
	writeScanFile(t, root, "b.nfo", "<movie><title>B</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	writeScanFile(t, root, "a.nfo", "<movie><title>Changed</title></movie>")
	writeScanFile(t, root, "new.strm", "http://media.test/new.mp4\n")
	requireScan(t, database, library, Result{Added: 1, Updated: 1, Skipped: 1, Success: 1, Pending: 1})
	if movies := visible(t, database); len(movies) != 2 || movies[1].Title != "Changed" {
		t.Fatalf("NFO change not indexed: %+v", movies)
	}
	if err := os.Rename(filepath.Join(root, "a.strm"), filepath.Join(root, "moved.strm")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "b.strm")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Added: 1, Deleted: 2, Skipped: 1, Pending: 1})
	requireScan(t, database, library, Result{Skipped: 2})
	writeScanFile(t, root, "moved.strm", "ed2k://file/movie\n")
	requireScan(t, database, library, Result{Updated: 1, Incompatible: 1, Skipped: 1})
}

func TestIncrementalTracksNFOAndImages(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Pending: 1})
	requireScan(t, database, library, Result{Skipped: 1})
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	for _, name := range []string{"cover.jpg", "poster.jpg", "poster.webp", "fanart.png", "landscape.jpg"} {
		writeScanFile(t, root, name, "image")
		requireScan(t, database, library, Result{Updated: 1, Success: 1})
		requireScan(t, database, library, Result{Skipped: 1})
	}
	writeScanFile(t, root, "poster.webp", "changed image")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; movie.PosterPath != filepath.Join(root, "poster.webp") || movie.BackdropPath != filepath.Join(root, "fanart.png") {
		t.Fatalf("image selection = %+v", movie)
	}
	if err := os.Remove(filepath.Join(root, "poster.webp")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; movie.PosterPath != filepath.Join(root, "poster.jpg") {
		t.Fatalf("poster fallback = %s", movie.PosterPath)
	}
	if err := os.Remove(filepath.Join(root, "a.nfo")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Pending: 1})
	requireScan(t, database, library, Result{Skipped: 1})
}

func TestIncrementalSubsecondChange(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	path := filepath.Join(root, "a.nfo")
	stamp := time.Unix(1700000000, 100000000)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	writeScanFile(t, root, "a.nfo", "<movie><title>B</title></movie>")
	stamp = stamp.Add(100 * time.Millisecond)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; movie.Title != "B" {
		t.Fatalf("subsecond NFO change missed: %+v", movie)
	}
}

func TestIncrementalCDGroupChanges(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "Movie-CD2.strm", "http://media.test/cd2.mp4\n")
	writeScanFile(t, root, "Movie.nfo", "<movie><title>Movie</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Pending: 1})
	writeScanFile(t, root, "Movie-CD1.strm", "http://media.test/cd1.mp4\n")
	requireScan(t, database, library, Result{Added: 1, Deleted: 1, Success: 1})
	writeScanFile(t, root, "Movie-CD3.strm", "http://media.test/cd3.mp4\n")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	writeScanFile(t, root, "Movie-CD2.strm", "http://media.test/cd2-new.mp4\n")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	writeScanFile(t, root, "Movie.nfo", "<movie><title>Updated fallback</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	writeScanFile(t, root, "Movie-CD1.nfo", "<movie><title>Primary</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; movie.Title != "Primary" || len(movie.AdditionalParts) != 2 {
		t.Fatalf("CD grouping = %+v", movie)
	}
	if err := os.Remove(filepath.Join(root, "Movie-CD3.strm")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if movie := visible(t, database)[0]; len(movie.AdditionalParts) != 1 {
		t.Fatalf("removed CD part remained: %+v", movie)
	}
	if err := os.Remove(filepath.Join(root, "Movie-CD1.strm")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Added: 1, Deleted: 1, Pending: 1})
	requireScan(t, database, library, Result{Skipped: 1})
}

func TestIncrementalFailureRetriesAndProtectsIndex(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	writeScanFile(t, root, "b.strm", "http://media.test/b.mp4\n")
	writeScanFile(t, root, "b.nfo", "<movie><title>B</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	writeScanFile(t, root, "a.strm", "")
	if err := os.Remove(filepath.Join(root, "b.strm")); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Failed: 1})
	requireScan(t, database, library, Result{Failed: 1})
	if movies := visible(t, database); len(movies) != 2 {
		t.Fatalf("failed scan removed existing index: %+v", movies)
	}
	writeScanFile(t, root, "a.strm", "http://media.test/recovered.mp4\n")
	requireScan(t, database, library, Result{Updated: 1, Deleted: 1, Success: 1})
	if err := os.Remove(filepath.Join(root, "a.nfo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a.nfo"), 0755); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Failed: 1})
	if movies := visible(t, database); len(movies) != 1 {
		t.Fatalf("unreadable NFO overwrote existing index: %+v", movies)
	}
	if err := os.Remove(filepath.Join(root, "a.nfo")); err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "a.nfo", "<movie><title>Recovered</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	library.Path = filepath.Join(root, "offline")
	if _, err := Scan(database, library); err == nil {
		t.Fatal("missing library root should fail")
	}
	library.Path = filepath.Join(root, "a.strm")
	if _, err := RebuildWithProgress(database, library, nil); err == nil {
		t.Fatal("non-directory library root should fail")
	}
	if movies := visible(t, database); len(movies) != 1 || movies[0].Title != "Recovered" {
		t.Fatalf("offline library lost index: %+v", movies)
	}
}

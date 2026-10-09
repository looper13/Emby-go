package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshFilesUnicodeCDAndLiteralPrefixDeletion(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"ÄMovie-CD1", "ÄMovie-CD2", "Other", "a_b", "axb", "a%b", "azb", "bang!a"} {
		writeScanFile(t, root, name+".strm", "http://media.test/movie.mp4")
		writeScanFile(t, root, name+".nfo", "<movie><title>Old</title></movie>")
	}
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 7, Success: 7})
	if err := os.Remove(filepath.Join(root, "ÄMovie-CD1.strm")); err != nil {
		t.Fatal(err)
	}
	result, err := RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "ÄMovie-CD1.strm")}, nil)
	if err != nil || result != (Result{Added: 1, Deleted: 1, Success: 1}) {
		t.Fatalf("Unicode CD refresh: %+v %v", result, err)
	}
	for _, name := range []string{"a_b", "a%b", "bang!a"} {
		path := filepath.Join(root, name+".strm")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		result, err := RefreshFiles(context.Background(), database, library, []string{path}, nil)
		if err != nil || result != (Result{Deleted: 1}) {
			t.Fatalf("literal deletion %q: %+v %v", name, result, err)
		}
	}
	if len(visible(t, database)) != 4 {
		t.Fatal("targeted refresh removed an unrelated source")
	}
}

func TestRefreshFilesIsLocalAndHonorsEvents(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "child/c"} {
		writeScanFile(t, root, name+".strm", "http://media.test/movie.mp4\n")
		writeScanFile(t, root, name+".nfo", "<movie><title>Old</title></movie>")
	}
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 3, Success: 3})
	aPath := filepath.Join(root, "a.nfo")
	before, err := os.Stat(aPath)
	if err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "a.nfo", "<movie><title>New</title></movie>")
	if err := os.Chtimes(aPath, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, root, "b.nfo", "<movie><title>Untouched</title></movie>")
	writeScanFile(t, root, "child/c.strm", "")
	result, err := RefreshFiles(context.Background(), database, library, []string{aPath}, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("local file refresh = %+v, %v", result, err)
	}
	for _, movie := range visible(t, database) {
		want := "Old"
		if filepath.Base(movie.SourcePath) == "a.strm" {
			want = "New"
		}
		if movie.Title != want {
			t.Fatalf("unexpected refresh outside event group: %+v", movie)
		}
	}
	if err := os.Remove(filepath.Join(root, "b.strm")); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshFiles(context.Background(), database, library, []string{aPath}, nil); err != nil {
		t.Fatal(err)
	}
	if len(visible(t, database)) != 3 {
		t.Fatal("single-file event purged an unrelated source")
	}
	result, err = RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "b.strm")}, nil)
	if err != nil || result != (Result{Deleted: 1}) {
		t.Fatalf("local deletion = %+v, %v", result, err)
	}
}

func TestRefreshFilesImageAndUppercaseSTRM(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.STRM", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	writeScanFile(t, root, "child/b.strm", "http://media.test/b.mp4\n")
	writeScanFile(t, root, "child/b.nfo", "<movie><title>B</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	result, err := RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "a.nfo")}, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("uppercase source matching = %+v, %v", result, err)
	}
	writeScanFile(t, root, "poster.jpg", "new image")
	result, err = RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "poster.jpg")}, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("image refresh crossed directory boundary: %+v, %v", result, err)
	}
}

func TestRefreshFilesCDDeletionAndFallback(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Movie-CD1", "Movie-CD2", "Other"} {
		writeScanFile(t, root, name+".strm", "http://media.test/movie.mp4\n")
	}
	writeScanFile(t, root, "Movie.nfo", "<movie><title>Fallback</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 2, Success: 1, Pending: 1})
	result, err := RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "Movie.nfo")}, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("fallback NFO group = %+v, %v", result, err)
	}
	if err := os.Remove(filepath.Join(root, "Movie-CD1.strm")); err != nil {
		t.Fatal(err)
	}
	result, err = RefreshFiles(context.Background(), database, library, []string{filepath.Join(root, "Movie-CD1.strm")}, nil)
	if err != nil || result != (Result{Added: 1, Deleted: 1, Pending: 1}) {
		t.Fatalf("CD1 deletion = %+v, %v", result, err)
	}
	fingerprints, err := database.ScanFingerprints(library.ID)
	if err != nil || len(fingerprints) != 2 {
		t.Fatalf("unrelated movie was lost: %+v, %v", fingerprints, err)
	}
}

func TestRefreshDirectoryScopesAndBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one/a", "one/child/b", "one-more/c"} {
		writeScanFile(t, root, name+".strm", "http://media.test/movie.mp4\n")
		writeScanFile(t, root, name+".nfo", "<movie><title>Movie</title></movie>")
	}
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 3, Success: 3})
	for _, name := range []string{"one/a.strm", "one/a.nfo", "one/child/b.strm", "one/child/b.nfo", "one/child", "one"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := RefreshDirectory(context.Background(), database, library, filepath.Join(root, "one"), true, nil)
	if err != nil || result != (Result{Deleted: 2}) || len(visible(t, database)) != 1 {
		t.Fatalf("subtree deletion escaped prefix: %+v, %v", result, err)
	}
	if _, err := RefreshDirectory(context.Background(), database, library, filepath.Dir(root), true, nil); err == nil {
		t.Fatal("outside directory should be rejected")
	}
	if _, err := RefreshFiles(context.Background(), database, library, []string{filepath.Join(filepath.Dir(root), "outside.strm")}, nil); err == nil {
		t.Fatal("outside file should be rejected")
	}
	library.Path = filepath.Join(root, "offline")
	if _, err := RefreshDirectory(context.Background(), database, library, library.Path, true, nil); err == nil {
		t.Fatal("offline root must not purge index")
	}
}

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMovieImagesRemainIsolatedAcrossScanAndEvents(t *testing.T) {
	root := t.TempDir()
	// Exercise the large-directory stability check as well as shared snapshots.
	for index := 0; index < 65; index++ {
		writeScanFile(t, root, fmt.Sprintf("unrelated-%d.txt", index), "unused")
	}
	for _, name := range []string{"a", "b"} {
		writeScanFile(t, root, name+".strm", "http://media.test/movie.mp4")
		writeScanFile(t, root, name+".nfo", "<movie><title>"+name+"</title></movie>")
		for _, kind := range []string{"poster", "fanart", "landscape"} {
			writeScanFile(t, root, name+"-"+kind+".webp", name)
		}
	}
	writeScanFile(t, root, "poster.jpg", "legacy cover")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	for _, movie := range visible(t, database) {
		name := movie.Title
		if movie.PosterPath != filepath.Join(root, name+"-poster.webp") || movie.BackdropPath != filepath.Join(root, name+"-fanart.webp") || movie.LandscapePath != filepath.Join(root, name+"-landscape.webp") {
			t.Fatalf("wrong art: %+v", movie)
		}
	}
	// Even a forced event refresh of one poster must not reindex its sibling.
	writeScanFile(t, root, "a-poster.webp", "new")
	result, err := RefreshFiles(database, library, []string{filepath.Join(root, "a-poster.webp")}, nil)
	if err != nil || result != (Result{Updated: 1, Success: 1}) {
		t.Fatalf("art event scope: %+v, %v", result, err)
	}
	// Removing private art restores the legacy directory fallback.
	if err := os.Remove(filepath.Join(root, "a-poster.webp")); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshFiles(database, library, []string{filepath.Join(root, "a-poster.webp")}, nil); err != nil {
		t.Fatal(err)
	}
	for _, movie := range visible(t, database) {
		if movie.Title == "a" && movie.PosterPath != filepath.Join(root, "poster.jpg") {
			t.Fatalf("legacy fallback missing: %+v", movie)
		}
		if movie.Title == "b" && movie.PosterPath != filepath.Join(root, "b-poster.webp") {
			t.Fatal("sibling art changed")
		}
	}
}

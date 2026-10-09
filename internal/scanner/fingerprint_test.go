package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadataFingerprintIncludesPrimaryAndFallbackNFO(t *testing.T) {
	root := t.TempDir()
	source, fallback := filepath.Join(root, "movie-CD1.strm"), filepath.Join(root, "movie.nfo")
	state := func() string {
		t.Helper()
		snapshot, err := readMetadataState(source, fallback)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot.Fingerprint
	}
	missing := state()
	writeScanFile(t, root, "movie.nfo", "fallback")
	withFallback := state()
	writeScanFile(t, root, "movie-CD1.nfo", "primary")
	withPrimary := state()
	if missing == withFallback || withFallback == withPrimary || !strings.HasPrefix(withPrimary, "nfo-v3:") {
		t.Fatal("NFO dependencies did not affect fingerprint")
	}
	writeScanFile(t, root, "movie-CD1.strm", "changed source")
	writeScanFile(t, root, "poster.jpg", "changed image")
	writeScanFile(t, root, "extrafanart/fanart1.jpg", "changed still")
	if state() != withPrimary {
		t.Fatal("non-NFO dependency affected metadata fingerprint")
	}
	if err := os.Remove(filepath.Join(root, "movie-CD1.nfo")); err != nil {
		t.Fatal(err)
	}
	if state() != withFallback {
		t.Fatal("removed primary NFO was not detected")
	}
}

func TestMetadataFingerprintUpgradesOnce(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "https://media.test/a.mp4")
	writeScanFile(t, root, "a.nfo", "<movie><title>A</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	if err := database.SaveScanFingerprint(library.ID, filepath.Join(root, "a.strm"), "v2:legacy"); err != nil {
		t.Fatal(err)
	}
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	requireScan(t, database, library, Result{Skipped: 1})
}

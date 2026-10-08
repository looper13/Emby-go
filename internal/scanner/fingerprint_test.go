package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"emby-go/internal/imageutil"
)

func legacyScanFingerprint(test *testing.T, path string, parts []string, fallbackNFO string) string {
	test.Helper()
	nfoPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	dependencies := append([]string{path}, parts...)
	dependencies = append(dependencies, nfoPath)
	optional := map[string]bool{nfoPath: true}
	if fallbackNFO != "" && fallbackNFO != nfoPath {
		dependencies = append(dependencies, fallbackNFO)
		optional[fallbackNFO] = true
	}
	directory := filepath.Dir(path)
	for _, image := range []string{imageutil.FindPoster(directory), imageutil.FindImage(directory, "fanart"), imageutil.FindImage(directory, "landscape")} {
		if image != "" {
			dependencies = append(dependencies, image)
		}
	}
	sort.Strings(dependencies)
	stamps := make([]fileStamp, 0, len(dependencies))
	for _, dependency := range dependencies {
		stamp := fileStamp{Path: dependency}
		info, err := os.Stat(dependency)
		if os.IsNotExist(err) && optional[dependency] {
			stamp.Missing = true
		} else if err != nil {
			test.Fatal(err)
		} else {
			stamp.Size, stamp.Modified, stamp.Mode = info.Size(), info.ModTime().UnixNano(), info.Mode()
		}
		stamps = append(stamps, stamp)
	}
	payload, err := json.Marshal(stamps)
	if err != nil {
		test.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	return "v1:" + hex.EncodeToString(digest[:])
}

func TestOptimizedFingerprintMatchesExistingVersion(test *testing.T) {
	root := test.TempDir()
	for _, name := range []string{"movie-CD1.strm", "movie-CD2.strm", "movie.nfo", "folder.JPG", "fanart.png", "landscape.WebP"} {
		writeScanFile(test, root, name, "fixture")
	}
	path := filepath.Join(root, "movie-CD1.strm")
	parts := []string{filepath.Join(root, "movie-CD2.strm")}
	fallback := filepath.Join(root, "movie.nfo")
	directory := &imageDirectory{}
	for _, additions := range [][]string{nil, {"movie-CD1.nfo"}, {"poster.webp"}} {
		for _, name := range additions {
			writeScanFile(test, root, name, "added")
		}
		state, err := readSourceState(path, parts, fallback, directory)
		if err != nil {
			test.Fatal(err)
		}
		if want := legacyScanFingerprint(test, path, parts, fallback); state.Fingerprint != want {
			test.Fatalf("fingerprint format changed: got=%s want=%s", state.Fingerprint, want)
		}
	}
}

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/nfo"
)

func TestOversizedNFORetainsIndexAndRetriesAfterRepair(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "a.strm", "http://media.test/a.mp4\n")
	writeScanFile(t, root, "a.nfo", "<movie><title>Original</title></movie>")
	database, library := scanLibrary(t, root)
	requireScan(t, database, library, Result{Added: 1, Success: 1})
	before := visible(t, database)[0]
	path := filepath.Join(root, "a.nfo")
	if err := os.Truncate(path, nfo.MaxFileSize+1); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), database, library)
	if err != nil || result.Failed != 1 || result.Deleted != 0 || len(result.Failures) != 1 {
		t.Fatalf("oversized read should be a retryable per-file failure: result=%+v err=%v", result, err)
	}
	if result.Failures[0].Path != path || !strings.Contains(result.Failures[0].Error, "8 MiB") {
		t.Fatalf("failure lacks actionable path and cause: %+v", result.Failures)
	}
	after := visible(t, database)[0]
	if after.ID != before.ID || after.Title != before.Title || after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("oversized NFO changed the indexed movie: before=%+v after=%+v", before, after)
	}
	writeScanFile(t, root, "a.nfo", "<movie><title>Repaired</title></movie>")
	requireScan(t, database, library, Result{Updated: 1, Success: 1})
	if repaired := visible(t, database)[0]; repaired.ID != before.ID || repaired.Title != "Repaired" {
		t.Fatalf("repair did not retry the same movie: %+v", repaired)
	}
}

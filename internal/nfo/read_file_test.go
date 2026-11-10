package nfo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"emby-go/internal/regularfile"
)

func TestNFOSizeBoundaryPreservesOversizedDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.nfo")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxFileSize); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadFile(path); err != nil || len(data) != MaxFileSize {
		t.Fatalf("exact size limit rejected: bytes=%d err=%v", len(data), err)
	}
	if err := os.Truncate(path, MaxFileSize+1); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() error{
		"read": func() error { _, err := Read(path); return err },
		"stats": func() error {
			_, stats, err := ReadWithStats(path)
			if stats.Reads != 1 || stats.Parses != 0 {
				t.Errorf("oversized NFO must fail before parsing: %+v", stats)
			}
			return err
		},
		"scrape":   func() error { return UpdateScraped(path, ScrapeFields{Title: "New title", Overwrite: true}) },
		"fileinfo": func() error { return SaveFileInfo(path, FileInfoMeta{}, &StreamDetails{}) },
		"actor":    func() error { _, err := SetActorThumb(path, "Actor", "https://image.test/a.jpg"); return err },
		"preview":  func() error { _, err := TagPreview(path, []string{"title"}); return err },
		"list":     func() error { _, _, err := ListPreview(path); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := read(); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("expected size limit: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Size() != MaxFileSize+1 {
				t.Fatalf("oversized document was changed: info=%v err=%v", info, err)
			}
		})
	}
}

func TestNFORejectsDirectoryBeforeReading(t *testing.T) {
	if _, err := Read(t.TempDir()); !errors.Is(err, regularfile.ErrNotRegular) {
		t.Fatalf("expected regular file boundary: %v", err)
	}
}

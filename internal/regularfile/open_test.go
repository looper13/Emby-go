package regularfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRegularFileAndRejectDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "document")
	if err := os.WriteFile(path, []byte("document"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(data) != "document" {
		t.Fatalf("regular read changed: %q %v", data, err)
	}
	if _, err := Open(root); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("directory should be rejected: %v", err)
	}
	if _, err := Open(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-file contract changed: %v", err)
	}
}

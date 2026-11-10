//go:build unix

package regularfile

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOpenRejectsFIFOWithoutWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("FIFO should be rejected before blocking: %v", err)
	}
}

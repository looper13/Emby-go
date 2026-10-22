package imageutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeLimitRejectsHugeHeaderBeforePixelAllocation(t *testing.T) {
	header := append([]byte("GIF89a"), make([]byte, 7)...)
	binary.LittleEndian.PutUint16(header[6:8], 65535)
	binary.LittleEndian.PutUint16(header[8:10], 65535)
	if _, err := decodeLimited(bytes.NewReader(header)); !errors.Is(err, errImageTooLarge) {
		t.Fatalf("decode error=%v", err)
	}
	if err := EncodeWebPTo(bytes.NewReader(header), io.Discard); !errors.Is(err, errImageTooLarge) {
		t.Fatalf("encode error=%v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "oversize.gif")
	if err := os.WriteFile(src, header, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Thumbnail(src, 100, 100, 82); !errors.Is(err, errImageTooLarge) {
		t.Fatalf("thumbnail error=%v", err)
	}
}

package imageutil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestR18CatalogArtworkSizingAndDecodeLimit(t *testing.T) {
	for _, test := range []struct {
		w, h, maxWidth, maxHeight int
		poster                    bool
		wantWidth, wantHeight     int
	}{
		{800, 600, 100, 0, true, 100, 150},
		{565, 800, 100, 0, true, 100, 150},
		{400, 800, 0, 150, true, 100, 150},
		{800, 600, 100, 0, false, 100, 75},
		{400, 600, 800, 0, true, 400, 600},
	} {
		var source bytes.Buffer
		if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, test.w, test.h))); err != nil {
			t.Fatal(err)
		}
		data, err := CatalogArtwork(bytes.NewReader(source.Bytes()), test.poster, test.maxWidth, test.maxHeight, 82)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != test.wantWidth || config.Height != test.wantHeight {
			t.Fatal("invalid artwork size", config, test, err)
		}
	}
	header := append([]byte("GIF89a"), make([]byte, 7)...)
	binary.LittleEndian.PutUint16(header[6:8], 65535)
	binary.LittleEndian.PutUint16(header[8:10], 65535)
	if _, err := CatalogArtwork(bytes.NewReader(header), true, 100, 0, 82); !errors.Is(err, errImageTooLarge) {
		t.Fatal("pixel limit not enforced", err)
	}
}

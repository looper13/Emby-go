package imageutil

import (
	"image"
	"image/draw"
	"io"
)

// CatalogArtwork keeps a 2:3 poster crop consistent with the R18 web cards.
// Portraits are centered; a wide DVD jacket's front cover is on the right.
func CatalogArtwork(src io.ReadSeeker, poster bool, width, height, quality int) ([]byte, error) {
	img, err := decodeLimited(src)
	if err != nil {
		return nil, err
	}
	if poster {
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		crop := b
		if w*3 > h*2 {
			cropWidth := max(1, h*2/3)
			x := b.Min.X + (w-cropWidth)/2
			if float64(w) > float64(h)*1.2 {
				x = b.Max.X - cropWidth
			}
			crop = image.Rect(x, b.Min.Y, x+cropWidth, b.Max.Y)
		} else if w*3 < h*2 {
			cropHeight := max(1, w*3/2)
			y := b.Min.Y + (h-cropHeight)/2
			crop = image.Rect(b.Min.X, y, b.Max.X, y+cropHeight)
		}
		if sub, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			img = sub.SubImage(crop)
		} else {
			out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
			draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
			img = out
		}
	}
	return EncodeWebPBuffer(Downscale(img, width, height), quality)
}

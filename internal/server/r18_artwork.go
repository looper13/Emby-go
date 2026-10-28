package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"emby-go/internal/imageutil"
	"github.com/gin-gonic/gin"
)

type r18ArtworkSpec struct {
	transform              string
	width, height, quality int
}

func r18ArtworkParams(c *gin.Context, transform string) r18ArtworkSpec {
	width, height, quality := imageResizeParams(c)
	if transform == "" {
		width, height, quality = 0, 0, 0
	} else if transform != "poster" && width == 0 && height == 0 {
		transform, quality = "", 0
	} else if transform == "poster" && width == 0 && height == 0 {
		width, height = 320, 480
	}
	if transform != "" && quality == 0 {
		quality = 82
	}
	return r18ArtworkSpec{transform, width, height, quality}
}

func (spec r18ArtworkSpec) key() string {
	return spec.transform + ":" + strconv.Itoa(spec.width) + "x" + strconv.Itoa(spec.height) + ":" + strconv.Itoa(spec.quality)
}

func (a *App) makeR18Artwork(ctx context.Context, mime string, data []byte, spec r18ArtworkSpec) (string, []byte, error) {
	if spec.transform == "" || (spec.transform != "poster" && spec.width == 0 && spec.height == 0) {
		return mime, data, nil
	}
	select {
	case a.thumbSem <- struct{}{}:
		defer func() { <-a.thumbSem }()
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
	out, err := imageutil.CatalogArtwork(bytes.NewReader(data), spec.transform == "poster", spec.width, spec.height, spec.quality)
	if err != nil {
		// Formats without a local decoder (e.g. AVIF) still show the source.
		return mime, data, nil
	}
	return "image/webp", out, nil
}

func (a *App) respondR18Artwork(c *gin.Context, mime string, data []byte, transform string) {
	spec := r18ArtworkParams(c, transform)
	if spec.transform == "" || (spec.transform != "poster" && spec.width == 0 && spec.height == 0) {
		serveR18Image(c, mime, data)
		return
	}
	sum := sha256.Sum256(data)
	key := "r18art:" + hex.EncodeToString(sum[:]) + ":" + spec.key()
	if cached, ok := a.imgThumb.Get(key); ok {
		serveR18Image(c, "image/webp", cached)
		return
	}
	outMime, out, err := a.makeR18Artwork(c.Request.Context(), mime, data, spec)
	if err != nil {
		c.Status(502)
		return
	}
	if outMime == "image/webp" && len(out) <= r18ImageCacheLimit {
		a.imgThumb.Set(key, out, scrapeImageTTL)
	}
	serveR18Image(c, outMime, out)
}

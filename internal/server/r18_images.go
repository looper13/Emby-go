package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

var r18ImageHTTP = r18.NewImageClient()

const r18ImageLimit = 8 << 20
const r18ImageCacheLimit = 512 << 10

func r18VideoCacheKey(l store.Library, id string) string {
	return "r18meta:" + strconv.FormatInt(l.ID, 10) + ":" + strconv.FormatInt(l.Revision, 10) + ":" + id
}

func (a *App) r18Presentation(l store.Library, v r18.Video) r18.Video {
	if data, err := json.Marshal(v); err == nil {
		a.imgMeta.Set(r18VideoCacheKey(l, v.ID), data, imageMetaTTL)
	}
	base := "/api/r18/libraries/" + strconv.FormatInt(l.ID, 10) + "/videos/" + v.ID + "/images/"
	if v.CoverURL != "" {
		v.CoverURL = base + "poster"
	}
	images := make([]string, 0, len(v.Images))
	for i := range v.Images {
		images = append(images, base+strconv.Itoa(i))
	}
	v.Images = images
	return v
}

func (a *App) r18ImageVideo(ctx context.Context, l store.Library, id string) (r18.Video, error) {
	var v r18.Video
	if data, ok := a.imgMeta.Get(r18VideoCacheKey(l, id)); ok && json.Unmarshal(data, &v) == nil {
		return v, nil
	}
	client, err := a.r18Client(l)
	if err != nil {
		return v, err
	}
	v, err = client.Get(ctx, id)
	if err == nil {
		a.r18Presentation(l, v)
	}
	return v, err
}

func serveR18Image(c *gin.Context, mime string, data []byte) {
	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:])
	setPrivateImageCache(c)
	c.Header("ETag", `"`+tag+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if ifNoneMatchHit(c.GetHeader("If-None-Match"), tag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Length", strconv.Itoa(len(data)))
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Type", mime)
		c.Status(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, mime, data)
}

func (a *App) r18Image(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	id, kind := c.Param("video"), c.Param("image")
	if _, _, err := r18.DecodeID(id); err != nil || (kind != "cover" && kind != "poster" && kind != "0" && kind != "1") {
		c.Status(http.StatusNotFound)
		return
	}
	transform := ""
	if kind == "poster" {
		kind, transform = "cover", "poster"
	}
	a.serveR18SourceImage(c, l, id, kind, transform)
}

func (a *App) serveR18SourceImage(c *gin.Context, l store.Library, id, kind, transform string) {
	c.Header("Cache-Control", "no-store")
	respond := func(mime string, data []byte) { a.respondR18Artwork(c, mime, data, transform) }
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(a.rootCtx, cancel)
	defer stop()
	v, err := a.r18ImageVideo(ctx, l, id)
	if err != nil {
		a.r18Failure(c, "图片资料查询", err)
		return
	}
	raw := v.CoverURL
	if kind != "cover" {
		i, _ := strconv.Atoi(kind)
		raw = ""
		if i < len(v.Images) {
			raw = v.Images[i]
		}
	}
	if r18.SafeHTTPURL(raw) == "" {
		c.Status(http.StatusNotFound)
		return
	}
	// No user-supplied URL: only artwork stored on this authorized item can be fetched.
	sum := sha256.Sum256([]byte(r18VideoCacheKey(l, id) + ":" + raw))
	if kind == "cover" {
		a.serveR18Cover(c, ctx, l, id, raw, hex.EncodeToString(sum[:]), r18ArtworkParams(c, transform))
		return
	}
	key := "r18img:" + hex.EncodeToString(sum[:])
	serveCached := func() bool {
		if data, ok := a.imgThumb.Get(key); ok {
			mime, body, found := bytes.Cut(data, []byte{'\n'})
			if found {
				respond(string(mime), body)
				return true
			}
		}
		return false
	}
	if serveCached() {
		return
	}
	mime, data, err := a.fetchR18Image(ctx, l, id, kind, raw, key)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	if len(data) <= r18ImageCacheLimit {
		a.imgThumb.Set(key, packR18Image(mime, data), scrapeImageTTL)
	}
	respond(mime, data)
}

func packR18Image(mime string, data []byte) []byte {
	return append([]byte(mime+"\n"), data...)
}

func unpackR18Image(data []byte) (string, []byte, error) {
	mime, body, ok := bytes.Cut(data, []byte{'\n'})
	if ok && len(body) > 0 {
		switch string(mime) {
		case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif":
			return string(mime), body, nil
		}
	}
	return "", nil, errors.New("invalid R18 cached image")
}

func (a *App) serveR18Cover(c *gin.Context, ctx context.Context, l store.Library, id, raw, sourceKey string, spec r18ArtworkSpec) {
	key := "r18cover:v2:" + sourceKey
	loadSource := func() (cache.FileValue, error) {
		return a.loadR18Cover(ctx, key+":original", func() (cache.FileValue, error) {
			mime, data, err := a.fetchR18Image(ctx, l, id, "cover", raw, "")
			return cache.FileValue{Data: packR18Image(mime, data)}, err
		})
	}
	var value cache.FileValue
	var err error
	if spec.transform == "" {
		value, err = loadSource()
	} else {
		// A derived hit is served before the original is read or downloaded.
		value, err = a.loadR18Cover(ctx, key+":"+spec.key(), func() (cache.FileValue, error) {
			original, err := loadSource()
			if err != nil {
				return cache.FileValue{}, err
			}
			mime, body, err := unpackR18Image(original.Data)
			if err != nil {
				return cache.FileValue{}, err
			}
			mime, body, err = a.makeR18Artwork(ctx, mime, body, spec)
			return cache.FileValue{Data: packR18Image(mime, body), Expires: original.Expires}, err
		})
	}
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	mime, body, err := unpackR18Image(value.Data)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	serveR18Image(c, mime, body)
}

func (a *App) fetchR18Image(ctx context.Context, l store.Library, id, kind, raw, sharedKey string) (string, []byte, error) {
	select {
	case a.scrapeSem <- struct{}{}:
		defer func() { <-a.scrapeSem }()
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
	if sharedKey != "" {
		if cached, ok := a.imgThumb.Get(sharedKey); ok {
			if mime, body, err := unpackR18Image(cached); err == nil {
				return mime, body, nil
			}
		}
	}
	client := a.r18ImageHTTP
	if client == nil {
		client = r18ImageHTTP
	}
	req, err := r18.ImageRequest(ctx, raw)
	if err != nil {
		return "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		// Log the original cause without signed image URLs or browser credentials.
		if upstream, ok := err.(*url.Error); ok {
			err = upstream.Err
		}
		slog.Warn("R18 image proxy failed", "library_id", l.ID, "video_id", id, "image", kind, "stage", "fetch", "host", req.URL.Hostname(), "error", err.Error())
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > r18ImageLimit {
		slog.Warn("R18 image proxy rejected response", "library_id", l.ID, "video_id", id, "image", kind, "stage", "response", "status", resp.StatusCode, "size", resp.ContentLength)
		return "", nil, errors.New("R18 upstream image rejected")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, r18ImageLimit+1))
	if err != nil || len(data) == 0 || len(data) > r18ImageLimit {
		slog.Warn("R18 image proxy rejected body", "library_id", l.ID, "video_id", id, "image", kind, "stage", "body", "size", len(data), "error", err)
		return "", nil, errors.New("R18 upstream image body rejected")
	}
	mime := http.DetectContentType(data)
	if len(data) >= 16 && string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis") {
		mime = "image/avif"
	}
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif":
	default:
		slog.Warn("R18 image proxy rejected type", "library_id", l.ID, "video_id", id, "image", kind, "stage", "type", "type", mime)
		return "", nil, errors.New("R18 upstream image type rejected")
	}
	return mime, data, nil
}

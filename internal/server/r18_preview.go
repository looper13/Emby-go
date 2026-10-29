package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

var r18PreviewHTTP = r18.NewPreviewClient()

const r18PreviewResourceLimit = 64 << 20

func r18PreviewItem(l store.Library, id string) string {
	return "r18-preview:" + strconv.FormatInt(l.ID, 10) + ":" + strconv.FormatInt(l.Revision, 10) + ":" + id
}

func r18PreviewBase(l store.Library, id string) string {
	return "/api/r18/libraries/" + strconv.FormatInt(l.ID, 10) + "/videos/" + id
}

func r18PreviewExtension(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && strings.EqualFold(path.Ext(u.Path), ".mp4") {
		return ".mp4"
	}
	return ".m3u8"
}

func (a *App) r18PreviewContext(c *gin.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	stop := context.AfterFunc(a.rootCtx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (a *App) r18PreviewVideo(c *gin.Context, ctx context.Context, l store.Library, id string) (r18.Video, bool) {
	_, service, err := r18.DecodeID(id)
	if err != nil || service != "javdb" {
		c.Status(http.StatusNotFound)
		return r18.Video{}, false
	}
	// 只复用完整详情；稀疏列表不能误判已经存在的预览视频。
	v, found := a.cachedR18Video(l, id, true)
	if !found {
		v, err = a.loadR18ImageDetail(ctx, l, id)
		if err != nil {
			a.r18Failure(c, "预览资料查询", err)
			return v, false
		}
	}
	if !v.HasPreview || r18.SafeHTTPURL(v.PreviewURL) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "当前影片暂无可用预览视频"})
		return v, false
	}
	return v, true
}

func (a *App) r18Preview(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	ctx, cancel := a.r18PreviewContext(c)
	defer cancel()
	id := c.Param("video")
	v, ok := a.r18PreviewVideo(c, ctx, l, id)
	if !ok {
		return
	}
	a.serveR18Preview(c, ctx, l, id, "", v.PreviewURL, r18PreviewExtension(v.PreviewURL) == ".m3u8")
}

func (a *App) r18PreviewResource(c *gin.Context) {
	l, ok := a.r18Library(c)
	if !ok {
		return
	}
	id, sessionID := c.Param("video"), c.Param("session")
	raw, userID, ok := a.hlsProxyTarget(r18PreviewItem(l, id), sessionID, c.Param("resource"))
	if !ok || userID != a.db.UserID() {
		c.Status(http.StatusNotFound)
		return
	}
	ctx, cancel := a.r18PreviewContext(c)
	defer cancel()
	if _, ok := a.r18PreviewVideo(c, ctx, l, id); !ok {
		return
	}
	a.serveR18Preview(c, ctx, l, id, sessionID, raw, false)
}

func r18PreviewPlaylistSource(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (strings.EqualFold(path.Ext(u.Path), ".m3u8") || strings.EqualFold(path.Ext(u.Path), ".m3u"))
}

func r18PreviewTarget(ctx context.Context, base *url.URL, raw string) (string, error) {
	ref, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("无效的预览资源引用")
	}
	target := base.ResolveReference(ref).String()
	if _, err = r18.PreviewRequest(ctx, target, ""); err != nil {
		return "", err
	}
	return target, nil
}

func validateR18PreviewPlaylist(ctx context.Context, base string, body []byte) error {
	baseURL, err := url.Parse(base)
	if err != nil || r18.SafeHTTPURL(base) == "" {
		return errors.New("无效的预览清单地址")
	}
	count, byteCount := 0, 0
	_, err = rewriteHLSPlaylistURIs(body, func(raw string) (string, error) {
		target, err := r18PreviewTarget(ctx, baseURL, raw)
		if err != nil {
			return "", err
		}
		count++
		byteCount += len(target)
		if count > maxHLSResourceCount || byteCount > maxHLSResourceBytes {
			return "", errors.New("预览资源数量或地址大小超过限制")
		}
		return raw, nil
	})
	return err
}

func (a *App) rewriteR18Preview(c *gin.Context, l store.Library, id, sessionID, base string, body []byte) ([]byte, error) {
	baseURL, err := url.Parse(base)
	if err != nil || r18.SafeHTTPURL(base) == "" {
		return nil, errors.New("无效的预览清单地址")
	}
	return rewriteHLSPlaylistURIs(body, func(raw string) (string, error) {
		target, err := r18PreviewTarget(c.Request.Context(), baseURL, raw)
		if err != nil {
			return "", err
		}
		resourceID, err := a.addHLSProxyResource(r18PreviewItem(l, id), sessionID, target)
		if err != nil {
			return "", err
		}
		return r18PreviewBase(l, id) + "/preview/" + sessionID + "/" + resourceID, nil
	})
}

func r18PreviewError(c *gin.Context) {
	// 不回显带签名的 CDN 地址、重定向目标或上游正文。
	c.JSON(http.StatusBadGateway, gin.H{"error": "预览视频暂不可用或地址已失效，请刷新详情后重试"})
}

func (a *App) serveR18Preview(c *gin.Context, ctx context.Context, l store.Library, id, sessionID, raw string, playlist bool) {
	byteRange := c.GetHeader("Range")
	if playlist || r18PreviewPlaylistSource(raw) {
		byteRange = ""
	}
	req, err := r18.PreviewRequest(ctx, raw, byteRange)
	if err != nil {
		r18PreviewError(c)
		return
	}
	resp, err := r18PreviewHTTP.Do(req)
	if err != nil {
		r18PreviewError(c)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		c.Header("Content-Range", resp.Header.Get("Content-Range"))
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		r18PreviewError(c)
		return
	}
	reader := bufio.NewReader(resp.Body)
	prefix, readErr := reader.Peek(512)
	if readErr != nil && readErr != io.EOF && readErr != bufio.ErrBufferFull {
		r18PreviewError(c)
		return
	}
	playlist = playlist || isHLSResponse(resp) || bytes.HasPrefix(bytes.TrimSpace(prefix), []byte("#EXTM3U"))
	if playlist {
		if resp.StatusCode != http.StatusOK {
			r18PreviewError(c)
			return
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxHLSPlaylistBytes+1))
		if err != nil || len(body) > maxHLSPlaylistBytes {
			r18PreviewError(c)
			return
		}
		base := raw
		if resp.Request != nil && resp.Request.URL != nil {
			base = resp.Request.URL.String()
		}
		// 完整预检后再分配，坏清单不能占用容量或驱逐已有正片会话。
		if err = validateR18PreviewPlaylist(ctx, base, body); err != nil {
			r18PreviewError(c)
			return
		}
		if c.Request.Method == http.MethodHead {
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.Status(http.StatusOK)
			return
		}
		if sessionID == "" {
			sessionID, err = a.newHLSProxySession(r18PreviewItem(l, id))
			if err != nil {
				r18PreviewError(c)
				return
			}
		}
		body, err = a.rewriteR18Preview(c, l, id, sessionID, base, body)
		if err != nil {
			r18PreviewError(c)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Content-Length", strconv.Itoa(len(body)))
		c.Status(http.StatusOK)
		if c.Request.Method != http.MethodHead {
			_, _ = c.Writer.Write(body)
		}
		return
	}
	contentType, parameters, typeErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	detected := http.DetectContentType(prefix)
	if resp.ContentLength > r18PreviewResourceLimit || strings.Contains(detected, "text/html") || strings.Contains(detected, "xml") || !r18PreviewMediaType(contentType) || (typeErr != nil && resp.Header.Get("Content-Type") != "") {
		r18PreviewError(c)
		return
	}
	if contentType == "multipart/byteranges" && parameters["boundary"] == "" {
		r18PreviewError(c)
		return
	}
	if contentType == "" || contentType == "text/plain" {
		contentType = "application/octet-stream"
	} else {
		contentType = mime.FormatMediaType(contentType, parameters)
	}
	var body io.Reader = reader
	if resp.ContentLength < 0 && c.Request.Method != http.MethodHead {
		// 无长度的响应先有界读取；超限或中断不能伪装成完整的 200 视频。
		data, err := io.ReadAll(io.LimitReader(reader, r18PreviewResourceLimit+1))
		if err != nil || len(data) > r18PreviewResourceLimit {
			r18PreviewError(c)
			return
		}
		resp.ContentLength = int64(len(data))
		body = bytes.NewReader(data)
	}
	for _, header := range []string{"Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := resp.Header.Get(header); value != "" {
			c.Header(header, value)
		}
	}
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	if resp.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	c.Status(resp.StatusCode)
	if c.Request.Method != http.MethodHead {
		_, _ = io.Copy(c.Writer, io.LimitReader(body, r18PreviewResourceLimit))
	}
}

func r18PreviewMediaType(value string) bool {
	return strings.HasPrefix(value, "video/") || strings.HasPrefix(value, "audio/") || value == "" || value == "application/octet-stream" || value == "binary/octet-stream" || value == "application/mp4" || value == "text/plain" || value == "text/vtt" || value == "multipart/byteranges"
}

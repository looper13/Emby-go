package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

var webUIFiles = loadWebUIAssets()

var webUIHashedName = regexp.MustCompile(`[-.][A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

type webUIBuildInfo struct {
	SchemaVersion int    `json:"schemaVersion"`
	BuiltAt       string `json:"builtAt"`
	Source        struct {
		Commit string `json:"commit"`
		Dirty  bool   `json:"dirty"`
	} `json:"source"`
	Files []webUIBuildFile `json:"files"`
}

type webUIBuildFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Immutable bool   `json:"immutable"`
}

func registerWebUIRoutes(r gin.IRoutes, files fs.FS) {
	handler := webUIHandler(files)
	for _, entry := range []string{"/", "/web", "/admin", "/web/index.html"} {
		r.GET(entry, handler)
		r.HEAD(entry, handler)
	}
	redirect := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		target := "/admin"
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		// Browsers preserve the original fragment when Location has no fragment.
		c.Redirect(http.StatusTemporaryRedirect, target)
	}
	r.GET("/admin-vue", redirect)
	r.HEAD("/admin-vue", redirect)
	// Keep the asset root a 404 instead of Gin's automatic slash redirect.
	r.GET("/web/ui", handler)
	r.HEAD("/web/ui", handler)
	r.GET("/web/ui/*asset", handler)
	r.HEAD("/web/ui/*asset", handler)
}

func webUIHandler(files fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		requestPath := c.Request.URL.Path
		// Build URLs are canonical: never decode or clean a path into another asset.
		if strings.Contains(c.Request.URL.EscapedPath(), "%") {
			c.Status(http.StatusNotFound)
			return
		}
		entry := requestPath == "/" || requestPath == "/web" || requestPath == "/admin" || requestPath == "/web/index.html"
		name := "index.html"
		if !entry {
			if !strings.HasPrefix(requestPath, "/web/ui/") {
				c.Status(http.StatusNotFound)
				return
			}
			name = strings.TrimPrefix(requestPath, "/web/ui/")
			if !validWebUIPath(name) {
				c.Status(http.StatusNotFound)
				return
			}
		}

		data, err := readWebUIFile(files, name)
		if err != nil {
			if entry {
				writeWebUIData(c, http.StatusServiceUnavailable, "text/plain; charset=utf-8", []byte("Vue UI unavailable; build the frontend assets first.\n"))
			} else {
				c.Status(http.StatusNotFound)
			}
			return
		}
		contentType := webUIContentType(name)
		if !strings.HasPrefix(contentType, "text/html") {
			sum := sha256.Sum256(data)
			digest := hex.EncodeToString(sum[:])
			cacheControl := "no-cache"
			if webUIImmutable(files, name, digest, int64(len(data))) {
				cacheControl = "public, max-age=31536000, immutable"
			}
			c.Header("Cache-Control", cacheControl)
			c.Header("ETag", `"`+digest+`"`)
			if ifNoneMatchHit(c.GetHeader("If-None-Match"), digest) {
				c.Status(http.StatusNotModified)
				return
			}
		}
		writeWebUIData(c, http.StatusOK, contentType, data)
	}
}

func validWebUIPath(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:%") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.TrimRight(part, " .") != part || strings.EqualFold(part, "build-info.json") {
			return false
		}
	}
	return !strings.EqualFold(path.Ext(name), ".map")
}

func readWebUIFile(files fs.FS, name string) ([]byte, error) {
	if files == nil {
		return nil, fs.ErrNotExist
	}
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	return io.ReadAll(file)
}

func webUIImmutable(files fs.FS, name, digest string, size int64) bool {
	if !webUIHashedName.MatchString(path.Base(name)) {
		return false
	}
	data, err := readWebUIFile(files, "build-info.json")
	if err != nil {
		return false
	}
	var info webUIBuildInfo
	if json.Unmarshal(data, &info) != nil || info.SchemaVersion != 1 {
		return false
	}
	// Recheck bytes and the manifest on each request so a dev rebuild cannot
	// accidentally give changed or partially published content a one-year cache.
	var match *webUIBuildFile
	for i := range info.Files {
		file := &info.Files[i]
		if file.Path == name {
			if match != nil {
				return false
			}
			match = file
		}
	}
	return match != nil && match.Immutable && match.SHA256 == digest && match.Size == size
}

func writeWebUIData(c *gin.Context, status int, contentType string, data []byte) {
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	if c.Request.Method == http.MethodHead {
		c.Status(status)
		return
	}
	c.Data(status, contentType, data)
}

func webUIContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	default:
		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			return contentType
		}
		return "application/octet-stream"
	}
}

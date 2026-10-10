package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"emby-go/internal/cache"
	"emby-go/internal/config"
)

const webUITestScript = "assets/app-AbCd012_.js"

func webUIFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{
		"index.html":                        {Data: []byte(`<!doctype html><title>Vue fixture</title><script type="module" src="/web/ui/assets/app-AbCd012_.js"></script>`)},
		webUITestScript:                     {Data: []byte(`document.title = "Vue fixture";`)},
		"assets/main-a1b2c3d4.css":          {Data: []byte(`body { color: black; }`)},
		"assets/module-BcDe_123.mjs":        {Data: []byte(`export const fixture = true;`)},
		"assets/logo-12ab34cd.svg":          {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><path d="M0 0h1v1H0z"/></svg>`)},
		"assets/icons-0123abcd.woff2":       {Data: []byte("wOF2-test")},
		"fonts/icons.woff":                  {Data: []byte("wOFF-test")},
		"fonts/icons.ttf":                   {Data: []byte{0, 1, 0, 0}},
		"fonts/icons.otf":                   {Data: []byte("OTTO-test")},
		"fonts/icons.eot":                   {Data: []byte("eot-test")},
		"favicon.ico":                       {Data: []byte{0, 0, 1, 0, 1, 0}},
		"vendor/artplayer.min.js":           {Data: []byte(`window.player = {};`)},
		"assets/plain.js":                   {Data: []byte(`window.plain = true;`)},
		"assets/mutable-Abcd1234.js":        {Data: []byte(`window.mutable = true;`)},
		"assets/unlisted-Abcd1234.js":       {Data: []byte(`window.unlisted = true;`)},
		"data.json":                         {Data: []byte(`{"fixture":true}`)},
		"opaque.unknown-web-ui-test-format": {Data: []byte{1, 2, 3}},
	}
	entries := make([]map[string]any, 0, len(files))
	for name, file := range files {
		if strings.Contains(name, "unlisted") {
			continue
		}
		sum := sha256.Sum256(file.Data)
		entries = append(entries, map[string]any{
			"path": name, "sha256": hex.EncodeToString(sum[:]), "size": len(file.Data),
			"immutable": !strings.Contains(name, "mutable"),
		})
	}
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "builtAt": "2026-10-10T00:00:00Z",
		"source": map[string]any{"commit": "test-commit", "dirty": false}, "files": entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	files["build-info.json"] = &fstest.MapFile{Data: manifest}
	return files
}

func webUITestRouter(files fs.FS) *gin.Engine {
	r := gin.New()
	registerWebUIRoutes(r, files)
	return r
}

func webUIRequest(handler http.Handler, method, target, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept", "text/html")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestWebUIStaticResources(t *testing.T) {
	files := webUIFixture(t)
	router := webUITestRouter(files)
	cases := []struct {
		name        string
		contentType string
		immutable   bool
	}{
		{webUITestScript, "application/javascript; charset=utf-8", true},
		{"assets/module-BcDe_123.mjs", "application/javascript; charset=utf-8", true},
		{"assets/main-a1b2c3d4.css", "text/css; charset=utf-8", true},
		{"assets/logo-12ab34cd.svg", "image/svg+xml", true},
		{"assets/icons-0123abcd.woff2", "font/woff2", true},
		{"fonts/icons.woff", "font/woff", false},
		{"fonts/icons.ttf", "font/ttf", false},
		{"fonts/icons.otf", "font/otf", false},
		{"fonts/icons.eot", "application/vnd.ms-fontobject", false},
		{"favicon.ico", "image/x-icon", false},
		{"vendor/artplayer.min.js", "application/javascript; charset=utf-8", false},
		{"assets/plain.js", "application/javascript; charset=utf-8", false},
		{"assets/mutable-Abcd1234.js", "application/javascript; charset=utf-8", false},
		{"assets/unlisted-Abcd1234.js", "application/javascript; charset=utf-8", false},
		{"data.json", "application/json", false},
		{"opaque.unknown-web-ui-test-format", "application/octet-stream", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/web/ui/" + tc.name + "?v=%25test"
			get := webUIRequest(router, http.MethodGet, target, "")
			if get.Code != http.StatusOK || get.Body.String() != string(files[tc.name].Data) {
				t.Fatalf("GET: status=%d body=%q", get.Code, get.Body.String())
			}
			cacheControl := "no-cache"
			if tc.immutable {
				cacheControl = "public, max-age=31536000, immutable"
			}
			sum := sha256.Sum256(files[tc.name].Data)
			etag := `"` + hex.EncodeToString(sum[:]) + `"`
			wantHeaders := map[string]string{
				"Content-Type": tc.contentType, "Cache-Control": cacheControl, "ETag": etag,
				"Content-Length": strconv.Itoa(len(files[tc.name].Data)), "X-Content-Type-Options": "nosniff",
			}
			head := webUIRequest(router, http.MethodHead, target, "")
			if head.Code != http.StatusOK || head.Body.Len() != 0 {
				t.Fatalf("HEAD: status=%d body=%q", head.Code, head.Body.String())
			}
			for header, want := range wantHeaders {
				if get.Header().Get(header) != want || head.Header().Get(header) != want {
					t.Errorf("%s: GET=%q HEAD=%q want=%q", header, get.Header().Get(header), head.Header().Get(header), want)
				}
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, condition := range []string{etag, "W/" + etag, `"other", W/` + etag, "*"} {
					response := webUIRequest(router, method, target, condition)
					if response.Code != http.StatusNotModified || response.Body.Len() != 0 {
						t.Errorf("%s %s: status=%d body=%q", method, condition, response.Code, response.Body.String())
					}
					if response.Header().Get("ETag") != etag || response.Header().Get("Cache-Control") != cacheControl || response.Header().Get("Content-Length") != "" {
						t.Errorf("304 headers: %v", response.Header())
					}
				}
			}
			if response := webUIRequest(router, http.MethodGet, target, `"different"`); response.Code != http.StatusOK {
				t.Errorf("nonmatching ETag: %d", response.Code)
			}
		})
	}
}

func TestWebUIHTMLNoStore(t *testing.T) {
	files := webUIFixture(t)
	router := webUITestRouter(files)
	for _, target := range []string{"/admin", "/admin?next=%2Fitems", "/web/ui/index.html"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := webUIRequest(router, method, target, "*")
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != "" {
				t.Errorf("%s %s: status=%d headers=%v", method, target, response.Code, response.Header())
			}
			if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Header().Get("Content-Length") != strconv.Itoa(len(files["index.html"].Data)) {
				t.Errorf("HTML headers: %v", response.Header())
			}
			if method == http.MethodHead && response.Body.Len() != 0 || method == http.MethodGet && response.Body.String() != string(files["index.html"].Data) {
				t.Errorf("%s HTML body=%q", method, response.Body.String())
			}
		}
	}
}

func TestWebUIMissingAssets(t *testing.T) {
	for name, files := range map[string]fs.FS{
		"nil": nil, "empty": fstest.MapFS{},
		"directory-entry": fstest.MapFS{"index.html": {Mode: fs.ModeDir}},
	} {
		t.Run(name, func(t *testing.T) {
			router := webUITestRouter(files)
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for target, status := range map[string]int{"/admin": http.StatusServiceUnavailable, "/web/ui/missing.js": http.StatusNotFound} {
					response := webUIRequest(router, method, target, "*")
					if response.Code != status || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") != "" {
						t.Errorf("%s %s: status=%d headers=%v", method, target, response.Code, response.Header())
					}
					if method == http.MethodHead && response.Body.Len() != 0 {
						t.Errorf("HEAD body=%q", response.Body.String())
					}
					if strings.Contains(response.Body.String(), "<!doctype") {
						t.Error("missing assets must not return HTML")
					}
				}
			}
		})
	}
}

func TestWebUIRejectsUnsafePaths(t *testing.T) {
	files := webUIFixture(t)
	for _, name := range []string{".env", ".git/config", "assets/app.js.map", "private/build-info.json", "BUILD-INFO.JSON", "100%.js", "assets/app.js:secret"} {
		files[name] = &fstest.MapFile{Data: []byte("must not be exposed")}
	}
	router := webUITestRouter(files)
	targets := []string{
		"/web/ui", "/web/ui/", "/web/ui/assets", "/web/ui/assets/", "/web/ui/.",
		"/web/ui/../web/app.js", "/web/ui/assets/../../web/app.js", "/web/ui//index.html",
		"/web/ui/assets/./app-AbCd012_.js", "/web/ui/assets/../index.html",
		"/web/ui/%2e%2e/web/app.js", "/web/ui/%252e%252e/web/app.js", "/web/ui/%2Findex.html",
		"/web/ui/assets%2fapp-AbCd012_.js", "/web/ui/%61ssets/app-AbCd012_.js",
		"/web/ui/%5c..%5cweb%5capp.js", "/web/ui/100%25.js", "/web/ui/%25zz", "/web/ui/%00index.html",
		"/web/ui/C:/Windows/win.ini", "/web/ui/assets/app.js:secret",
		"/web/ui/build-info.json", "/web/ui/%62uild-info.json", "/web/ui/BUILD-INFO.JSON",
		"/web/ui/build-info.json.", "/web/ui/build-info.json%20", "/web/ui/assets./app-AbCd012_.js",
		"/web/ui/private/build-info.json", "/web/ui/.env", "/web/ui/.git/config", "/web/ui/assets/app.js.map",
		"/web/ui/missing.js", "/web/ui/missing.html", "/web/ui/index.html/extra",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := webUIRequest(router, method, target, "*")
				if response.Code != http.StatusNotFound || response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s: status=%d headers=%v body=%q", method, response.Code, response.Header(), response.Body.String())
				}
			}
		})
	}
	for _, rawPath := range []string{"/web/ui/%", "/web/ui/%zz"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path, req.URL.RawPath, req.RequestURI = rawPath, rawPath, rawPath
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusNotFound {
			t.Errorf("malformed percent path %q: %d", rawPath, response.Code)
		}
	}
}

func TestWebUIImmutableValidation(t *testing.T) {
	cases := map[string]func(*webUIBuildInfo){
		"schema":    func(info *webUIBuildInfo) { info.SchemaVersion = 2 },
		"unlisted":  func(info *webUIBuildInfo) { info.Files = nil },
		"digest":    func(info *webUIBuildInfo) { info.Files[0].SHA256 = strings.Repeat("0", 64) },
		"size":      func(info *webUIBuildInfo) { info.Files[0].Size++ },
		"flag":      func(info *webUIBuildInfo) { info.Files[0].Immutable = false },
		"duplicate": func(info *webUIBuildInfo) { info.Files = append(info.Files, info.Files[0]) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			files := webUIFixture(t)
			var info webUIBuildInfo
			if err := json.Unmarshal(files["build-info.json"].Data, &info); err != nil {
				t.Fatal(err)
			}
			for _, entry := range info.Files {
				if entry.Path == webUITestScript {
					info.Files = []webUIBuildFile{entry}
					break
				}
			}
			mutate(&info)
			data, err := json.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			files["build-info.json"].Data = data
			response := webUIRequest(webUITestRouter(files), http.MethodGet, "/web/ui/"+webUITestScript, "")
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-cache" || response.Header().Get("ETag") == "" {
				t.Errorf("invalid manifest: status=%d headers=%v", response.Code, response.Header())
			}
		})
	}
	for _, data := range []string{"", "{broken", "null", `{"schemaVersion":1} {}`} {
		files := webUIFixture(t)
		if data == "" {
			delete(files, "build-info.json")
		} else {
			files["build-info.json"].Data = []byte(data)
		}
		response := webUIRequest(webUITestRouter(files), http.MethodGet, "/web/ui/"+webUITestScript, "")
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("manifest %q: status=%d headers=%v", data, response.Code, response.Header())
		}
	}
}

func TestWebUIRebuildRevalidatesContent(t *testing.T) {
	files := webUIFixture(t)
	router := webUITestRouter(files)
	target := "/web/ui/" + webUITestScript
	before := webUIRequest(router, http.MethodGet, target, "")
	original := append([]byte(nil), files[webUITestScript].Data...)
	files[webUITestScript].Data[0] = 'D'
	after := webUIRequest(router, http.MethodGet, target, before.Header().Get("ETag"))
	if after.Code != http.StatusOK || after.Header().Get("ETag") == before.Header().Get("ETag") || after.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("changed content: status=%d headers=%v", after.Code, after.Header())
	}
	files[webUITestScript].Data = original
	manifest := files["build-info.json"]
	delete(files, "build-info.json")
	withoutManifest := webUIRequest(router, http.MethodGet, target, "")
	if withoutManifest.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("removed manifest retained immutable caching")
	}
	files["build-info.json"] = manifest
	rebuilt := webUIRequest(router, http.MethodGet, target, "")
	if rebuilt.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal("new manifest was not picked up")
	}
}

func TestWebUIRouteIsolation(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(strconv.FormatBool(ready), func(t *testing.T) {
			files := fstest.MapFS{}
			if ready {
				files = webUIFixture(t)
			}
			original := webUIFiles
			webUIFiles = files
			t.Cleanup(func() { webUIFiles = original })
			app, err := newApp(config.Config{
				DBPath: filepath.Join(t.TempDir(), "web-ui-test.db"), ServerID: "web-ui-test",
			}, cache.NewMemory(64))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(app.Close)
			entryStatus, assetStatus := http.StatusServiceUnavailable, http.StatusNotFound
			if ready {
				entryStatus, assetStatus = http.StatusOK, http.StatusOK
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for target, want := range map[string]int{"/": entryStatus, "/web": entryStatus, "/admin": entryStatus, "/web/index.html": entryStatus, "/web/ui/" + webUITestScript: assetStatus} {
					response := webUIRequest(app.Handler(), method, target, "")
					if response.Code != want {
						t.Errorf("%s %s: status=%d want=%d", method, target, response.Code, want)
					}
				}
			}
			for target, status := range map[string]int{
				"/api/auth/status": http.StatusOK, "/System/Info/Public": http.StatusOK,
				"/api/admin/libraries": http.StatusUnauthorized, "/Users/Me": http.StatusUnauthorized,
				"/emby/Users/Me": http.StatusUnauthorized, "/api/unknown": http.StatusNotFound,
				"/System/UnknownProtocolOperation":      http.StatusNotFound,
				"/emby/System/UnknownProtocolOperation": http.StatusNotFound,
				"/unknown-protocol":                     http.StatusNotFound, "/admin-vue/items": http.StatusNotFound,
			} {
				response := webUIRequest(app.Handler(), http.MethodGet, target, "*")
				if response.Code != status || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") || !json.Valid(response.Body.Bytes()) {
					t.Errorf("API %s: status=%d headers=%v body=%s", target, response.Code, response.Header(), response.Body.String())
				}
			}
			for _, target := range []string{"/admin", "/web/ui/" + webUITestScript} {
				response := webUIRequest(app.Handler(), http.MethodPost, target, "")
				if response.Code != http.StatusNotFound || !json.Valid(response.Body.Bytes()) {
					t.Errorf("POST %s must keep the original no-route behavior: %d %s", target, response.Code, response.Body.String())
				}
			}
			for target, asset := range map[string]string{
				"/web/login.js": "web/login.js", "/web/app.js": "web/app.js", "/web/style.css": "web/style.css",
				"/web/vendor/artplayer.min.js": "web/vendor/artplayer.min.js", "/favicon.ico": "web/favicon.ico",
			} {
				want, err := webFiles.ReadFile(asset)
				if err != nil {
					t.Fatal(err)
				}
				response := webUIRequest(app.Handler(), http.MethodGet, target, "")
				if response.Code != http.StatusOK || response.Body.String() != string(want) {
					t.Errorf("legacy %s changed: status=%d", target, response.Code)
				}
				if strings.HasSuffix(asset, ".html") && response.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("legacy HTML cache changed: %v", response.Header())
				}
				if etag := response.Header().Get("ETag"); etag != "" {
					conditional := webUIRequest(app.Handler(), http.MethodGet, target, etag)
					if conditional.Code != http.StatusNotModified {
						t.Errorf("legacy conditional GET %s: %d", target, conditional.Code)
					}
				}
			}
		})
	}
}

func TestWebUIMigrationEntryRedirect(t *testing.T) {
	router := webUITestRouter(webUIFixture(t))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := webUIRequest(router, method, "/admin-vue?library_id=3&search=%E4%B8%AD%E6%96%87", "")
		if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/admin?library_id=3&search=%E4%B8%AD%E6%96%87" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("redirect: %d %v", response.Code, response.Header())
		}
	}
}

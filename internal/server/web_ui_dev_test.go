//go:build !embedui

package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWebUIDevSourceDirectory(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	t.Chdir(t.TempDir())
	files, ok := loadWebUIAssets().(webUIDevFS)
	want := filepath.Join(filepath.Dir(source), "web_dist")
	if !ok || string(files) != want || !filepath.IsAbs(string(files)) {
		t.Fatalf("asset directory=%q want=%q", files, want)
	}
}

func TestWebUIDevMissingDirectoryAndLateBuild(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "web_dist")
	files := webUIDevFS(directory)
	router := webUITestRouter(files)
	response := webUIRequest(router, http.MethodGet, "/admin", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing directory: %d", response.Code)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, file := range webUIFixture(t) {
		target := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, file.Data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	response = webUIRequest(router, http.MethodGet, "/admin", "")
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("late build: status=%d headers=%v", response.Code, response.Header())
	}
	response = webUIRequest(router, http.MethodGet, "/web/ui/"+webUITestScript, "")
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("late build asset: status=%d headers=%v", response.Code, response.Header())
	}
	for _, target := range []string{"/web/ui/build-info.json", "/web/ui/build-info.json.", "/web/ui/build-info.json%20"} {
		response := webUIRequest(router, http.MethodGet, target, "")
		if response.Code != http.StatusNotFound || response.Body.Len() != 0 {
			t.Errorf("build metadata exposed through %s: status=%d body=%q", target, response.Code, response.Body.String())
		}
	}
	for _, name := range []string{"../outside", "/index.html", "assets/../index.html"} {
		if file, err := files.Open(name); err == nil {
			file.Close()
			t.Errorf("invalid filesystem path accepted: %s", name)
		}
	}
}

func TestWebUIDevSymlinkCannotEscape(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "web_dist")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.js")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape.js")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files := webUIDevFS(directory)
	if _, err := fs.ReadFile(files, "escape.js"); err == nil {
		t.Fatal("filesystem read escaped web_dist")
	}
	response := webUIRequest(webUITestRouter(files), http.MethodGet, "/web/ui/escape.js", "")
	if response.Code != http.StatusNotFound || response.Body.Len() != 0 {
		t.Fatalf("symlink response: status=%d body=%q", response.Code, response.Body.String())
	}
}

//go:build embedui

package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
)

func TestWebUIEmbeddedBuild(t *testing.T) {
	files := loadWebUIAssets()
	manifest, err := readWebUIFile(files, "build-info.json")
	if err != nil {
		t.Fatal(err)
	}
	var info webUIBuildInfo
	if err := json.Unmarshal(manifest, &info); err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != 1 || len(info.Files) == 0 {
		t.Fatalf("invalid build manifest: %s", manifest)
	}
	indexFound := false
	for _, entry := range info.Files {
		if !validWebUIPath(entry.Path) {
			t.Fatalf("non-public build entry: %q", entry.Path)
		}
		data, err := readWebUIFile(files, entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != entry.SHA256 || int64(len(data)) != entry.Size {
			t.Errorf("embedded content does not match manifest: %s", entry.Path)
		}
		if entry.Path == "index.html" && len(data) > 0 {
			indexFound = true
		}
	}
	if !indexFound {
		t.Fatal("manifest must contain the real entry HTML")
	}
	router := webUITestRouter(files)
	response := webUIRequest(router, http.MethodGet, "/admin", "")
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("embedded entry: status=%d headers=%v", response.Code, response.Header())
	}
	if response := webUIRequest(router, http.MethodGet, "/web/ui/build-info.json", ""); response.Code != http.StatusNotFound {
		t.Errorf("embedded build metadata exposed: %d", response.Code)
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/alist"
	"emby-go/internal/cache"
	"emby-go/internal/store"
)

type remoteDeleteFixture struct {
	mu        sync.Mutex
	files     map[string]alist.File
	failed    map[string]bool
	lostReply bool
	removed   []string
}

func (f *remoteDeleteFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "fixture-alist-token" {
		w.WriteHeader(403)
		return
	}
	var req struct {
		Path  string   `json:"path"`
		Dir   string   `json:"dir"`
		Names []string `json:"names"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.HasSuffix(r.URL.Path, "/list") {
		files := []alist.File{}
		for p, file := range f.files {
			if path.Dir(p) == req.Path {
				files = append(files, file)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"total": len(files), "content": files}})
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/remove") || len(req.Names) != 1 {
		w.WriteHeader(400)
		return
	}
	p := path.Join(req.Dir, req.Names[0])
	if f.failed[p] {
		_, _ = w.Write([]byte(`{"code":403,"message":"fixture-alist-token permission denied"}`))
		return
	}
	delete(f.files, p)
	f.removed = append(f.removed, p)
	if f.lostReply {
		w.WriteHeader(502)
		return
	}
	_, _ = w.Write([]byte(`{"code":200,"data":null}`))
}

func deletionFixture(t *testing.T, parts int) (*App, string, int64, int64, *remoteDeleteFixture, []string) {
	t.Helper()
	a := accountApp(t)
	token := accountLogin(t, a)
	f := &remoteDeleteFixture{files: map[string]alist.File{}, failed: map[string]bool{}}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	a.cfgMu.Lock()
	a.cfg.AListURL, a.cfg.AListToken = ts.URL, "fixture-alist-token"
	a.cfgMu.Unlock()
	lib, err := a.db.AddLibrary("Deletion", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sources := []string{}
	for i := 1; i <= parts; i++ {
		name := fmt.Sprintf("ALDN-001-CD%d.mp4", i)
		remote := "/115/" + name
		f.files[remote] = alist.File{Name: name, ID: name, Size: int64(i * 100), Modified: "2026-10-24T00:00:00Z"}
		source := filepath.Join(lib.Path, fmt.Sprintf("copy-CD%d.strm", i))
		writeFile(t, source, ts.URL+"/d"+remote+"?sign=secret-sign")
		sources = append(sources, source)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: sources[0], AdditionalParts: sources[1:], Number: "ALDN-001", Title: "Delete copy", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(lib.Path, "keep.strm")
	writeFile(t, keep, ts.URL+"/d/115/keep.mp4")
	keepID, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: keep, Number: "aldn001", Title: "Keep copy", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(lib.Path, "poster.jpg"), "cover")
	writeFile(t, filepath.Join(lib.Path, "copy.nfo"), "metadata")
	return a, token, id, keepID, f, sources
}

func previewDeletion(t *testing.T, a *App, token string, id int64) map[string]any {
	t.Helper()
	resp := accountRequest(a, "GET", fmt.Sprintf("/api/admin/duplicates/items/%d/delete-preview?key=ALDN001", id), token, "")
	if resp.Code != 200 {
		t.Fatalf("preview: %d %s", resp.Code, resp.Body)
	}
	if strings.Contains(resp.Body.String(), "fixture-alist-token") || strings.Contains(resp.Body.String(), "secret-sign") {
		t.Fatal("secret exposed")
	}
	var data map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &data)
	return data
}

func deleteBody(preview map[string]any) string {
	data, _ := json.Marshal(map[string]any{"key": "ALDN001", "library_id": 0, "expires_at": preview["expires_at"], "preview_token": preview["preview_token"]})
	return string(data)
}

func TestDuplicateDeleteRemovesRemoteSTRMsAndIndexOnlyAfterConfirmation(t *testing.T) {
	a, token, id, keepID, f, sources := deletionFixture(t, 2)
	preview := previewDeletion(t, a, token, id)
	if len(preview["files"].([]any)) != 2 || len(f.removed) != 0 {
		t.Fatal("preview mutated files")
	}
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview))
	if resp.Code != 204 {
		t.Fatalf("delete: %d %s", resp.Code, resp.Body)
	}
	if len(f.removed) != 2 {
		t.Fatal(f.removed)
	}
	for _, source := range sources {
		if _, err := os.Stat(source); !os.IsNotExist(err) {
			t.Fatal("STRM retained")
		}
	}
	if _, err := a.db.Movie(id); err == nil {
		t.Fatal("index retained")
	}
	keep, err := a.db.Movie(keepID)
	if err != nil {
		t.Fatal("keeper deleted")
	}
	for _, source := range []string{keep.SourcePath, filepath.Join(filepath.Dir(sources[0]), "poster.jpg"), filepath.Join(filepath.Dir(sources[0]), "copy.nfo")} {
		if _, err := os.Stat(source); err != nil {
			t.Fatal("unrelated file deleted", err)
		}
	}
}

func TestDuplicateDeleteRejectsChangedPreviewAndSharedRemote(t *testing.T) {
	for _, change := range []string{"strm", "remote", "shared", "shared-cross-library", "token"} {
		t.Run(change, func(t *testing.T) {
			a, token, id, keepID, f, sources := deletionFixture(t, 1)
			preview := previewDeletion(t, a, token, id)
			switch change {
			case "strm":
				writeFile(t, sources[0], a.cfg.AListURL+"/d/115/changed.mp4")
			case "remote":
				f.mu.Lock()
				file := f.files["/115/ALDN-001-CD1.mp4"]
				file.Size++
				f.files["/115/ALDN-001-CD1.mp4"] = file
				f.mu.Unlock()
			case "shared":
				keep, _ := a.db.Movie(keepID)
				data, _ := os.ReadFile(sources[0])
				writeFile(t, keep.SourcePath, string(data))
			case "shared-cross-library":
				lib, err := a.db.AddLibrary("Other number", t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				primary, part := filepath.Join(lib.Path, "other.strm"), filepath.Join(lib.Path, "other-CD2.strm")
				writeFile(t, primary, a.cfg.AListURL+"/d/115/other.mp4")
				data, _ := os.ReadFile(sources[0])
				writeFile(t, part, string(data))
				if _, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: primary, AdditionalParts: []string{part}, Number: "OTHER-001", Status: "success"}, 0, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "token":
				preview["preview_token"] = "invalid"
			}
			resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview))
			if resp.Code != 409 || len(f.removed) != 0 {
				t.Fatalf("unsafe deletion: %d %s", resp.Code, resp.Body)
			}
			if _, err := os.Stat(sources[0]); err != nil {
				t.Fatal("local deleted")
			}
		})
	}
}

func TestDuplicateDeletePersistsPartialProgressAcrossRestart(t *testing.T) {
	a, token, id, _, f, sources := deletionFixture(t, 2)
	f.failed["/115/ALDN-001-CD2.mp4"] = true
	preview := previewDeletion(t, a, token, id)
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview))
	if resp.Code != 502 || len(f.removed) != 1 || strings.Contains(resp.Body.String(), "fixture-alist-token") {
		t.Fatalf("partial: %d %s", resp.Code, resp.Body)
	}
	for _, source := range sources {
		if _, err := os.Stat(source); err != nil {
			t.Fatal("local removed before all remote files")
		}
	}
	if _, err := a.db.Movie(id); err != nil {
		t.Fatal("index removed on failure")
	}
	cfg := a.configSnapshot()
	a.Close()
	restarted, err := newApp(cfg, cache.NewMemory(512))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Close() })
	f.mu.Lock()
	delete(f.failed, "/115/ALDN-001-CD2.mp4")
	f.mu.Unlock()
	preview = previewDeletion(t, restarted, token, id)
	resp = accountRequest(restarted, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview))
	if resp.Code != 204 || len(f.removed) != 2 {
		t.Fatalf("resume: %d %s %v", resp.Code, resp.Body, f.removed)
	}
}

func TestDuplicateDeleteRecoversLostReplyWithoutRemovingAgain(t *testing.T) {
	a, token, id, _, f, _ := deletionFixture(t, 1)
	f.lostReply = true
	preview := previewDeletion(t, a, token, id)
	if resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview)); resp.Code != 502 {
		t.Fatal(resp.Code)
	}
	preview = previewDeletion(t, a, token, id)
	if resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview)); resp.Code != 204 || len(f.removed) != 1 {
		t.Fatalf("recovery %d %s", resp.Code, resp.Body)
	}
}

func TestDuplicateDeleteAuthorizationAndTaskConflict(t *testing.T) {
	a, token, id, _, f, _ := deletionFixture(t, 1)
	_, viewer := createViewer(t, a, token, "delete-viewer")
	for _, endpoint := range []struct{ method, path string }{
		{"GET", fmt.Sprintf("/api/admin/duplicates/items/%d/delete-preview?key=ALDN001", id)},
		{"DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id)},
	} {
		for _, auth := range []struct {
			token string
			code  int
		}{{"", 401}, {viewer, 403}} {
			if resp := accountRequest(a, endpoint.method, endpoint.path, auth.token, `{}`); resp.Code != auth.code {
				t.Fatalf("auth %d", resp.Code)
			}
		}
	}
	preview := previewDeletion(t, a, token, id)
	a.claimNFO("scan")
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, deleteBody(preview))
	a.releaseNFO("scan")
	if resp.Code != 409 || len(f.removed) != 0 {
		t.Fatal("deleted while scanning")
	}
}

func TestDuplicateDeleteLocalFileBoundaries(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "movie.strm")
	outside := filepath.Join(t.TempDir(), "outside.strm")
	writeFile(t, inside, "http://alist.test/d/file.mp4")
	writeFile(t, outside, "http://alist.test/d/outside.mp4")
	if _, err := deletionLocalPath(root, inside, false); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{outside, filepath.Join(root, "poster.jpg"), filepath.Join(root, "missing.strm"), root} {
		if _, err := deletionLocalPath(root, file, false); err == nil {
			t.Fatal("unsafe local target accepted", file)
		}
	}
	if _, err := deletionLocalPath(root, filepath.Join(root, "missing.strm"), true); err != nil {
		t.Fatal("completed deletion cannot resume", err)
	}
	link := filepath.Join(root, "linked.strm")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := deletionLocalPath(root, link, false); err == nil {
			t.Fatal("outside symlink accepted")
		}
	} else {
		t.Log("symlink unavailable", err)
	}
	if _, err := readDeletionSTRM(root); err == nil {
		t.Fatal("directory accepted as STRM")
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestDuplicateSTRMReadsCurrentLocalContentsWithoutRemoteRequests(t *testing.T) {
	a, token, id, keepID, _, sources := deletionFixture(t, 2)
	var remoteRequests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteRequests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(remote.Close)
	a.cfgMu.Lock()
	a.cfg.AListURL = remote.URL
	a.cfgMu.Unlock()
	content := "  " + remote.URL + "/movie.mp4?sign=full-signed-link\r\n<script>not executable</script>\n"
	writeFile(t, sources[0], "\ufeff"+content)
	writeFile(t, sources[1], remote.URL+"/CD2.mp4\n")
	endpoint := fmt.Sprintf("/api/admin/duplicates/items/%d/strm?key=ALDN001", id)
	read := func() duplicateSTRMContents {
		t.Helper()
		resp := accountRequest(a, "GET", endpoint, token, "")
		var contents duplicateSTRMContents
		if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &contents) != nil {
			t.Fatalf("contents: %d %s", resp.Code, resp.Body)
		}
		if resp.Header().Get("Cache-Control") != "no-store" || contents.MovieID != id || len(contents.Files) != 2 {
			t.Fatalf("invalid contents or cache policy: %+v", contents)
		}
		return contents
	}
	contents := read()
	if contents.Files[0].LocalPath != sources[0] || contents.Files[0].Content != content || contents.Files[0].Error != "" || contents.Files[1].Error != "" || !strings.Contains(contents.Files[1].Content, "CD2.mp4") {
		t.Fatalf("lost main/part content: %+v", contents)
	}
	writeFile(t, sources[0], "updated locally\n")
	if next := read(); next.Files[0].Content != "updated locally\n" {
		t.Fatal("read stale indexed content", next)
	}
	if remoteRequests.Load() != 0 {
		t.Fatal("reading contents made remote requests", remoteRequests.Load())
	}
	for _, movieID := range []int64{id, keepID} {
		if _, err := a.db.Movie(movieID); err != nil {
			t.Fatal("read modified the index", err)
		}
		if state, err := a.db.MovieDeletionState(movieID); err != nil || state != "" {
			t.Fatal("read created deletion state", state, err)
		}
	}
}

func TestDuplicateSTRMReportsEachFileFailureAndEmptyContents(t *testing.T) {
	a, token, id, _, _, sources := deletionFixture(t, 3)
	writeFile(t, sources[0], "")
	if err := os.Remove(sources[1]); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sources[2], "third part\n")
	resp := accountRequest(a, "GET", fmt.Sprintf("/api/admin/duplicates/items/%d/strm?key=ALDN001", id), token, "")
	var contents duplicateSTRMContents
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &contents) != nil || len(contents.Files) != 3 {
		t.Fatalf("partial contents: %d %s", resp.Code, resp.Body)
	}
	if contents.Files[0].Content != "" || contents.Files[0].Error != "" || contents.Files[1].Error == "" || contents.Files[1].Content != "" || contents.Files[2].Content != "third part\n" {
		t.Fatalf("one missing part hid other files: %+v", contents)
	}
}

func TestDuplicateSTRMRejectsInvalidScopeAndNonAdmins(t *testing.T) {
	a, token, id, _, _, sources := deletionFixture(t, 1)
	endpoint := fmt.Sprintf("/api/admin/duplicates/items/%d/strm", id)
	for _, query := range []string{"", "?key=", "?key=ALDN001&library_id=-1", "?key=ALDN001&library_id=bad", "?key=" + strings.Repeat("A", 257)} {
		if resp := accountRequest(a, "GET", endpoint+query, token, ""); resp.Code != 400 {
			t.Fatalf("invalid query accepted %s: %d", query, resp.Code)
		}
	}
	for _, badID := range []string{"0", "-1", "bad", "999999"} {
		resp := accountRequest(a, "GET", "/api/admin/duplicates/items/"+badID+"/strm?key=ALDN001", token, "")
		if resp.Code != 400 && resp.Code != 404 {
			t.Fatalf("invalid id accepted %s: %d", badID, resp.Code)
		}
	}
	for query, code := range map[string]int{"?key=OTHER": 409, "?key=ALDN001&library_id=999999": 404} {
		if resp := accountRequest(a, "GET", endpoint+query, token, ""); resp.Code != code {
			t.Fatalf("invalid scope accepted: %d %s", resp.Code, resp.Body)
		}
	}
	_, viewer := createViewer(t, a, token, "strm-viewer")
	for credential, code := range map[string]int{"": 401, viewer: 403} {
		if resp := accountRequest(a, "GET", endpoint+"?key=ALDN001", credential, ""); resp.Code != code || strings.Contains(resp.Body.String(), sources[0]) {
			t.Fatalf("non-admin contents leak: %d %s", resp.Code, resp.Body)
		}
	}
	if err := a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if resp := accountRequest(a, "GET", endpoint+"?key=ALDN001", token, ""); resp.Code != 404 || strings.Contains(resp.Body.String(), sources[0]) {
		t.Fatalf("inaccessible library leak: %d %s", resp.Code, resp.Body)
	}
}

func TestDuplicateSTRMBoundsLocalFiles(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, source, content string
		directory, allowed    bool
	}{
		{name: "regular", source: filepath.Join(root, "copy.strm"), content: "exact\n", allowed: true},
		{name: "uppercase", source: filepath.Join(root, "copy.STRM"), content: "uppercase", allowed: true},
		{name: "at limit", source: filepath.Join(root, "limit.strm"), content: strings.Repeat("a", 64<<10), allowed: true},
		{name: "over limit", source: filepath.Join(root, "large.strm"), content: strings.Repeat("a", (64<<10)+1)},
		{name: "other extension", source: filepath.Join(root, "copy.nfo"), content: "private"},
		{name: "outside library", source: filepath.Join(t.TempDir(), "outside.strm"), content: "private"},
		{name: "sibling prefix", source: filepath.Join(root+"-sibling", "outside.strm"), content: "private"},
		{name: "directory", source: filepath.Join(root, "directory.strm"), directory: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.directory {
				if err := os.Mkdir(test.source, 0755); err != nil {
					t.Fatal(err)
				}
			} else if test.name == "sibling prefix" {
				if file := readDuplicateSTRM(root, test.source); file.Error == "" || file.Content != "" {
					t.Fatal("sibling path accepted", file)
				}
				return
			} else {
				writeFile(t, test.source, test.content)
			}
			file := readDuplicateSTRM(root, test.source)
			if (file.Error == "") != test.allowed || file.LocalPath != test.source || test.allowed && file.Content != test.content || !test.allowed && file.Content != "" {
				t.Fatalf("unexpected local file result: error=%q content length=%d", file.Error, len(file.Content))
			}
		})
	}
	t.Run("symlink outside library", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "private.strm")
		writeFile(t, outside, "private")
		link := filepath.Join(root, "link.strm")
		if err := os.Symlink(outside, link); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		if file := readDuplicateSTRM(root, link); file.Error == "" || file.Content != "" {
			t.Fatal("symlink leaked external contents", file)
		}
	})
}

func TestDuplicateSTRMRejectsExcessivePartsAndTelevision(t *testing.T) {
	a, token, id, _, _, _ := deletionFixture(t, 1)
	movie, err := a.db.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	movie.AdditionalParts = make([]string, 64)
	if _, err = a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	endpoint := fmt.Sprintf("/api/admin/duplicates/items/%d/strm?key=ALDN001", id)
	if resp := accountRequest(a, "GET", endpoint, token, ""); resp.Code != 400 {
		t.Fatalf("unbounded part read: %d %s", resp.Code, resp.Body)
	}
	movie.AdditionalParts = nil
	movie.Television = &store.Television{}
	if _, err = a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if resp := accountRequest(a, "GET", endpoint, token, ""); resp.Code != 409 {
		t.Fatalf("television contents exposed: %d %s", resp.Code, resp.Body)
	}
}

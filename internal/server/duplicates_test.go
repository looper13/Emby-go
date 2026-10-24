package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestAdminDuplicateMovies(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Duplicate library", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, number := range []string{"ALDN-001", "aldn001"} {
		if _, err = a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, fmt.Sprintf("copy-%d.strm", i)), Number: number, Title: "Copy", Status: "success"}, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	resp := accountRequest(a, "GET", "/api/admin/duplicates", token, "")
	var groups store.DuplicateGroups
	if err := json.Unmarshal(resp.Body.Bytes(), &groups); err != nil || resp.Code != 200 || groups.Total != 1 || groups.TotalMovies != 2 || groups.ExtraCopies != 1 {
		t.Fatalf("report: %d %s %v", resp.Code, resp.Body.String(), err)
	}
	resp = accountRequest(a, "GET", "/api/admin/duplicates/items?key=ALDN001&limit=1&offset=1", token, "")
	var members store.DuplicateMembers
	if err := json.Unmarshal(resp.Body.Bytes(), &members); err != nil || resp.Code != 200 || members.Total != 2 || len(members.Items) != 1 || members.Items[0].LibraryName != lib.Name {
		t.Fatalf("members: %d %s %v", resp.Code, resp.Body.String(), err)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=bad", "?library_id=-1", "/items", "/items?key=", "?library_id=999999"} {
		resp = accountRequest(a, "GET", "/api/admin/duplicates"+query, token, "")
		if resp.Code != 400 && resp.Code != 404 {
			t.Fatalf("invalid query accepted %s: %d", query, resp.Code)
		}
	}
	_, viewer := createViewer(t, a, token, "duplicate-viewer")
	for _, endpoint := range []string{"/api/admin/duplicates", "/api/admin/duplicates/items?key=ALDN001"} {
		if resp := accountRequest(a, "GET", endpoint, viewer, ""); resp.Code != 403 {
			t.Fatalf("non-admin leak: %d %s", resp.Code, resp.Body.String())
		}
		if resp := accountRequest(a, "GET", endpoint, "", ""); resp.Code != 401 {
			t.Fatalf("anonymous leak: %d", resp.Code)
		}
	}
}

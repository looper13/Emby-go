package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/alist"
	"emby-go/internal/store"
)

func selectedDuplicates(t *testing.T, a *App, token, rule string) []signedDuplicateSelection {
	t.Helper()
	resp := accountRequest(a, "GET", "/api/admin/duplicates/selection?rule="+rule, token, "")
	var result struct {
		Items []signedDuplicateSelection `json:"items"`
		Total int                        `json:"total"`
	}
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &result) != nil || result.Total != len(result.Items) || resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("selection: %d %s", resp.Code, resp.Body)
	}
	return result.Items
}

func bulkPreview(t *testing.T, a *App, token string, item signedDuplicateSelection) map[string]any {
	t.Helper()
	query := url.Values{"key": {item.Key}, "keep_id": {fmt.Sprint(item.Keep.ID)}, "selection_token": {item.Token}}
	resp := accountRequest(a, "GET", fmt.Sprintf("/api/admin/duplicates/items/%d/delete-preview?%s", item.Movie.ID, query.Encode()), token, "")
	var result map[string]any
	if resp.Code != 200 || json.Unmarshal(resp.Body.Bytes(), &result) != nil {
		t.Fatalf("bulk preview: %d %s", resp.Code, resp.Body)
	}
	return result
}

func bulkDeleteBody(preview map[string]any, item signedDuplicateSelection) string {
	var body map[string]any
	_ = json.Unmarshal([]byte(deleteBody(preview)), &body)
	body["key"], body["keep_id"], body["selection_token"] = item.Key, item.Keep.ID, item.Token
	data, _ := json.Marshal(body)
	return string(data)
}

func TestDuplicateSelectionDeletesOnlySurplusAndPreservesFixedKeeper(t *testing.T) {
	a, token, id, keepID, remote, sources := deletionFixture(t, 1)
	movie, _ := a.db.Movie(id)
	extra := filepath.Join(filepath.Dir(sources[0]), "another-long-copy.strm")
	writeFile(t, extra, a.configSnapshot().AListURL+"/d/115/extra.mp4")
	remote.files["/115/extra.mp4"] = alist.File{Name: "extra.mp4", ID: "extra", Size: 50, Modified: "today"}
	movie.SourcePath = extra
	if _, err := a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	items := selectedDuplicates(t, a, token, "shortest")
	if len(items) != 2 || len(remote.readPaths) != 0 || len(remote.removed) != 0 {
		t.Fatal("selection incomplete or contacted AList", items, remote)
	}
	for _, item := range items {
		if item.Keep.ID != keepID || item.Movie.ID == keepID || item.Token == "" {
			t.Fatal("wrong selected keeper", item)
		}
		preview := bulkPreview(t, a, token, item)
		resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", item.Movie.ID), token, bulkDeleteBody(preview, item))
		if resp.Code != 204 {
			t.Fatalf("bulk item deletion: %d %s", resp.Code, resp.Body)
		}
	}
	keep, err := a.db.Movie(keepID)
	if err != nil {
		t.Fatal("keeper deleted", err)
	}
	if _, err := os.Stat(keep.SourcePath); err != nil {
		t.Fatal("keeper STRM removed", err)
	}
	if len(remote.removed) != 2 || len(selectedDuplicates(t, a, token, "shortest")) != 0 {
		t.Fatal("surplus not removed", remote.removed)
	}
}

func TestDuplicateSelectionRejectsMissingKeeperAndChangedPartsBeforeAnyRemoval(t *testing.T) {
	for _, change := range []string{"keeper", "parts", "signature", "scope"} {
		t.Run(change, func(t *testing.T) {
			a, token, id, keepID, remote, _ := deletionFixture(t, 1)
			item := selectedDuplicates(t, a, token, "shortest")[0]
			preview := bulkPreview(t, a, token, item)
			var body map[string]any
			_ = json.Unmarshal([]byte(bulkDeleteBody(preview, item)), &body)
			switch change {
			case "keeper":
				if err := a.db.DeleteMovie(keepID); err != nil {
					t.Fatal(err)
				}
			case "parts":
				movie, _ := a.db.Movie(id)
				movie.AdditionalParts = []string{"new-CD2.strm"}
				if _, err := a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "signature":
				body["selection_token"] = strings.Repeat("0", 64)
			case "scope":
				body["library_id"] = 999999
			}
			data, _ := json.Marshal(body)
			reads := len(remote.readPaths)
			resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, string(data))
			if resp.Code != 409 || len(remote.removed) != 0 || len(remote.readPaths) != reads {
				t.Fatalf("changed selection touched remote: %d %s", resp.Code, resp.Body)
			}
		})
	}
}

func TestDuplicateSelectionPermissionsAndInvalidRules(t *testing.T) {
	a, token, _, _, _, _ := deletionFixture(t, 1)
	for _, query := range []string{"", "?rule=bad", "?rule=oldest&library_id=-1", "?rule=oldest&library_id=no"} {
		if resp := accountRequest(a, "GET", "/api/admin/duplicates/selection"+query, token, ""); resp.Code != 400 {
			t.Fatalf("invalid selection accepted: %d %s", resp.Code, resp.Body)
		}
	}
	_, viewer := createViewer(t, a, token, "bulk-viewer")
	for credential, code := range map[string]int{"": 401, viewer: 403} {
		if resp := accountRequest(a, "GET", "/api/admin/duplicates/selection?rule=oldest", credential, ""); resp.Code != code {
			t.Fatal("selection leaked to non-admin", resp.Code)
		}
	}
	if err := a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if items := selectedDuplicates(t, a, token, "oldest"); len(items) != 0 {
		t.Fatal("selection leaked inaccessible library", items)
	}
}

func TestDuplicateSelectionCannotResumePartialDeletionWithoutKeeper(t *testing.T) {
	a, token, id, keepID, remote, sources := deletionFixture(t, 2)
	item := selectedDuplicates(t, a, token, "shortest")[0]
	preview := bulkPreview(t, a, token, item)
	remote.failed["/115/ALDN-001-CD2.mp4"] = true
	resp := accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, bulkDeleteBody(preview, item))
	if resp.Code != 502 || len(remote.removed) != 1 {
		t.Fatalf("partial batch deletion: %d %s", resp.Code, resp.Body)
	}
	if err := a.db.DeleteMovie(keepID); err != nil {
		t.Fatal(err)
	}
	remote.failed["/115/ALDN-001-CD2.mp4"] = false
	resp = accountRequest(a, "DELETE", fmt.Sprintf("/api/admin/duplicates/items/%d", id), token, bulkDeleteBody(preview, item))
	if resp.Code != 409 || len(remote.removed) != 1 {
		t.Fatalf("resume removed last indexed copy: %d %s", resp.Code, resp.Body)
	}
	for _, source := range sources {
		if _, err := os.Stat(source); err != nil {
			t.Fatal("resume removed local STRM", err)
		}
	}
}

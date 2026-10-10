package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/avatar"
	"emby-go/internal/store"
)

func TestEmbyPersonsAndVirtualFoldersQuery(t *testing.T) {
	root := t.TempDir()
	app, ts, token := newProbeTestApp(t, root)
	other, err := app.db.AddLibrary("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "poster.jpg")
	writeJPEGImage(t, poster, 40, 20)
	fixtures := []struct {
		library            int64
		status, collection string
		names              []string
	}{
		{1, "success", "", []string{"Alpha", "Shared"}},
		{1, "manual", "Group", []string{"Beta", "Shared"}},
		{other.ID, "success", "", []string{"Other"}},
		{1, "pending", "", []string{"Hidden"}},
	}
	var movieID int64
	for i, fixture := range fixtures {
		id, err := app.db.UpsertMovie(store.Movie{LibraryID: fixture.library, Status: fixture.status, Collection: fixture.collection, Title: "Movie", PosterPath: poster, SourcePath: filepath.Join(root, strconv.Itoa(i)+".strm")}, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		refs := []store.ActorRef{}
		for _, name := range fixture.names {
			refs = append(refs, store.ActorRef{Name: name})
		}
		if err := app.db.ReplaceActors(id, refs); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			movieID = id
		}
	}
	type browseResult struct {
		Items []struct {
			Name, ID, ItemID, Type, ServerID string
			IsFolder                         bool
			Locations                        []string
			ImageTags                        map[string]string
		}
		TotalRecordCount, StartIndex int
	}
	get := func(path string) browseResult {
		t.Helper()
		resp, raw := embyRaw(t, ts, "GET", path, token, "")
		var result browseResult
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil || result.Items == nil {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		return result
	}
	for _, path := range []string{"/Persons", "/emby/Persons", "/persons", "/emby/persons", "/Library/VirtualFolders/Query", "/emby/Library/VirtualFolders/Query", "/library/virtualfolders/query", "/emby/library/virtualfolders/query"} {
		if resp, raw := embyRaw(t, ts, "GET", path, "", ""); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unprotected %s: %d %s", path, resp.StatusCode, raw)
		}
		result := get(path)
		if strings.Contains(strings.ToLower(path), "persons") {
			if result.TotalRecordCount != 4 || len(result.Items) != 4 {
				t.Fatalf("persons: %+v", result)
			}
			for _, person := range result.Items {
				if person.ID != entityId("Person", person.Name) || person.Type != "Person" || person.IsFolder || person.ServerID != app.serverID || person.ImageTags == nil {
					t.Fatalf("invalid Person DTO: %+v", person)
				}
			}
		} else if result.TotalRecordCount != 2 || len(result.Items) != 2 || result.Items[0].ID != externalLibraryID(1) || result.Items[0].ItemID != result.Items[0].ID || !reflect.DeepEqual(result.Items[0].Locations, []string{root}) {
			t.Fatalf("folders: %+v", result)
		}
	}
	for _, test := range []struct {
		query string
		names []string
		total int
	}{
		{"?ParentId=1", []string{"Alpha", "Beta", "Shared"}, 3},
		{"?ParentId=" + boxsetID("Group"), []string{"Beta", "Shared"}, 2},
		{"?ParentId=1&SortOrder=Descending&StartIndex=1&Limit=1", []string{"Beta"}, 3},
		{"?SearchTerm=alp", []string{"Alpha"}, 1},
		{"?NameStartsWith=BE", []string{"Beta"}, 1},
		{"?SearchTerm=missing", []string{}, 0},
		{"?IncludeItemTypes=Series", []string{}, 0},
		{"?MediaTypes=Audio", []string{}, 0},
		{"?StartIndex=9223372036854775807&Limit=1", []string{}, 4},
	} {
		result := get("/Persons" + test.query)
		names := []string{}
		for _, person := range result.Items {
			names = append(names, person.Name)
		}
		if !reflect.DeepEqual(names, test.names) || result.TotalRecordCount != test.total {
			t.Fatalf("%s: %+v", test.query, result)
		}
	}
	for _, query := range []string{"?UserId=2", "?ParentId=999", "?ParentId=unknown"} {
		if resp, raw := embyRaw(t, ts, "GET", "/Persons"+query, token, ""); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("invalid scope %s: %d %s", query, resp.StatusCode, raw)
		}
	}
	for _, test := range []struct {
		query        string
		count, start int
	}{
		{"?StartIndex=1&Limit=1", 1, 1},
		{"?StartIndex=9223372036854775807&Limit=1", 0, 9223372036854775807},
		{"?StartIndex=-1&Limit=1", 1, 0},
		{"?Limit=0", 0, 0},
	} {
		result := get("/Library/VirtualFolders/Query" + test.query)
		if len(result.Items) != test.count || result.TotalRecordCount != 2 || result.StartIndex != test.start {
			t.Fatalf("folder pagination %s: %+v", test.query, result)
		}
	}
	// A newly written avatar must invalidate a previously cached person listing.
	if result := get("/Persons"); result.Items[0].ImageTags["Primary"] != app.posterTag(poster) {
		t.Fatalf("missing movie poster fallback: %+v", result.Items[0])
	}
	if err := os.MkdirAll(app.avatarsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	image := []byte("avatar-bytes")
	if err := os.WriteFile(avatar.Path(app.avatarsDir(), "Alpha"), image, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.db.SetActorAvatar("Alpha", "https://example.test/alpha.jpg", avatar.Tag(image)); err != nil {
		t.Fatal(err)
	}
	result := get("/Persons")
	if result.Items[0].ImageTags["Primary"] != avatar.Tag(image) {
		t.Fatalf("avatar cache stale: %+v", result.Items[0])
	}
	result = get("/Persons?EnableImages=false")
	if len(result.Items[0].ImageTags) != 0 {
		t.Fatalf("images disabled: %+v", result.Items[0])
	}
	resp, raw := embyRaw(t, ts, "GET", "/Items/"+entityId("Person", "Alpha")+"/Images/Primary", "", "")
	if resp.StatusCode != http.StatusOK || string(raw) != string(image) {
		t.Fatalf("person image: %d %s", resp.StatusCode, raw)
	}
	if err := app.db.ReplaceActors(movieID, []store.ActorRef{{Name: "Changed"}}); err != nil {
		t.Fatal(err)
	}
	if err := app.db.BumpVersion(1); err != nil {
		t.Fatal(err)
	}
	result = get("/Persons?SearchTerm=Alpha")
	if result.TotalRecordCount != 0 {
		t.Fatalf("person cache stale: %+v", result)
	}
}

func TestEmbyEmptyBrowseQueries(t *testing.T) {
	app, ts, token := newProbeTestApp(t, t.TempDir())
	if err := app.db.DeleteLibrary(1); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/Persons", "/Library/VirtualFolders/Query"} {
		resp, raw := embyRaw(t, ts, "GET", path, token, "")
		var result struct {
			Items            []json.RawMessage
			TotalRecordCount int
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil || result.Items == nil || len(result.Items) != 0 || result.TotalRecordCount != 0 {
			t.Fatalf("empty %s: %d %s", path, resp.StatusCode, raw)
		}
	}
}

func TestWebIndexCompatibility(t *testing.T) {
	_, ts, _ := newProbeTestApp(t, t.TempDir())
	resp, raw := embyRaw(t, ts, "GET", "/web/index.html", "", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" || resp.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(raw), `id="app-shell"`) {
		t.Fatalf("index: %d %v %s", resp.StatusCode, resp.Header, raw)
	}
	_, admin := embyRaw(t, ts, "GET", "/admin", "", "")
	if string(raw) != string(admin) {
		t.Fatal("index must serve the existing dashboard")
	}
	if resp, _ := embyRaw(t, ts, "GET", "/web/missing.html", "", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown web page: %d", resp.StatusCode)
	}
}

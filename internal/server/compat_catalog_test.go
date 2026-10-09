package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestEmbyMissingCatalogEndpoints(t *testing.T) {
	root := t.TempDir()
	app, ts, token := newProbeTestApp(t, root)
	other, err := app.db.AddLibrary("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []store.Movie{
		{LibraryID: 1, Status: "success", Title: "A", Year: 2024, OfficialRating: "PG-13", Tags: []string{"Alpha", "Shared", "Alpha"}, Studios: []string{"Studio A"}},
		{LibraryID: 1, Status: "manual", Title: "B", Year: 2025, OfficialRating: "R", Tags: []string{"Beta", "Shared"}, Studios: []string{"Studio B"}, Collection: "Group"},
		{LibraryID: other.ID, Status: "success", Title: "Other", Year: 2030, OfficialRating: "PG", Tags: []string{"Other"}, Studios: []string{"Other Studio"}},
		{LibraryID: 1, Status: "pending", Title: "Hidden", Year: 1999, OfficialRating: "X", Tags: []string{"Hidden"}, Studios: []string{"Hidden Studio"}},
		{LibraryID: 1, Status: "success", Title: "Blank", Tags: []string{}, Studios: []string{}},
	}
	for i, movie := range fixtures {
		movie.SourcePath = filepath.Join(root, strconv.Itoa(i)+".strm")
		if _, err := app.db.UpsertMovie(movie, 1, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) (map[string]any, []string) {
		t.Helper()
		resp, raw := embyRaw(t, ts, "GET", path, token, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		items, ok := body["Items"].([]any)
		if !ok {
			t.Fatalf("Items must be an array: %s", raw)
		}
		names := []string{}
		for _, value := range items {
			names = append(names, value.(map[string]any)["Name"].(string))
		}
		return body, names
	}
	for _, test := range []struct {
		path  string
		names []string
	}{
		{"/emby/Tags", []string{"Alpha", "Beta", "Other", "Shared"}},
		{"/Years", []string{"2024", "2025", "2030"}},
		{"/emby/OfficialRatings", []string{"PG", "PG-13", "R"}},
		{"/emby/studios?EnableImages=false", []string{"Other Studio", "Studio A", "Studio B"}},
		{"/Tags?ParentId=" + externalLibraryID(1), []string{"Alpha", "Beta", "Shared"}},
		{"/Tags?ParentId=1&SearchTerm=alp", []string{"Alpha"}},
		{"/Tags?ParentId=1&NameStartsWith=BE", []string{"Beta"}},
		{"/Years?ParentId=" + boxsetID("Group"), []string{"2025"}},
		{"/OfficialRatings?ParentId=boxsets", []string{"R"}},
		{"/Tags?IncludeItemTypes=Series", []string{}},
		{"/Studios?MediaTypes=Audio", []string{}},
		{"/Years?ExcludeItemTypes=Movie", []string{}},
		{"/Tags?SearchTerm=missing", []string{}},
	} {
		body, names := get(test.path)
		if !reflect.DeepEqual(names, test.names) || body["TotalRecordCount"] != float64(len(test.names)) {
			t.Fatalf("%s: %+v", test.path, body)
		}
	}
	body, names := get("/Tags?ParentId=1&SortOrder=Descending&StartIndex=1&Limit=1")
	if !reflect.DeepEqual(names, []string{"Beta"}) || body["TotalRecordCount"] != float64(3) || body["StartIndex"] != float64(1) {
		t.Fatalf("bad pagination: %+v", body)
	}
	body, names = get("/Tags?StartIndex=9223372036854775807&Limit=1")
	if len(names) != 0 || body["TotalRecordCount"] != float64(4) {
		t.Fatalf("out-of-range page: %+v", body)
	}
	for _, path := range []string{"/Years?UserId=2", "/Tags?ParentId=unknown", "/Studios?ParentId=999"} {
		resp, raw := embyRaw(t, ts, "GET", path, token, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
	}
	_, raw := embyRaw(t, ts, "GET", "/Tags?SearchTerm=Alpha", token, "")
	var tags struct {
		Items []struct {
			ID string `json:"Id"`
		}
	}
	if err := json.Unmarshal(raw, &tags); err != nil || len(tags.Items) != 1 || tags.Items[0].ID != entityId("Tag", "Alpha") {
		t.Fatalf("unstable Tag ID: %s", raw)
	}
	_, raw = embyRaw(t, ts, "GET", "/Years", token, "")
	if !strings.Contains(string(raw), `"Id":"2024"`) {
		t.Fatalf("Year must use TagItem Name/Id: %s", raw)
	}
	// Returned filters must work on the media list and stay separate in cache.
	for _, test := range []struct {
		query string
		total int
	}{
		{"", 4},
		{"?OfficialRatings=R", 1},
		{"?OfficialRatings=PG%7CPG-13", 2},
		{"?ParentId=" + boxsetID("Group") + "&OfficialRatings=PG", 0},
		{"?Years=2024", 1},
		{"?TagIds=" + entityId("Tag", "Alpha"), 1},
		{"?StudioIds=" + entityId("Studio", "Studio B"), 1},
	} {
		resp, raw := embyRaw(t, ts, "GET", "/Items"+test.query, token, "")
		var result struct{ TotalRecordCount int }
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil || result.TotalRecordCount != test.total {
			t.Fatalf("filter %s: %d %s", test.query, resp.StatusCode, raw)
		}
	}
	// Version changes must invalidate cached catalogs after index writes.
	fixtures[0].SourcePath = filepath.Join(root, "0.strm")
	fixtures[0].Tags = []string{"Changed"}
	if _, err := app.db.UpsertMovie(fixtures[0], 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := app.db.BumpVersion(1); err != nil {
		t.Fatal(err)
	}
	_, names = get("/emby/Tags")
	if !reflect.DeepEqual(names, []string{"Beta", "Changed", "Other", "Shared"}) {
		t.Fatalf("stale catalog: %+v", names)
	}
}

func TestEmbyMissingUserLibraryAndThumbnailEndpoints(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "movie.strm"), "http://media.test/movie.mp4\n")
	writeFile(t, filepath.Join(root, "movie.nfo"), "<movie><title>Movie</title></movie>")
	app, ts, token := newProbeTestApp(t, root)
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{"/Users", "/Library/VirtualFolders", "/Tags", "/Studios", "/Years", "/OfficialRatings", "/Items/1/ThumbnailSet"} {
			resp, raw := embyRaw(t, ts, "GET", prefix+path, "", "")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("unprotected %s: %d %s", prefix+path, resp.StatusCode, raw)
			}
		}
		resp, raw := embyRaw(t, ts, "GET", prefix+"/Users", token, "")
		var users []struct {
			ID   string `json:"Id"`
			Name string
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &users) != nil || len(users) != 1 || users[0].ID != "1" || users[0].Name != "admin" {
			t.Fatalf("UserDto array: %d %s", resp.StatusCode, raw)
		}
		resp, raw = embyRaw(t, ts, "GET", prefix+"/Library/VirtualFolders", token, "")
		var folders []struct {
			Name, ItemID, ID, CollectionType string
			Locations                        []string
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &folders) != nil || len(folders) != 1 || folders[0].ID != externalLibraryID(1) || folders[0].ItemID != externalLibraryID(1) || folders[0].CollectionType != "movies" || !reflect.DeepEqual(folders[0].Locations, []string{root}) {
			t.Fatalf("VirtualFolderInfo array: %d %s", resp.StatusCode, raw)
		}
		resp, raw = embyRaw(t, ts, "GET", prefix+"/Items/1/ThumbnailSet?Width=320", token, "")
		var thumbnails struct {
			AspectRatio *float64
			Thumbnails  []struct {
				PositionTicks int64
				ImageTag      string
			}
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &thumbnails) != nil || thumbnails.AspectRatio == nil || thumbnails.Thumbnails == nil || len(thumbnails.Thumbnails) != 0 {
			t.Fatalf("ThumbnailSetInfo: %d %s", resp.StatusCode, raw)
		}
		resp, raw = embyRaw(t, ts, "POST", prefix+"/Users/1/Authenticate", "", `{"Pw":"password-1234"}`)
		var auth tsukimiLoginResponse
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &auth) != nil || auth.AccessToken == "" || auth.User.ID != "1" || !auth.User.Policy.IsAdministrator {
			t.Fatalf("login by ID: %d %s", resp.StatusCode, raw)
		}
		if resp, raw := embyRaw(t, ts, "GET", "/Users/Me", auth.AccessToken, ""); resp.StatusCode != http.StatusOK {
			t.Fatalf("login token unusable: %d %s", resp.StatusCode, raw)
		}
	}
	for _, test := range []struct {
		method, path, token, body string
		status                    int
	}{
		{"POST", "/Users/1/Authenticate", "", `{"Pw":"wrong"}`, http.StatusUnauthorized},
		{"POST", "/Users/1/Authenticate", "", `{}`, http.StatusUnauthorized},
		{"POST", "/Users/1/Authenticate", "", `{`, http.StatusBadRequest},
		{"POST", "/Users/d26056a09cd14e5784f0571f6bf4b31e/Authenticate", "", `{"Pw":"password-1234"}`, http.StatusNotFound},
		{"GET", "/Items/999/ThumbnailSet", token, "", http.StatusNotFound},
		{"GET", "/emby/items/1/thumbnailset", token, "", http.StatusOK},
		{"GET", "/Users?IsDisabled=true", token, "", http.StatusOK},
	} {
		resp, raw := embyRaw(t, ts, test.method, test.path, test.token, test.body)
		if resp.StatusCode != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, resp.StatusCode, raw)
		}
	}
	formResp, err := http.Post(ts.URL+"/emby/users/1/authenticate", "application/x-www-form-urlencoded", strings.NewReader("Pw=password-1234"))
	if err != nil {
		t.Fatal(err)
	}
	formBody, _ := io.ReadAll(formResp.Body)
	formResp.Body.Close()
	if formResp.StatusCode != http.StatusOK {
		t.Fatalf("form login: %d %s", formResp.StatusCode, formBody)
	}
	// Keep unavailable items hidden from the preview endpoint.
	movie, err := app.db.Movie(1)
	if err != nil {
		t.Fatal(err)
	}
	movie.Status = "pending"
	if _, err := app.db.UpsertMovie(movie, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if resp, raw := embyRaw(t, ts, "GET", "/Items/1/ThumbnailSet", token, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("hidden movie: %d %s", resp.StatusCode, raw)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/store"
	"emby-go/internal/subtitle"
	"github.com/gin-gonic/gin"
)

func TestEmbyFavoritesAcrossMediaAndCatalog(t *testing.T) {
	root := t.TempDir()
	a, server, token := newProbeTestApp(t, root)
	other, err := a.db.AddLibrary("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, status string, lib int64, genres []string) int64 {
		t.Helper()
		path := filepath.Join(root, title+".strm")
		writeFile(t, path, "https://media.invalid/video.mp4")
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib, SourcePath: path, Status: status, Title: title, Genres: genres, Tags: []string{"Tagged"}, Studios: []string{"Studio"}, Collection: "Collection", RuntimeSeconds: 100}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	one, two := add("A", "success", 1, []string{"Drama", "类型 / 特别"}), add("B", "manual", 1, []string{"Comedy"})
	add("C", "success", other.ID, []string{"Drama"})
	hidden := add("Hidden", "pending", 1, []string{"HiddenGenre"})
	if err = a.db.ReplaceActors(one, []store.ActorRef{{Name: "Alice"}}); err != nil {
		t.Fatal(err)
	}
	if err = a.db.ReplaceActors(two, []store.ActorRef{{Name: "Bob"}}); err != nil {
		t.Fatal(err)
	}
	if err = a.db.SavePlayback(one, 1234567, 3, "2026-10-19T00:00:00Z", 1234567); err != nil {
		t.Fatal(err)
	}
	var discoveries atomic.Int32
	a.discoverSubtitles = func(path string) []subtitle.File { discoveries.Add(1); return subtitle.Discover(path) }
	call := func(method, path string, status int) map[string]any {
		t.Helper()
		response, raw := embyRaw(t, server, method, path, token, "")
		if response.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, raw)
		}
		body := map[string]any{}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err, string(raw))
		}
		return body
	}
	list := func(path string, count int) []any {
		t.Helper()
		before := discoveries.Load()
		body := call("GET", path, 200)
		items, ok := body["Items"].([]any)
		if !ok || body["TotalRecordCount"] != float64(count) {
			t.Fatalf("%s: %v", path, body)
		}
		if discoveries.Load() != before {
			t.Fatal("favorite list discovered subtitles")
		}
		return items
	}
	list("/Items?Filters=IsFavorite", 0)
	list("/Genres?ParentId=1&IsFavorite=true", 0)
	list("/Genres?ParentId="+externalLibraryID(other.ID)+"&IsFavorite=true", 0)
	ids := []string{strconv.FormatInt(one, 10), entityId("Person", "Alice"), entityId("Genre", "Drama"), entityId("Genre", "类型 / 特别"), entityId("Tag", "Tagged"), entityId("Studio", "Studio"), boxsetID("Collection")}
	for i, id := range ids {
		path := "/Users/1/FavoriteItems/" + url.PathEscape(id)
		if i%2 == 1 {
			path = "/emby/users/1/favoriteitems/" + url.PathEscape(id)
		}
		for repeat := 0; repeat < 2; repeat++ {
			body := call("POST", path, 200)
			if body["IsFavorite"] != true || body["ItemId"] != id || body["Key"] != id || body["ServerId"] != a.serverID {
				t.Fatal(body)
			}
		}
		detail := call("GET", "/Items/"+url.PathEscape(id), 200)
		if detail["UserData"].(map[string]any)["IsFavorite"] != true {
			t.Fatal(detail)
		}
	}
	if state, _ := a.db.Data(one); state.PositionTicks != 1234567 || state.PlayCount != 3 {
		t.Fatal("favorite overwrote playback", state)
	}
	if state, _ := a.db.Data(two); state.IsFavorite {
		t.Fatal("entity favorites cascaded to movies")
	}
	list("/Items?IncludeItemTypes=Movie,Person,Genre,Tag,Studio,BoxSet&Filters=IsFavorite", len(ids))
	for _, path := range []string{"/Items?IncludeItemTypes=Movie&IsFavorite=true", "/Users/1/Items?ParentId=1&IsFavorite=true", "/Persons?IsFavorite=true", "/Studios?Filters=IsFavorite", "/Tags?IsFavorite=true", "/Items?IncludeItemTypes=BoxSet&IsFavorite=true", "/Items?ParentId=boxsets&IsFavorite=true", "/Items?IncludeItemTypes=Person&IsFavorite=false"} {
		list(path, 1)
	}
	list("/Items?IncludeItemTypes=Movie&IsFavorite=false", 2)
	list("/Items?ParentId=1&IsFavorite=false", 1)
	list("/Genres?ParentId=1&IsFavorite=true", 2)
	list("/Genres?ParentId="+externalLibraryID(other.ID)+"&IsFavorite=true", 1)
	list("/Genres?IsFavorite=false", 1)
	list("/emby/persons?userid=1&isfavorite=true&searchterm=ali", 1)
	list("/Items?IncludeItemTypes=Genre,Person&IsFavorite=false&SearchTerm=Bob", 1)
	list("/Items?IsFavorite=true&PersonIds="+url.QueryEscape(entityId("Person", "Alice")), 1)
	list("/Items?IsFavorite=true&Genres=Drama&IncludeItemTypes=Movie", 1)
	seen := map[string]bool{}
	for start := 0; start < len(ids); start += 2 {
		items := list("/Items?Filters=IsFavorite&Limit=2&StartIndex="+strconv.Itoa(start), len(ids))
		for _, raw := range items {
			id := raw.(map[string]any)["Id"].(string)
			if seen[id] {
				t.Fatal("duplicate page", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != len(ids) {
		t.Fatal("missing favorites", seen)
	}
	if page := list("/Items?Filters=IsFavorite&StartIndex=9999", len(ids)); len(page) != 0 {
		t.Fatal(page)
	}
	call("GET", "/Genres/Drama?UserId=1", 200)
	call("GET", "/Persons/Alice?UserId=1", 200)
	call("GET", "/Studios/Studio?UserId=1", 200)
	call("POST", "/Users/1/FavoriteItems/person:Alice", 200)
	call("DELETE", "/Users/1/FavoriteItems/person:Alice", 200)
	list("/Persons?IsFavorite=true", 0)
	list("/Items?IncludeItemTypes=Person&IsFavorite=false", 2)
	call("POST", "/Users/1/FavoriteItems/"+ids[2]+"/Delete", 200)
	list("/Genres?ParentId="+externalLibraryID(other.ID)+"&IsFavorite=true", 0)
	for _, id := range ids {
		body := call("DELETE", "/emby/users/1/favoriteitems/"+url.PathEscape(id), 200)
		if body["IsFavorite"] != false {
			t.Fatal(body)
		}
	}
	list("/Items?IsFavorite=true", 0)
	for _, id := range []string{entityId("Person", "Missing"), entityId("Genre", "HiddenGenre"), boxsetID("Missing"), strconv.FormatInt(hidden, 10), "999999"} {
		call("POST", "/Users/1/FavoriteItems/"+url.PathEscape(id), 404)
	}
	for _, path := range []string{"/Items?UserId=2&IsFavorite=true", "/Persons?userid=2", "/Genres?UserId=2", "/Items/" + ids[2] + "?UserId=2"} {
		call("GET", path, 404)
	}
	call("POST", "/Users/2/FavoriteItems/"+ids[0], 404)
	call("GET", "/Items?IsFavorite=invalid", 400)
	call("GET", "/Genres?IsFavorite=invalid", 400)
	response, _ := embyRaw(t, server, "POST", "/Users/1/FavoriteItems/"+ids[0], "", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("favorite requires authentication", response.StatusCode)
	}
}

func TestEmbyFavoriteSortingAndMediaTypes(t *testing.T) {
	for _, key := range []string{"SortName", "DateCreated", "PremiereDate", "ProductionYear", "CommunityRating", "PlayCount"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/Items?SortBy="+key+"&SortOrder=Descending&MediaTypes=Video", nil)
		items := []gin.H{
			{"Id": "1", "Name": "A", "Type": "Episode", "SeriesId": "s", "IndexNumber": 2, "DateCreated": "2025-01-01", "PremiereDate": "2025-01-01", "ProductionYear": 2025, "CommunityRating": 6.5, "UserData": gin.H{"PlayCount": 1}, "MediaType": "Video"},
			{"Id": "2", "Name": "B", "Type": "Episode", "SeriesId": "s", "IndexNumber": 1, "DateCreated": "2026-01-01", "PremiereDate": "2026-01-01", "ProductionYear": 2026, "CommunityRating": 8.5, "UserData": gin.H{"PlayCount": 2}, "MediaType": "Video"},
		}
		sortFavoriteItems(c, items)
		if items[0]["Id"] != "2" {
			t.Fatal(key, items)
		}
		if !favoriteCandidateMatches(c, items[0]) || favoriteCandidateMatches(c, gin.H{"Id": "3", "Type": "Person"}) {
			t.Fatal("MediaTypes filter")
		}
	}
}

func TestEmbyFavoriteTelevisionFoldersAreIndependent(t *testing.T) {
	root := t.TempDir()
	a, server, token := newProbeTestApp(t, root)
	library, err := a.db.AddLibrary("TV", t.TempDir(), "tvshows")
	if err != nil {
		t.Fatal(err)
	}
	show := filepath.Join(root, "Show")
	for i := 1; i <= 2; i++ {
		path := filepath.Join(root, "episode"+strconv.Itoa(i)+".strm")
		writeFile(t, path, "https://media.invalid/episode.mp4")
		_, err = a.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: path, Title: "Episode", Status: "success", Television: &store.Television{SeriesPath: show, Title: "Show", Season: 1, Episode: i}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	series, season := store.TVSeriesID(library.ID, show), store.TVSeasonID(library.ID, show, 1)
	for _, id := range []string{series, season} {
		response, raw := embyRaw(t, server, "POST", "/Users/1/FavoriteItems/"+url.PathEscape(id), token, "")
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode, string(raw))
		}
	}
	for _, path := range []string{"/Items?IncludeItemTypes=Series,Season&IsFavorite=true", "/Items?ParentId=" + externalLibraryID(library.ID) + "&IncludeItemTypes=Series,Season&IsFavorite=true"} {
		_, raw := embyRaw(t, server, "GET", path, token, "")
		var body struct{ TotalRecordCount int }
		if err = json.Unmarshal(raw, &body); err != nil || body.TotalRecordCount != 2 {
			t.Fatal(string(raw), err)
		}
	}
	_, raw := embyRaw(t, server, "GET", "/Shows/"+url.PathEscape(series)+"/Episodes?IsFavorite=true", token, "")
	var body struct{ TotalRecordCount int }
	if err = json.Unmarshal(raw, &body); err != nil || body.TotalRecordCount != 0 {
		t.Fatal("folder favorite leaked to children", string(raw), err)
	}
}

func TestEmbyFavoriteDirectorAndHiddenEntities(t *testing.T) {
	root := t.TempDir()
	a, server, token := newProbeTestApp(t, root)
	movie := store.Movie{LibraryID: 1, SourcePath: filepath.Join(root, "movie.strm"), Title: "Film", Status: "success", Director: "Director"}
	id, err := a.db.UpsertMovie(movie, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.ReplaceActors(id, []store.ActorRef{{Name: "Actor"}}); err != nil {
		t.Fatal(err)
	}
	call := func(method, path string) map[string]any {
		t.Helper()
		response, raw := embyRaw(t, server, method, path, token, "")
		if response.StatusCode != 200 {
			t.Fatal(path, response.StatusCode, string(raw))
		}
		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, name := range []string{"Director", "Actor"} {
		call("POST", "/Users/1/FavoriteItems/"+entityId("Person", name))
	}
	if result := call("GET", "/Persons?IsFavorite=true"); result["TotalRecordCount"] != float64(2) {
		t.Fatal(result)
	}
	if result := call("GET", "/Items?PersonIds="+entityId("Person", "Director")); result["TotalRecordCount"] != float64(1) {
		t.Fatal(result)
	}
	if result := call("GET", "/api/admin/items?person=Director"); result["total"] != float64(1) {
		t.Fatal(result)
	}
	for _, suffix := range []string{"true", "false"} {
		result := call("GET", "/api/admin/actors?favorite="+suffix)
		want := float64(0)
		if suffix == "true" {
			want = 1
		}
		if result["total"] != want {
			t.Fatal(result)
		}
		if suffix == "true" && result["items"].([]any)[0].(map[string]any)["favorite"] != true {
			t.Fatal(result)
		}
	}
	movie.Status = "pending"
	if _, err = a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if result := call("GET", "/Items?Filters=IsFavorite"); result["TotalRecordCount"] != float64(0) {
		t.Fatal(result)
	}
	movie.Status = "manual"
	if _, err = a.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if result := call("GET", "/Items?Filters=IsFavorite"); result["TotalRecordCount"] != float64(2) {
		t.Fatal("favorite state lost after metadata refresh", result)
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/store"
)

func createViewer(t *testing.T, a *App, admin, name string) (string, string) {
	t.Helper()
	body := fmt.Sprintf(`{"Username":%q,"Pw":"password-1234"}`, name)
	w := accountRequest(a, "POST", "/api/admin/users", admin, body)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var user struct{ Id string }
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	w = accountRequest(a, "POST", "/Users/AuthenticateByName", "", body)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var login struct{ AccessToken string }
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	return user.Id, login.AccessToken
}

func TestUserBrowsingAndIsolation(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, token := createViewer(t, a, admin, "viewer")
	otherID, otherToken := createViewer(t, a, admin, "other")
	lib, err := a.db.AddLibrary("Allowed", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	denied, err := a.db.AddLibrary("Denied", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "film.strm"), Title: "Film", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item := strconv.FormatInt(id, 10)
	if w := accountRequest(a, "GET", "/Items/"+item, token, ""); w.Code != 404 {
		t.Fatalf("default grant: %d", w.Code)
	}
	policy := fmt.Sprintf(`{"EnableAllFolders":false,"EnabledFolders":[%q]}`, externalLibraryID(lib.ID))
	for _, user := range []string{uid, otherID} {
		if w := accountRequest(a, "POST", "/Users/"+user+"/Policy", admin, policy); w.Code != 204 {
			t.Fatalf("grant: %d %s", w.Code, w.Body)
		}
	}
	w := accountRequest(a, "GET", "/api/browse/libraries", token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Allowed") || strings.Contains(w.Body.String(), "Denied") || strings.Contains(w.Body.String(), filepath.Base(denied.Path)) {
		t.Fatalf("browse libraries: %d %s", w.Code, w.Body)
	}
	for _, tkn := range []string{admin, token, otherToken} {
		if w := accountRequest(a, "GET", "/Items/"+item, tkn, ""); w.Code != 200 {
			t.Fatalf("warm detail: %d %s", w.Code, w.Body)
		}
	}
	if w := accountRequest(a, "POST", "/Users/"+uid+"/FavoriteItems/"+item, token, ""); w.Code != 200 {
		t.Fatalf("favorite: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, "POST", "/Sessions/Playing/Progress", token, fmt.Sprintf(`{"ItemId":%q,"PositionTicks":12300000}`, item)); w.Code != 204 {
		t.Fatalf("progress: %d %s", w.Code, w.Body)
	}
	for _, entry := range []struct {
		token    string
		favorite bool
		position int64
	}{{token, true, 12300000}, {otherToken, false, 0}, {admin, false, 0}} {
		w := accountRequest(a, "GET", "/Items/"+item, entry.token, "")
		var result struct {
			UserData struct {
				IsFavorite            bool
				PlaybackPositionTicks int64
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.UserData.IsFavorite != entry.favorite || result.UserData.PlaybackPositionTicks != entry.position {
			t.Fatalf("cached user data: %d %s", w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/admin/libraries", "/api/admin/status", "/api/admin/users", "/api/admin/config", "/api/admin/apikeys"} {
		if w := accountRequest(a, "GET", path, token, ""); w.Code != 403 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	if w := accountRequest(a, "POST", "/Users/"+uid+"/Policy", token, policy); w.Code != 403 {
		t.Fatalf("self escalation: %d", w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/"+otherID+"/FavoriteItems/"+item, token, ""); w.Code != 404 {
		t.Fatalf("other user write: %d", w.Code)
	}
	if w := accountRequest(a, "PUT", "/api/admin/users/"+uid, admin, `{"Username":"viewer","IsDisabled":true}`); w.Code != 204 {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, "GET", "/Users/Me", token, ""); w.Code != 401 {
		t.Fatalf("disabled token: %d", w.Code)
	}
	if w := accountRequest(a, "DELETE", "/api/admin/users/1", admin, ""); w.Code != 400 {
		t.Fatalf("delete original admin: %d", w.Code)
	}
}

func TestUserHLSUsesSessionOwner(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, _ := createViewer(t, a, admin, "viewer")
	viewerID, _ := strconv.ParseInt(uid, 10, 64)
	lib, err := a.db.AddLibrary("Viewer library", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "film.strm"), Title: "Film", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.mediaSessions = map[string]*audioSession{"test-session": {userID: viewerID, item: strconv.FormatInt(id, 10), dir: dir}}
	defer delete(a.mediaSessions, "test-session")
	if err = a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "/emby"} {
		path := fmt.Sprintf("%s/Videos/%d/hls/test-session/index.m3u8", prefix, id)
		if w := accountRequest(a, "GET", path, "", ""); w.Code != 200 {
			t.Fatalf("owner HLS: %d %s", w.Code, w.Body)
		}
	}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if w := accountRequest(a, "GET", fmt.Sprintf("/Videos/%d/hls/test-session/index.m3u8", id), "", ""); w.Code != 404 {
		t.Fatalf("revoked HLS: %d", w.Code)
	}
}

func TestUserSubtitleUsesDeliveryOwner(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, token := createViewer(t, a, admin, "viewer")
	viewerID, _ := strconv.ParseInt(uid, 10, 64)
	lib, err := a.db.AddLibrary("Viewer subtitles", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(lib.Path, "film.strm")
	writeFile(t, path, "https://media.invalid/film.mp4")
	writeFile(t, filepath.Join(lib.Path, "film.eng.srt"), "1\n00:00:01,000 --> 00:00:03,000\nViewer subtitle\n")
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: path, Title: "Film", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	grant := store.LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(grant); err != nil {
		t.Fatal(err)
	}
	if err = a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	w := accountRequest(a, "GET", fmt.Sprintf("/Items/%d/PlaybackInfo", id), token, "")
	var body map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("playback: %d %s", w.Code, w.Body)
	}
	delivery := findSubtitle(t, playbackSource(t, body), "eng")["DeliveryUrl"].(string)
	for _, prefix := range []string{"", "/emby"} {
		for _, credential := range []string{"", token} {
			if w = accountRequest(a, "GET", prefix+delivery, credential, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Viewer subtitle") {
				t.Fatalf("subtitle owner: %d %s", w.Code, w.Body)
			}
		}
	}
	if w = accountRequest(a, "GET", delivery, admin, ""); w.Code != 404 {
		t.Fatalf("admin bypass: %d", w.Code)
	}
	u, _ := url.Parse(delivery)
	query := u.Query()
	query.Set("SubtitleUserId", "1")
	u.RawQuery = query.Encode()
	if w = accountRequest(a, "GET", u.String(), "", ""); w.Code != 401 {
		t.Fatalf("tampered owner: %d", w.Code)
	}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	if w = accountRequest(a, "GET", delivery, "", ""); w.Code != 404 {
		t.Fatalf("revoked delivery: %d", w.Code)
	}
	if err = a.db.ForUser(viewerID).SaveLibraryAccessPolicy(grant); err != nil {
		t.Fatal(err)
	}
	if err = a.db.EditUser(viewerID, "viewer", "", true); err != nil {
		t.Fatal(err)
	}
	if w = accountRequest(a, "GET", delivery, "", ""); w.Code != 401 {
		t.Fatalf("disabled delivery: %d", w.Code)
	}
}

func TestUserLifecycleAndSessionOwnership(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, token := createViewer(t, a, admin, "viewer")
	viewerID, _ := strconv.ParseInt(uid, 10, 64)
	if w := accountRequest(a, "POST", "/api/admin/users", admin, `{"Username":"VIEWER","Pw":"password-1234"}`); w.Code != 409 {
		t.Fatalf("duplicate: %d", w.Code)
	}
	if w := accountRequest(a, "PUT", "/api/admin/users/1", admin, `{"Username":"admin","IsDisabled":true}`); w.Code != 400 {
		t.Fatalf("disable admin: %d", w.Code)
	}
	if w := accountRequest(a, "PUT", "/api/admin/users/"+uid, admin, `{"Username":"viewer","NewPassword":"reset-password"}`); w.Code != 204 {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, "GET", "/Users/Me", token, ""); w.Code != 401 {
		t.Fatalf("reset token: %d", w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"viewer","Pw":"password-1234"}`); w.Code != 401 {
		t.Fatalf("old password: %d", w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"viewer","Pw":"reset-password"}`); w.Code != 200 {
		t.Fatalf("new password: %d", w.Code)
	}
	done := make(chan struct{})
	close(done)
	cancelled := false
	a.mediaSessions = map[string]*audioSession{"owned": {userID: viewerID, item: "1", cancel: func() { cancelled = true }, done: done}}
	defer delete(a.mediaSessions, "owned")
	a.stopMediaSession("1", "owned")
	if cancelled {
		t.Fatal("other user stopped session")
	}
	(&App{appState: a.appState, db: a.db.ForUser(viewerID)}).stopMediaSession("1", "owned")
	if !cancelled {
		t.Fatal("owner could not stop session")
	}
	if w := accountRequest(a, "DELETE", "/api/admin/users/"+uid, admin, ""); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"viewer","Pw":"reset-password"}`); w.Code != 401 {
		t.Fatalf("deleted login: %d", w.Code)
	}
	if w := accountRequest(a, "GET", "/Users/Me", admin, ""); w.Code != 200 {
		t.Fatalf("admin session lost: %d", w.Code)
	}
}

func TestUserTelevisionBrowsingAndProgress(t *testing.T) {
	a := accountApp(t)
	admin := accountLogin(t, a)
	uid, token := createViewer(t, a, admin, "viewer")
	for _, typ := range []string{"tvshows", "mixed"} {
		lib, err := a.db.AddLibrary(typ, t.TempDir(), typ)
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(lib.Path, "show")
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "episode.strm"), Title: "Viewer episode", Status: "success", RuntimeSeconds: 600,
			Television: &store.Television{Title: "Viewer show", SeriesPath: root, Season: 1, Episode: 1}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "episode2.strm"), Title: "Next episode", Status: "success", RuntimeSeconds: 600,
			Television: &store.Television{Title: "Viewer show", SeriesPath: root, Season: 1, Episode: 2}}, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
		grant := fmt.Sprintf(`{"EnableAllFolders":false,"EnabledFolders":[%q]}`, externalLibraryID(lib.ID))
		if w := accountRequest(a, "POST", "/Users/"+uid+"/Policy", admin, grant); w.Code != 204 {
			t.Fatalf("grant: %d", w.Code)
		}
		series := url.PathEscape(store.TVSeriesID(lib.ID, root))
		for _, prefix := range []string{"", "/emby"} {
			for _, route := range []struct {
				path  string
				count int
			}{{"/Shows/" + series + "/Seasons", 1}, {"/Shows/" + series + "/Episodes", 2}, {"/Shows/NextUp?SeriesId=" + series, 0}, {"/Users/" + uid + "/Items?ParentId=" + externalLibraryID(lib.ID), 1}} {
				w := accountRequest(a, "GET", prefix+route.path, token, "")
				var result struct{ TotalRecordCount int }
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.TotalRecordCount != route.count {
					t.Fatalf("%s %s: %d %s", typ, route.path, w.Code, w.Body)
				}
			}
		}
		if w := accountRequest(a, "POST", fmt.Sprintf("/Users/%s/PlayedItems/%d", uid, id), token, ""); w.Code != 204 {
			t.Fatalf("played: %d %s", w.Code, w.Body)
		}
		for _, entry := range []struct {
			token string
			count int
		}{{token, 1}, {admin, 0}} {
			w := accountRequest(a, "GET", "/Shows/NextUp?SeriesId="+series, entry.token, "")
			var result struct{ TotalRecordCount int }
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.TotalRecordCount != entry.count {
				t.Fatalf("next-up isolation: %d %s", w.Code, w.Body)
			}
		}
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"emby-go/internal/store"
)

func accountRequest(a *App, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("X-Emby-Token", token)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func accountApp(t *testing.T) *App {
	t.Helper()
	a, err := newApp(config.Config{DBPath: filepath.Join(t.TempDir(), "account.db")}, cache.NewMemory(512))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	if err := a.db.InitializeAdministrator("admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	a.setAdminName("admin")
	return a
}

func accountLogin(t *testing.T, a *App) string {
	t.Helper()
	w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var result struct{ AccessToken string }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.AccessToken
}

func TestAccountLogoutRevokesToken(t *testing.T) {
	a := accountApp(t)
	token, other := accountLogin(t, a), accountLogin(t, a)
	a.cache.Set("token:"+token, []byte("1"), time.Hour)
	if w := accountRequest(a, "GET", "/Users/Me", token, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := accountRequest(a, "POST", "/emby/sessions/logout", token, ""); w.Code != 204 {
		t.Fatal(w.Code)
	}
	a.cache.Set("token:"+token, []byte("1"), time.Hour)
	if w := accountRequest(a, "GET", "/Users/Me", token, ""); w.Code != 401 {
		t.Fatalf("revoked token accepted: %d", w.Code)
	}
	if w := accountRequest(a, "GET", "/Users/Me", other, ""); w.Code != 200 {
		t.Fatalf("other session revoked: %d", w.Code)
	}
}

func TestAccountAuthorizationHeaders(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	for _, scheme := range []string{"Emby", "MediaBrowser"} {
		for _, value := range []string{scheme + ` Token="` + token + `", Client="test"`, scheme + ` Client="test", Token="` + token + `"`} {
			for _, header := range []string{"Authorization", "X-Emby-Authorization"} {
				r := httptest.NewRequest("GET", "/Users/Me", nil)
				r.Header.Set(header, value)
				w := httptest.NewRecorder()
				a.Handler().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Errorf("%s %s: %d", header, scheme, w.Code)
				}
			}
		}
	}
}

func TestAccountPolicyValidationAndRestore(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Allowed", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"EnableAllFolders":false,"EnabledFolders":["` + externalLibraryID(lib.ID) + `"]}`
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, body); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	version := a.db.AccessVersion()
	for _, invalid := range []string{`{}`, `{"EnableAllFolders":false}`, `{"EnableAllFolders":false,"EnabledFolders":null}`, `{"EnableAllFolders":false,"EnabledFolders":["wrong"]}`, `{"EnableAllFolders":false,"EnabledFolders":["999999999"]}`} {
		if w := accountRequest(a, "POST", "/Users/1/Policy", token, invalid); w.Code != 400 {
			t.Errorf("invalid policy: %d %s", w.Code, w.Body)
		}
	}
	if version != a.db.AccessVersion() || !a.db.CanAccessLibrary(lib.ID) {
		t.Fatal("invalid policy changed existing grants")
	}
	if w := accountRequest(a, "POST", "/Users/2/Policy", token, body); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, `{"EnableAllFolders":true,"EnabledFolders":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	var me struct {
		Policy struct {
			EnableAllFolders bool
			EnabledFolders   []string
		}
	}
	w := accountRequest(a, "GET", "/Users/Me", token, "")
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil || !me.Policy.EnableAllFolders || me.Policy.EnabledFolders == nil {
		t.Fatal(err, w.Body)
	}
}

func TestAccountLibraryPolicy(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	one, err := a.db.AddLibrary("Allowed", filepath.Join(t.TempDir(), "one"))
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.db.AddLibrary("Denied", filepath.Join(t.TempDir(), "two"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lib := range []store.Library{one, two} {
		_, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, "movie.strm"), Title: lib.Name, Status: "success", Tags: []string{lib.Name}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	// Warm the unrestricted response before changing policy.
	if w := accountRequest(a, "GET", "/Items", token, ""); !strings.Contains(w.Body.String(), "Denied") {
		t.Fatal(w.Body)
	}
	body := `{"EnableAllFolders":false,"EnabledFolders":["` + externalLibraryID(one.ID) + `"]}`
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, body); w.Code != 204 {
		t.Fatalf("policy: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/Items", "/Users/1/Views", "/Tags", "/Library/VirtualFolders", "/api/admin/items"} {
		w := accountRequest(a, "GET", path, token, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "Denied") {
			t.Fatalf("leak %s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := accountRequest(a, "GET", "/Items/2", token, ""); w.Code != 404 {
		t.Fatalf("direct detail: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, "GET", "/Items/2/PlaybackInfo", token, ""); w.Code != 404 {
		t.Fatalf("direct playback: %d %s", w.Code, w.Body)
	}
}

func TestAccountCollectionCacheInvalidation(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	var allowed int64
	for _, name := range []string{"Allowed", "Denied"} {
		lib, err := a.db.AddLibrary(name, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if name == "Allowed" {
			allowed = lib.ID
		}
		for i := 0; i < 2; i++ {
			_, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, strconv.Itoa(i)+".strm"), Title: name, Status: "success", Collection: name}, 0, time.Now())
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if names := a.cachedCollections(); len(names) != 2 {
		t.Fatal(names)
	}
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, `{"EnableAllFolders":false,"EnabledFolders":["`+externalLibraryID(allowed)+`"]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	if names := a.cachedCollections(); len(names) != 1 || names[0] != "Allowed" {
		t.Fatal("cached collections leaked revoked library", names)
	}
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, `{"EnableAllFolders":false,"EnabledFolders":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	w := accountRequest(a, "GET", "/Users/1/Views", token, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"boxsets"`) {
		t.Fatal("empty grants retained a collection view", w.Code, w.Body)
	}
}

func TestAccountTelevisionAccess(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	root := t.TempDir()
	lib, err := a.db.AddLibrary("Private TV", root, "tvshows")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "show", "episode.strm"), Title: "Private Episode", Status: "success", Television: &store.Television{Title: "Private Show", SeriesPath: filepath.Join(root, "show"), Season: 1, Episode: 1}}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	series := url.PathEscape(store.TVSeriesID(lib.ID, filepath.Join(root, "show")))
	season := url.PathEscape(store.TVSeasonID(lib.ID, filepath.Join(root, "show"), 1))
	if w := accountRequest(a, "GET", "/Items/"+series, token, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, `{"EnableAllFolders":false,"EnabledFolders":[]}`); w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{"/Items/" + series, "/Items/" + season, "/Items/" + series + "/Images/Primary", "/Shows/" + series + "/Seasons", "/Shows/" + series + "/Episodes", "/Shows/NextUp?SeriesId=" + series, "/Users/1/Items?ParentId=" + season, "/Items/" + strconv.FormatInt(id, 10)} {
			if w := accountRequest(a, "GET", prefix+path, token, ""); w.Code != 404 {
				t.Fatal(path, w.Code, w.Body)
			}
		}
		w := accountRequest(a, "GET", prefix+"/Shows/NextUp", token, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "Private") {
			t.Fatal(w.Code, w.Body)
		}
	}
}

func TestAccountCredentialChange(t *testing.T) {
	a := accountApp(t)
	token, other := accountLogin(t, a), accountLogin(t, a)
	key, err := a.db.CreateAPIKey("service")
	if err != nil {
		t.Fatal(err)
	}
	w := accountRequest(a, "PUT", "/api/admin/account", token, `{"Username":"owner","CurrentPassword":"wrong","NewPassword":"new-password-1234"}`)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	if w := accountRequest(a, "GET", "/Users/Me", token, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = accountRequest(a, "PUT", "/api/admin/account", token, `{"Username":"owner","CurrentPassword":"password-1234","NewPassword":"new-password-1234"}`)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	for _, revoked := range []string{token, other} {
		if w := accountRequest(a, "GET", "/Users/Me", revoked, ""); w.Code != 401 {
			t.Fatalf("old session accepted: %d", w.Code)
		}
	}
	if w := accountRequest(a, "GET", "/Users/Me", key.Key, ""); w.Code != 200 {
		t.Fatalf("service key invalidated: %d", w.Code)
	}
	w = accountRequest(a, "POST", "/emby/users/1/authenticate", "", `{"Pw":"new-password-1234"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"Name":"owner"`) {
		t.Fatal(w.Code, w.Body)
	}
	if w := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestAccountDeniedMediaAndCookie(t *testing.T) {
	a := accountApp(t)
	login := accountRequest(a, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`)
	var result struct{ AccessToken string }
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	token := result.AccessToken
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", cookies)
	}
	root := t.TempDir()
	lib, err := a.db.AddLibrary("Private", root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "movie.strm"), "https://media.test/private.mp4")
	writeJPEGImage(t, filepath.Join(root, "poster.jpg"), 10, 10)
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "movie.strm"), PosterPath: filepath.Join(root, "poster.jpg"), Title: "Private", Status: "success", AdditionalParts: []string{"part2.strm"}}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sid := strconv.FormatInt(id, 10)
	imagePath := "/Items/" + sid + "/Images/Primary"
	if w := accountRequest(a, "GET", imagePath, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", imagePath, nil)
	r.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(w.Code, w.Header())
	}
	etag := w.Header().Get("ETag")
	// Cookies must not authorize state changes (CSRF boundary).
	r = httptest.NewRequest("POST", "/Users/1/Policy", strings.NewReader(`{"EnableAllFolders":false,"EnabledFolders":[]}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("cookie authorized mutation", w.Code)
	}
	if w := accountRequest(a, "POST", "/Users/1/Policy", token, `{"EnableAllFolders":false,"EnabledFolders":[]}`); w.Code != 204 {
		t.Fatal(w.Code)
	}
	paths := []string{
		"/Items/" + sid, imagePath, "/Items/" + sid + "/Images", "/Items/" + sid + "/ThumbnailSet",
		"/Items/" + sid + "/Similar", "/Items/" + sid + "/PlaybackInfo", "/Videos/" + sid + "/AdditionalParts",
		"/Videos/" + sid + "/stream", "/Videos/" + sid + "/stream.mp4", "/Videos/" + sid + "/proxy", "/Videos/" + sid + "/trailer",
		"/Videos/" + sid + "/hls/oldsession/init.mp4", "/Videos/" + sid + "/mediasource_" + sid + "/Subtitles/0/Stream.vtt",
		"/Items/" + externalLibraryID(lib.ID), "/Users/1/Items?ParentId=" + externalLibraryID(lib.ID),
	}
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range paths {
			for _, variant := range []string{path, strings.ToLower(path)} {
				if w := accountRequest(a, "GET", prefix+variant, token, ""); w.Code != 404 {
					t.Errorf("denied %s%s: %d %s", prefix, variant, w.Code, w.Body)
				}
			}
		}
	}
	for _, path := range []string{"/Users/1/FavoriteItems/" + sid, "/Users/1/PlayedItems/" + sid} {
		if w := accountRequest(a, "POST", path, token, ""); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped"} {
		if w := accountRequest(a, "POST", path, token, `{"ItemId":"`+sid+`","PositionTicks":10000000}`); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	r = httptest.NewRequest("GET", imagePath, nil)
	r.AddCookie(cookies[0])
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("conditional request bypassed policy", w.Code)
	}
	for _, path := range []string{"/Items", "/Items?Ids=" + sid, "/Users/1/Items/Resume", "/Users/1/Items/Latest", "/Users/1/Views", "/Tags", "/Genres", "/Persons", "/api/admin/items"} {
		w := accountRequest(a, "GET", path, token, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "Private") {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	// The administrator can still manage grants, even with an empty allowlist.
	if w := accountRequest(a, "GET", "/api/admin/account", token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Private") {
		t.Fatal(w.Code, w.Body)
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestEmbyImageHEAD(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	root := t.TempDir()
	lib, err := a.db.AddLibrary("Images", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"poster.jpg", "landscape.jpg", "backdrop.jpg", "backdrop1.jpg"} {
		writeJPEGImage(t, filepath.Join(root, name), 120, 80)
	}
	movieID, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(root, "movie.strm"), OutputDir: root, Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(root, "Show")
	if err := os.Mkdir(seriesPath, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"poster.jpg", "landscape.jpg", "backdrop.jpg", "backdrop1.jpg"} {
		writeJPEGImage(t, filepath.Join(seriesPath, name), 120, 80)
	}
	_, err = a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(seriesPath, "episode.strm"), OutputDir: seriesPath, Status: "success", Television: &store.Television{Title: "Show", SeriesPath: seriesPath, Season: 1, Episode: 1}}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, credential, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if credential != "" {
			r.Header.Set("X-Emby-Token", credential)
		}
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	ids := []string{strconv.FormatInt(movieID, 10), store.TVSeriesID(lib.ID, seriesPath), store.TVSeasonID(lib.ID, seriesPath, 1)}
	var protectedPath, protectedETag string
	for _, prefix := range []string{"", "/emby"} {
		for _, route := range []string{"/Items/%s/Images/", "/items/%s/images/"} {
			for _, id := range ids {
				for _, image := range []string{"Primary", "Thumb", "Backdrop", "Backdrop/0", "Backdrop/1", "Primary/0"} {
					for _, query := range []string{"", "?maxWidth=30"} {
						path := prefix + fmt.Sprintf(route, id) + image + query
						head := request(http.MethodHead, path, "", "")
						get := request(http.MethodGet, path, "", "")
						if get.Code != 200 || head.Code != 200 || head.Body.Len() != 0 {
							t.Fatalf("%s: GET=%d HEAD=%d body=%d", path, get.Code, head.Code, head.Body.Len())
						}
						for _, header := range []string{"Content-Type", "ETag", "Cache-Control", "Vary"} {
							if get.Header().Get(header) == "" || get.Header().Get(header) != head.Header().Get(header) {
								t.Errorf("%s %s: GET=%q HEAD=%q", path, header, get.Header().Get(header), head.Header().Get(header))
							}
						}
						if head.Header().Get("Content-Length") != strconv.Itoa(get.Body.Len()) {
							t.Errorf("%s: HEAD length=%s GET body=%d", path, head.Header().Get("Content-Length"), get.Body.Len())
						}
						if query != "" && head.Header().Get("Content-Type") != "image/webp" {
							t.Errorf("%s: resized HEAD did not use the thumbnail response", path)
						}
						if head.Header().Get("Cache-Control") != "private, no-cache" {
							t.Errorf("%s: cache policy %s", path, head.Header().Get("Cache-Control"))
						}
						etag := head.Header().Get("ETag")
						conditional := request(http.MethodHead, path, "", etag)
						if conditional.Code != 304 || conditional.Body.Len() != 0 || conditional.Header().Get("ETag") != etag {
							t.Errorf("%s: conditional HEAD=%d body=%d", path, conditional.Code, conditional.Body.Len())
						}
						protectedPath, protectedETag = path, etag
					}
				}
				for _, image := range []string{"Logo", "Banner", "Art", "Backdrop/99", "Backdrop/-1", "Backdrop/invalid", "Primary/1"} {
					path := prefix + fmt.Sprintf(route, id) + image
					if w := request(http.MethodHead, path, "", ""); w.Code != 404 {
						t.Errorf("%s: expected absent image, got %d", path, w.Code)
					}
				}
			}
		}
	}
	if w := request(http.MethodHead, "/emby/Items/999999/Images/Primary", "", ""); w.Code != 404 {
		t.Errorf("unknown image: %d", w.Code)
	}
	if err := a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{}}); err != nil {
		t.Fatal(err)
	}
	if w := request(http.MethodHead, protectedPath, token, protectedETag); w.Code != 404 {
		t.Errorf("cached image bypassed library access: %d", w.Code)
	}
	if w := request(http.MethodHead, protectedPath+"&UserId=2", token, ""); w.Code != 404 {
		t.Errorf("cross-user image: %d", w.Code)
	}
	if probes, err := a.db.Probes(1000); err != nil || len(probes) != 0 {
		t.Fatalf("registered image HEADs must not create probes: %v %v", probes, err)
	}
}

func TestEmbyMediaFolders(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	libs := []store.Library{}
	for _, kind := range []string{store.LibraryMovies, store.LibraryTVShows, store.LibraryMixed} {
		lib, err := a.db.AddLibrary(kind, t.TempDir(), kind)
		if err != nil {
			t.Fatal(err)
		}
		libs = append(libs, lib)
	}
	writeJPEGImage(t, filepath.Join(libs[0].Path, "poster.jpg"), 40, 20)
	for i := range 2 {
		if _, err := a.db.UpsertMovie(store.Movie{LibraryID: libs[0].ID, SourcePath: filepath.Join(libs[0].Path, fmt.Sprintf("movie%d.strm", i)), Status: "success", Collection: "Group"}, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.cachedCollections()) != 1 {
		t.Fatal("fixture must contain a synthetic boxset view")
	}
	check := func(path, credential string, want []store.Library) {
		t.Helper()
		w := accountRequest(a, http.MethodGet, path, credential, "")
		var result struct {
			Items            []map[string]any
			TotalRecordCount int
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Items == nil || len(result.Items) != len(want) || result.TotalRecordCount != len(want) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: cache policy %s", path, w.Header().Get("Cache-Control"))
		}
		for i, lib := range want {
			item := result.Items[i]
			if item["Id"] != externalLibraryID(lib.ID) || item["Name"] != lib.Name || item["Type"] != "CollectionFolder" || item["IsFolder"] != true || item["ServerId"] != a.serverID {
				t.Errorf("%s: wrong folder DTO: %v", path, item)
			}
			if item["CollectionType"] != lib.CollectionType() && lib.CollectionType() != "" {
				t.Errorf("%s: collection type %v", path, item)
			}
			if _, ok := item["Locations"]; ok {
				t.Errorf("%s: returned VirtualFolderInfo instead of BaseItemDto", path)
			}
		}
		if len(want) > 0 && want[0].ID == libs[0].ID && result.Items[0]["ImageTags"] == nil {
			t.Errorf("%s: library cover omitted", path)
		}
	}
	for _, path := range []string{"/Library/MediaFolders", "/library/mediafolders", "/emby/Library/MediaFolders", "/emby/library/mediafolders"} {
		if w := accountRequest(a, http.MethodGet, path, "", ""); w.Code != 401 {
			t.Errorf("%s: unauthenticated=%d", path, w.Code)
		}
		check(path, token, libs)
		check(path+"?IsHidden=false", token, libs)
		check(path+"?ishidden=true", token, nil)
		if w := accountRequest(a, http.MethodGet, path+"?UserId=2", token, ""); w.Code != 404 {
			t.Errorf("%s: cross-user=%d", path, w.Code)
		}
	}
	viewer, err := a.db.CreateUser("viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{libs[1].ID}}); err != nil {
		t.Fatal(err)
	}
	w := accountRequest(a, http.MethodPost, "/Users/AuthenticateByName", "", `{"Username":"viewer","Pw":"password-1234"}`)
	var login struct{ AccessToken string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || login.AccessToken == "" {
		t.Fatalf("viewer login: %d %s", w.Code, w.Body)
	}
	check("/emby/Library/MediaFolders?UserId="+strconv.FormatInt(viewer.ID, 10), login.AccessToken, []store.Library{libs[1]})
	if err := a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{}}); err != nil {
		t.Fatal(err)
	}
	check("/Library/MediaFolders", token, nil)
}

func TestEmbySpecialFeatures(t *testing.T) {
	a := accountApp(t)
	token := accountLogin(t, a)
	lib, err := a.db.AddLibrary("Features", t.TempDir(), store.LibraryMixed)
	if err != nil {
		t.Fatal(err)
	}
	seriesPath := filepath.Join(lib.Path, "Show")
	validIDs, hiddenID := []string{}, ""
	for i, status := range []string{"success", "manual", "pending"} {
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(lib.Path, fmt.Sprintf("movie%d.strm", i)), Status: status, AdditionalParts: []string{"missing-part.strm"}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if status == "pending" {
			hiddenID = strconv.FormatInt(id, 10)
		} else {
			validIDs = append(validIDs, strconv.FormatInt(id, 10), virtualPartID(id, 2))
		}
	}
	for _, season := range []int{0, 1} {
		id, err := a.db.UpsertMovie(store.Movie{LibraryID: lib.ID, SourcePath: filepath.Join(seriesPath, fmt.Sprintf("S%02dE01.strm", season)), Status: "success", Television: &store.Television{Title: "Show", SeriesPath: seriesPath, Season: season, Episode: 1}}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		validIDs = append(validIDs, strconv.FormatInt(id, 10), store.TVSeasonID(lib.ID, seriesPath, season))
	}
	validIDs = append(validIDs, store.TVSeriesID(lib.ID, seriesPath))
	invalidIDs := []string{"999999", "invalid", hiddenID, "tv-series:broken", store.TVSeriesID(lib.ID, filepath.Join(lib.Path, "Missing")), store.TVSeasonID(lib.ID, seriesPath, 99), "part-" + validIDs[0] + "-3"}
	for _, prefix := range []string{"", "/emby"} {
		for _, route := range []string{"/Users/%s/Items/%s/SpecialFeatures", "/users/%s/items/%s/specialfeatures"} {
			for _, id := range validIDs {
				path := prefix + fmt.Sprintf(route, "1", id)
				if w := accountRequest(a, http.MethodGet, path, "", ""); w.Code != 401 {
					t.Errorf("%s: unauthenticated=%d", path, w.Code)
				}
				w := accountRequest(a, http.MethodGet, path+"?EnableImages=false&EnableUserData=false", token, "")
				if w.Code != 200 || w.Body.String() != "[]" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body)
				}
				if w := accountRequest(a, http.MethodGet, path+"?UserId=2", token, ""); w.Code != 404 {
					t.Errorf("%s: cross-user query=%d", path, w.Code)
				}
			}
			for _, id := range invalidIDs {
				path := prefix + fmt.Sprintf(route, "1", id)
				if w := accountRequest(a, http.MethodGet, path, token, ""); w.Code != 404 {
					t.Errorf("%s: invalid item=%d %s", path, w.Code, w.Body)
				}
			}
			if w := accountRequest(a, http.MethodGet, prefix+fmt.Sprintf(route, "2", validIDs[0]), token, ""); w.Code != 404 {
				t.Errorf("cross-user path: %d", w.Code)
			}
		}
	}
	viewer, err := a.db.CreateUser("feature-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	w := accountRequest(a, http.MethodPost, "/Users/AuthenticateByName", "", `{"Username":"feature-viewer","Pw":"password-1234"}`)
	var login struct{ AccessToken string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || login.AccessToken == "" {
		t.Fatalf("viewer login: %d %s", w.Code, w.Body)
	}
	viewerPath := fmt.Sprintf("/emby/Users/%d/Items/%s/SpecialFeatures", viewer.ID, validIDs[0])
	if w := accountRequest(a, http.MethodGet, viewerPath, login.AccessToken, ""); w.Code != 404 {
		t.Errorf("ungranted viewer: %d", w.Code)
	}
	if err := a.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{lib.ID}}); err != nil {
		t.Fatal(err)
	}
	if w := accountRequest(a, http.MethodGet, viewerPath, login.AccessToken, ""); w.Code != 200 || w.Body.String() != "[]" {
		t.Errorf("granted viewer: %d %s", w.Code, w.Body)
	}
	if w := accountRequest(a, http.MethodGet, "/Users/1/Items/"+validIDs[0]+"/SpecialFeatures", login.AccessToken, ""); w.Code != 404 {
		t.Errorf("viewer using administrator path: %d", w.Code)
	}
	if err := a.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range validIDs {
		path := "/emby/Users/1/Items/" + id + "/SpecialFeatures"
		if w := accountRequest(a, http.MethodGet, path, token, ""); w.Code != 404 {
			t.Errorf("%s: denied library=%d", path, w.Code)
		}
	}
	if probes, err := a.db.Probes(1000); err != nil || len(probes) != 0 {
		t.Fatalf("registered special features must not create probes: %v %v", probes, err)
	}
}

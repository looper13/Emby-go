package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type fakeR18Catalog struct {
	fakeR18
	videos  []r18.Video
	queries []r18.CatalogQuery
	counts  int
	err     error
}

func (f *fakeR18Catalog) CatalogCount(context.Context, r18.CatalogQuery) (int64, error) {
	f.counts++
	return int64(len(f.videos)), f.err
}
func (f *fakeR18Catalog) CatalogList(_ context.Context, q r18.CatalogQuery) ([]r18.Video, error) {
	f.queries = append(f.queries, q)
	from := min(q.Start, len(f.videos))
	to := from + min(q.Limit, len(f.videos)-from)
	return f.videos[from:to], f.err
}
func (f *fakeR18Catalog) Get(_ context.Context, id string) (r18.Video, error) {
	f.reads++
	for _, v := range f.videos {
		if v.ID == id {
			return v, f.err
		}
	}
	return r18.Video{}, sql.ErrNoRows
}

func TestR18EmbyBrowseAndAccess(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, err := a.db.CreateR18Library("External", r18.Config{Host: "db.invalid", Database: "catalog", Username: "reader", Password: "private-password"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeR18Catalog{videos: []r18.Video{}}
	for i, service := range []string{"digital", "mono"} {
		fake.videos = append(fake.videos, r18.Video{ID: r18.EncodeID("same001", service), ContentID: "same001", ServiceCode: service, Number: "TEST-001", Title: "Catalog " + strconv.Itoa(i), ReleaseDate: "2024-01-02", CoverURL: "https://private-source.invalid/cover.jpg?secret=upstream", Images: []string{"https://private-source.invalid/first.jpg", "https://private-source.invalid/last.jpg"}})
	}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	get := func(path, auth string, want int) []byte {
		t.Helper()
		resp, raw := embyRaw(t, server, "GET", path, auth, "")
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		for _, secret := range []string{"private-password", "db.invalid", "private-source.invalid", "upstream"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatalf("source credential/URL exposed: %s", raw)
			}
		}
		return raw
	}
	type result struct {
		Items                        []map[string]any
		TotalRecordCount, StartIndex int
	}
	list := func(path, auth string) result {
		t.Helper()
		var out result
		if err := json.Unmarshal(get(path, auth, 200), &out); err != nil || out.Items == nil {
			t.Fatalf("invalid list: %+v %v", out, err)
		}
		return out
	}
	libID := externalLibraryID(l.ID)
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{"/Users/1/Views", "/Library/MediaFolders", "/Library/VirtualFolders/Query", "/Items?IncludeItemTypes=CollectionFolder"} {
			out := list(prefix+path, token)
			found := false
			for _, item := range out.Items {
				if item["Id"] == libID {
					found = true
					if item["CollectionType"] != "movies" {
						t.Fatal("not a movie library", item)
					}
				}
			}
			if !found {
				t.Fatal("R18 library missing", path, out)
			}
		}
		get(prefix+"/Library/VirtualFolders", token, 200)
	}
	var folder map[string]any
	_ = json.Unmarshal(get("/Items/"+libID, token, 200), &folder)
	if folder["Type"] != "CollectionFolder" || folder["ImageTags"] == nil {
		t.Fatal(folder)
	}
	base := "/Items?ParentId=" + libID
	for _, path := range []string{base, "/emby/users/1/items?parentid=" + libID + "&includeitemtypes=Movie&startindex=0&limit=1", "/Users/1/Items?ParentId=" + strconv.FormatInt(l.ID, 10) + "&IncludeItemTypes=Movie&Limit=1"} {
		out := list(path, token)
		if out.TotalRecordCount != 2 || len(out.Items) == 0 {
			t.Fatal("missing count or page", out)
		}
		v := out.Items[0]
		if v["Id"] != r18ItemID(l.ID, fake.videos[0].ID) || v["Type"] != "Movie" || v["ParentId"] != libID || v["IsPlaceHolder"] != true || v["PlayAccess"] != "None" || v["ImageTags"] == nil || v["UserData"] == nil || len(v["MediaSources"].([]any)) != 0 {
			t.Fatal("invalid read-only DTO", v)
		}
		if v["PremiereDate"] != "2024-01-02T00:00:00.0000000Z" || v["ProductionYear"] != float64(2024) {
			t.Fatal("date mapping", v)
		}
		q := fake.queries[len(fake.queries)-1]
		if q.Sort != "PremiereDate" || !q.Descending {
			t.Fatal("default must be latest release first", q)
		}
	}
	noImages := list(base+"&enableimages=false", token)
	if len(noImages.Items[0]["ImageTags"].(map[string]any)) != 0 || len(noImages.Items[0]["BackdropImageTags"].([]any)) != 0 {
		t.Fatal("EnableImages=false ignored", noImages)
	}
	out := list(base+"&StartIndex=1&Limit=1&SortBy=PremiereDate&SortOrder=Descending&Years=2024&SearchTerm=TEST", token)
	q := fake.queries[len(fake.queries)-1]
	if out.StartIndex != 1 || out.Items[0]["Id"] != r18ItemID(l.ID, fake.videos[1].ID) || q.Start != 1 || q.Limit != 1 || q.Sort != "PremiereDate" || !q.Descending || q.Search != "TEST" || len(q.Years) != 1 {
		t.Fatal("offset query lost parameters", out, q)
	}
	if fake.counts != 2 {
		t.Fatal("count not reused across pages", fake.counts)
	}
	if out := list(base+"&StartIndex=9223372036854775807", token); len(out.Items) != 0 || out.TotalRecordCount != 2 {
		t.Fatal("large offset", out)
	}
	for _, query := range []string{"&IncludeItemTypes=Series", "&MediaTypes=Audio", "&IsFavorite=true", "&Filters=IsPlayed", "&Genres=Drama", "&ExcludeItemTypes=Movie", "&isplaceholder=false", "&isfolder=true"} {
		if out := list(base+query, token); len(out.Items) != 0 || out.TotalRecordCount != 0 {
			t.Fatal("unsupported filter returned movies", query, out)
		}
	}
	get(base+"&Years=oops", token, 400)
	get(base+"&SearchTerm="+strings.Repeat("x", 121), token, 400)
	itemID := r18ItemID(l.ID, fake.videos[0].ID)
	for _, path := range []string{"/Items/" + itemID, "/emby/users/1/items/" + itemID} {
		get(path, token, 200)
	}
	get("/Items/r18-999-bad", token, 404)
	get("/Items/"+r18ItemID(l.ID, r18.EncodeID("absent", "mono")), token, 404)
	if out := list("/Items?Ids="+itemID+","+itemID, token); len(out.Items) != 1 {
		t.Fatal("duplicate identity", out)
	}
	if out := list("/Items?Ids="+itemID+"&IsPlaceHolder=false", token); len(out.Items) != 0 {
		t.Fatal("Ids query bypassed placeholder filter", out)
	}
	localLibrary, _ := a.db.Library(1)
	localID, err := a.db.UpsertMovie(store.Movie{LibraryID: localLibrary.ID, SourcePath: filepath.Join(localLibrary.Path, "local.strm"), Title: "Local film", Status: "success", Collection: "Local set"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	mixed := "/Items?Ids=" + strconv.FormatInt(localID, 10) + "," + r18ItemID(l.ID, r18.EncodeID("absent", "mono")) + "," + itemID
	if out := list(mixed+"&StartIndex=1&Limit=1", token); out.TotalRecordCount != 2 || len(out.Items) != 1 || out.Items[0]["Id"] != itemID {
		t.Fatal("mixed IDs must omit missing items before pagination", out)
	}
	if out := list(mixed+"&ParentId="+libID, token); out.TotalRecordCount != 1 || out.Items[0]["Id"] != itemID {
		t.Fatal("mixed IDs escaped library scope", out)
	}
	if out := list("/Items?Ids="+itemID+"&ParentId="+url.QueryEscape(boxsetID("Local set")), token); len(out.Items) != 0 {
		t.Fatal("R18 incorrectly belongs to local collection", out)
	}
	get("/Items/"+itemID+"/Images", token, 200)
	get("/Items/"+itemID+"/ThumbnailSet", token, 200)
	get("/Items/"+itemID+"/Similar", token, 200)
	get("/Items/"+itemID+"/Collections", token, 200)
	get("/Videos/"+itemID+"/AdditionalParts", token, 200)
	get("/Users/1/Items/"+itemID+"/SpecialFeatures", token, 200)
	for _, method := range []string{"GET", "POST"} {
		resp, raw := embyRaw(t, server, method, "/emby/Items/"+itemID+"/PlaybackInfo", token, "{}")
		if resp.StatusCode != 200 || !bytes.Contains(raw, []byte(`"ErrorCode":"NoCompatibleStream"`)) || !bytes.Contains(raw, []byte(`"MediaSources":[]`)) {
			t.Fatal("invented playback", resp.StatusCode, string(raw))
		}
	}
	var latest []map[string]any
	_ = json.Unmarshal(get("/Users/1/Items/Latest?ParentId="+libID+"&Limit=1", token, 200), &latest)
	if q := fake.queries[len(fake.queries)-1]; len(latest) != 1 || !q.Descending || q.Sort != "PremiereDate" {
		t.Fatal("latest shape", latest)
	}
	user, _ := a.db.CreateUser("catalog-reader", "password-1234")
	_ = a.db.SaveUserToken("r18-reader-token", user.ID)
	before := len(fake.queries) + fake.reads + fake.counts
	get("/Users/"+strconv.FormatInt(user.ID, 10)+"/Views", "r18-reader-token", 200)
	get(base, "r18-reader-token", 404)
	get("/Items/"+itemID, "r18-reader-token", 404)
	get("/Items/"+itemID+"/Images/Primary", "r18-reader-token", 404)
	get("/Items/"+itemID+"/PlaybackInfo", "r18-reader-token", 404)
	if len(fake.queries)+fake.reads+fake.counts != before {
		t.Fatal("unauthorized request contacted source")
	}
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	list(base, "r18-reader-token")
	get("/Items/"+itemID, "r18-reader-token", 200)
	get("/Users/1/Items?ParentId="+libID, "r18-reader-token", 404)
	get("/Items?ParentId="+libID+"&UserId=1", "r18-reader-token", 404)
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	get(base, "r18-reader-token", 404)
	get("/Items/"+itemID, "r18-reader-token", 404)
	// Connection failures must not expose the PG DSN to any client.
	fake.err = errors.New("source error private-password")
	get(base+"&SearchTerm=uncached", token, 502)
}

func TestR18EmbyImagesAuthResizeAndCache(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 400 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var source bytes.Buffer
	_ = png.Encode(&source, img)
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		if req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Cookie") != "" {
			t.Error("forwarded credentials")
		}
		_, _ = w.Write(source.Bytes())
	}))
	defer upstream.Close()
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, _ := a.db.CreateR18Library("Images", r18.Config{Host: "db.invalid", Database: "catalog", Username: "reader"})
	v := r18.Video{ID: r18.EncodeID("image001", "mono"), Number: "IMAGE-001", CoverURL: upstream.URL + "/cover", Images: []string{upstream.URL + "/gallery", upstream.URL + "/gallery2"}}
	fake := &fakeR18Catalog{videos: []r18.Video{v}}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	a.r18ImageHTTP = upstream.Client()
	base := "/Items/" + r18ItemID(l.ID, v.ID) + "/Images/"
	call := func(method, path, auth, tag string, want int) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, nil)
		if auth != "" {
			req.Header.Set("X-Emby-Token", auth)
		}
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, data)
		}
		return resp, data
	}
	call("GET", base+"Primary", "", "", 401)
	call("GET", base+"Primary", "invalid-token", "", 401)
	if hits != 0 || fake.reads != 0 {
		t.Fatal("anonymous image accessed source")
	}
	resp, data := call("GET", base+"Primary?maxWidth=100", token, "", 200)
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 150 || resp.Header.Get("Content-Type") != "image/webp" {
		t.Fatal("bad poster dimensions", err)
	}
	r, _, b, _ := decoded.At(50, 75).RGBA()
	if r > b {
		t.Fatal("cropped back cover instead of front")
	}
	tag := resp.Header.Get("ETag")
	call("GET", base+"Primary?maxWidth=100&api_key="+url.QueryEscape(token), "", tag, 304)
	resp, _ = call("GET", "/emby/items/"+r18ItemID(l.ID, v.ID)+"/images/primary?maxWidth=100", token, tag, 304)
	if resp.Header.Get("Cache-Control") != "private, no-cache" || hits != 1 {
		t.Fatal("lost private image cache")
	}
	_, body := call("HEAD", base+"Primary?maxWidth=100", token, "", 200)
	if len(body) != 0 || hits != 1 {
		t.Fatal("HEAD fetched again or returned a body")
	}
	_, data = call("GET", base+"Backdrop/1?maxWidth=100", token, "", 200)
	decoded, _, err = image.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 75 {
		t.Fatal("backdrop should not be cropped", err)
	}
	for _, kind := range []string{"Logo", "Banner", "Primary/1", "Backdrop/2", "Backdrop/-1"} {
		call("GET", base+kind, token, "", 404)
	}
	user, _ := a.db.CreateUser("art-reader", "password-1234")
	_ = a.db.SaveUserToken("r18-art-token", user.ID)
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	call("GET", base+"Primary?maxWidth=100", "r18-art-token", "", 200)
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	call("GET", base+"Primary?maxWidth=100", "r18-art-token", tag, 404)
	if hits != 2 {
		t.Fatal("revoked access bypassed warm cache")
	}
}

func TestR18LiveEmbyBrowse(t *testing.T) {
	host := os.Getenv("EMBY_R18_TEST_HOST")
	if host == "" {
		t.Skip("set PostgreSQL test credentials for read-only Emby acceptance")
	}
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, err := a.db.CreateR18Library("Live R18", r18.Config{Host: host, Port: 5432, Database: os.Getenv("EMBY_R18_TEST_DATABASE"), Username: os.Getenv("EMBY_R18_TEST_USERNAME"), Password: os.Getenv("EMBY_R18_TEST_PASSWORD"), SSLMode: "prefer"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/emby/Users/1/Items?ParentId=" + externalLibraryID(l.ID)
	var items struct {
		Items []struct {
			ID           string `json:"Id"`
			Name         string
			MediaSources []any
			ProviderIds  map[string]string
			PremiereDate string
		}
		TotalRecordCount int
	}
	firstIDs := []string{}
	for _, query := range []string{"&Limit=2", "&Limit=2&StartIndex=2", "&Limit=2&SortBy=DateCreated", "&Limit=2&SortBy=SortName&SortOrder=Ascending", "&Limit=2&SortBy=PremiereDate&SortOrder=Descending", "&SearchTerm=" + url.QueryEscape("DASS-385")} {
		start := time.Now()
		resp, raw := embyRaw(t, server, "GET", base+query, token, "")
		if resp.StatusCode != 200 || json.Unmarshal(raw, &items) != nil || len(items.Items) == 0 || items.TotalRecordCount == 0 {
			t.Fatalf("live browse %s: %d %s", query, resp.StatusCode, raw)
		}
		for _, item := range items.Items {
			if item.ProviderIds["DVD"] == "" || len(item.MediaSources) != 0 {
				t.Fatal("invalid read-only item", item)
			}
		}
		if query == "&Limit=2" {
			firstIDs = []string{items.Items[0].ID, items.Items[1].ID}
			if items.Items[0].PremiereDate == "" || items.Items[0].PremiereDate < items.Items[1].PremiereDate {
				t.Fatal("live default not release-date descending", items.Items)
			}
		}
		t.Logf("live query %s: total=%d, items=%d, duration=%s", query, items.TotalRecordCount, len(items.Items), time.Since(start).Round(time.Millisecond))
	}
	resp, raw := embyRaw(t, server, "GET", "/emby/Users/1/Items/"+items.Items[0].ID, token, "")
	if resp.StatusCode != 200 || !bytes.Contains(raw, []byte(`"IsPlaceHolder":true`)) {
		t.Fatal("live detail", resp.StatusCode, string(raw))
	}
	resp, raw = embyRaw(t, server, "GET", "/emby/Users/1/Items/Latest?ParentId="+externalLibraryID(l.ID)+"&Limit=2", token, "")
	var latest []struct{ ID string }
	if resp.StatusCode != 200 || json.Unmarshal(raw, &latest) != nil || len(latest) != 2 || latest[0].ID != firstIDs[0] || latest[1].ID != firstIDs[1] {
		t.Fatal("live Latest disagrees with release-date default", resp.StatusCode, string(raw))
	}
}

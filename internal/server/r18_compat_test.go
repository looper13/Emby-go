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
	err     error
}

func (f *fakeR18Catalog) CatalogPage(_ context.Context, q r18.CatalogQuery) (r18.CatalogPage, error) {
	f.queries = append(f.queries, q)
	from := min(q.Start, len(f.videos))
	to := from + min(q.Limit, len(f.videos)-from)
	return r18.CatalogPage{Items: f.videos[from:to], HasMore: to < len(f.videos)}, f.err
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
	l, err := a.db.CreateR18Library("External", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "private-api-key"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeR18Catalog{videos: []r18.Video{}}
	for i := 0; i < 2; i++ {
		id := "movie00" + strconv.Itoa(i)
		fake.videos = append(fake.videos, r18.Video{ID: r18.EncodeID(id, "javdb"), ContentID: id, ServiceCode: "javdb", Number: "TEST-001", Title: "Catalog " + strconv.Itoa(i), ReleaseDate: "2024-01-02", Duration: 120, Score: 4.5, CoverURL: "https://private-source.invalid/cover.jpg?secret=upstream", Images: []string{"https://private-source.invalid/first.jpg", "https://private-source.invalid/last.jpg"}})
	}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	get := func(path, auth string, want int) []byte {
		t.Helper()
		resp, raw := embyRaw(t, server, "GET", path, auth, "")
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		for _, secret := range []string{"private-api-key", "avdb.invalid", "private-source.invalid", "upstream"} {
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
		if v["RunTimeTicks"] != float64(120*600000000) || v["CommunityRating"] != 4.5 {
			t.Fatal("online duration/score metadata missing", v)
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
	out := list(base+"&StartIndex=1&Limit=1&SortBy=PremiereDate&SortOrder=Descending&Years=2024", token)
	q := fake.queries[len(fake.queries)-1]
	if out.StartIndex != 1 || out.Items[0]["Id"] != r18ItemID(l.ID, fake.videos[1].ID) || q.Start != 1 || q.Limit != 1 || q.Sort != "PremiereDate" || !q.Descending || q.Search != "" || len(q.Years) != 1 {
		t.Fatal("offset query lost parameters", out, q)
	}
	list(base+"&StartIndex=1&Limit=1&SearchTerm=TEST", token)
	q = fake.queries[len(fake.queries)-1]
	if q.Search != "TEST" || q.Sort != "Relevance" || len(q.Years) != 0 {
		t.Fatal("online keyword query lost its supported sort", q)
	}
	beforeInvalid := len(fake.queries)
	for _, query := range []string{"&StartIndex=9223372036854775807", "&StartIndex=-1", "&StartIndex=oops", "&StartIndex=499950", "&Limit=0", "&Limit=1001", "&Limit=oops", "&SortOrder=invalid"} {
		get(base+query, token, 400)
	}
	if len(fake.queries) != beforeInvalid {
		t.Fatal("invalid offset contacted source")
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
	get("/Items/"+r18ItemID(l.ID, r18.EncodeID("absent", "javdb")), token, 404)
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
	mixed := "/Items?Ids=" + strconv.FormatInt(localID, 10) + "," + r18ItemID(l.ID, r18.EncodeID("absent", "javdb")) + "," + itemID
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
	before := len(fake.queries) + fake.reads
	get("/Users/"+strconv.FormatInt(user.ID, 10)+"/Views", "r18-reader-token", 200)
	get(base, "r18-reader-token", 404)
	get("/Items/"+itemID, "r18-reader-token", 404)
	get("/Items/"+itemID+"/Images/Primary", "r18-reader-token", 404)
	get("/Items/"+itemID+"/PlaybackInfo", "r18-reader-token", 404)
	if len(fake.queries)+fake.reads != before {
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
	// 上游连接失败不能向客户端泄露 API Key。
	fake.err = errors.New("source error private-api-key")
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
		if req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("X-API-Key") != "" {
			t.Error("forwarded credentials")
		}
		_, _ = w.Write(source.Bytes())
	}))
	defer upstream.Close()
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, _ := a.db.CreateR18Library("Images", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	v := r18.Video{ID: r18.EncodeID("image001", "javdb"), Number: "IMAGE-001", CoverURL: upstream.URL + "/cover", Images: []string{upstream.URL + "/gallery", upstream.URL + "/gallery2", upstream.URL + "/gallery3"}}
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
	call("GET", base+"Backdrop/2?maxWidth=100", token, "", 200)
	for _, kind := range []string{"Logo", "Banner", "Primary/1", "Backdrop/3", "Backdrop/100", "Backdrop/-1"} {
		call("GET", base+kind, token, "", 404)
	}
	user, _ := a.db.CreateUser("art-reader", "password-1234")
	_ = a.db.SaveUserToken("r18-art-token", user.ID)
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{l.ID}})
	call("GET", base+"Primary?maxWidth=100", "r18-art-token", "", 200)
	_ = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
	call("GET", base+"Primary?maxWidth=100", "r18-art-token", tag, 404)
	if hits != 3 {
		t.Fatal("revoked access bypassed warm cache")
	}
}

func TestR18EmbyOnlinePaginationHasUnknownCount(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	library, err := a.db.CreateR18Library("Online JavDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeR18Catalog{}
	for i := 0; i < 3; i++ {
		id := "movie" + strconv.Itoa(i)
		fake.videos = append(fake.videos, r18.Video{ID: r18.EncodeID(id, "javdb"), ContentID: id, ServiceCode: "javdb", Number: "TEST-00" + strconv.Itoa(i), Title: "Online " + strconv.Itoa(i), Images: []string{}})
	}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	base := "/Items?ParentId=" + externalLibraryID(library.ID) + "&Limit=2"
	for _, tc := range []struct {
		start, items, lowerBound int
	}{{0, 2, 3}, {2, 1, 3}, {5, 0, 0}} {
		resp, raw := embyRaw(t, server, http.MethodGet, base+"&StartIndex="+strconv.Itoa(tc.start), token, "")
		var result struct {
			Items                       []map[string]any
			TotalRecordCount            int
			TotalRecordCountIsEstimated bool
			TotalRecordCountIsKnown     bool
			AppliedSortBy               string
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &result) != nil || len(result.Items) != tc.items || result.TotalRecordCount != tc.lowerBound || !result.TotalRecordCountIsEstimated || result.TotalRecordCountIsKnown || result.AppliedSortBy != "PremiereDate" {
			t.Fatal("在线分页将下界错误地声明为精确总数", tc, resp.StatusCode, string(raw))
		}
	}
	resp, raw := embyRaw(t, server, http.MethodGet, base+"&SearchTerm=Fixture", token, "")
	var search struct{ AppliedSortBy string }
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &search) != nil || search.AppliedSortBy != "Relevance" {
		t.Fatal("关键词搜索错误地声明为发行日期排序", resp.StatusCode, string(raw))
	}
	for _, tc := range []struct{ requested, applied string }{
		{"release", "PremiereDate"}, {"ProductionYear", "PremiereDate"}, {"DateCreated", "PremiereDate"}, {"score", "CommunityRating"},
	} {
		resp, raw := embyRaw(t, server, http.MethodGet, base+"&SortBy="+tc.requested, token, "")
		var out struct{ AppliedSortBy string }
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || out.AppliedSortBy != tc.applied {
			t.Fatal("排序报告与实际 API 映射不一致", tc, resp.StatusCode, string(raw))
		}
	}
	if len(fake.queries) != 8 {
		t.Fatal("在线分页不应额外请求或缓存不存在的全库计数", len(fake.queries))
	}
}

func TestR18EmbyOffsetPaginationUsesOnlyOnlineAPIPages(t *testing.T) {
	requests := []url.Values{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/v1/javdb/movies/tags" || req.Header.Get("X-API-Key") != "fixture-key" {
			t.Error("unexpected online catalog request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		params := req.URL.Query()
		requests = append(requests, params)
		page, pageErr := strconv.Atoi(params.Get("page"))
		limit, limitErr := strconv.Atoi(params.Get("limit"))
		if pageErr != nil || limitErr != nil || page < 1 || limit != 50 || params.Get("sort_by") != "release" || params.Get("order_by") != "desc" || params.Get("movie_filter_by") != "magnets" {
			t.Error("offset adapter did not use bounded API pages and online defaults")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		start := (page - 1) * limit
		movies := []map[string]any{}
		for i := start; i < min(start+limit, 57); i++ {
			movies = append(movies, map[string]any{"id": "movie" + strconv.Itoa(i), "number": "TEST-" + strconv.Itoa(i), "title": "Online " + strconv.Itoa(i)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"movies": movies, "current_page": page, "has_next": start+len(movies) < 57}})
	}))
	defer upstream.Close()
	a, server, token := newProbeTestApp(t, t.TempDir())
	library, err := a.db.CreateR18Library("Online API", r18.Config{BaseURL: upstream.URL, APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/Items?ParentId=" + externalLibraryID(library.ID) + "&Limit=3"
	for _, tc := range []struct {
		start, items, lowerBound int
	}{{49, 3, 53}, {55, 2, 57}} {
		resp, raw := embyRaw(t, server, http.MethodGet, base+"&StartIndex="+strconv.Itoa(tc.start), token, "")
		var out struct {
			Items []struct {
				ProviderIds map[string]string
			}
			TotalRecordCount            int
			TotalRecordCountIsEstimated bool
			TotalRecordCountIsKnown     bool
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || len(out.Items) != tc.items || out.TotalRecordCount != tc.lowerBound || !out.TotalRecordCountIsEstimated || out.TotalRecordCountIsKnown {
			t.Fatal("online API offset result is incorrect", tc, resp.StatusCode, string(raw))
		}
		for i, item := range out.Items {
			if item.ProviderIds["JavDB"] != "movie"+strconv.Itoa(tc.start+i) {
				t.Fatal("online API offset duplicated or skipped an item", tc, i)
			}
		}
	}
	for _, query := range []string{"&SearchTerm=TEST&SortBy=PremiereDate", "&SearchTerm=TEST&Years=2024", "&NameStartsWith=T"} {
		resp, _ := embyRaw(t, server, http.MethodGet, base+query, token, "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatal("unsupported online API query was not rejected", query, resp.StatusCode)
		}
	}
	if len(requests) != 3 || requests[0].Get("page") != "1" || requests[1].Get("page") != "2" || requests[2].Get("page") != "2" {
		t.Fatal("offset pagination performed unnecessary online requests", len(requests))
	}
}

func TestR18LiveEmbyBrowse(t *testing.T) {
	baseURL, apiKey := os.Getenv("EMBY_AVDB_TEST_URL"), os.Getenv("EMBY_AVDB_TEST_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("设置 EMBY_AVDB_TEST_URL 和 EMBY_AVDB_TEST_KEY 后执行只读 JavDB 验收")
	}
	a, server, token := newProbeTestApp(t, t.TempDir())
	l, err := a.db.CreateR18Library("Live JavDB", r18.Config{BaseURL: baseURL, APIKey: apiKey})
	if err != nil {
		t.Fatal(err)
	}
	// 为真实上游请求设置总预算，不读取或输出服务端凭证。
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 12 * time.Second}
	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if requestErr != nil {
			t.Fatal("创建只读验收请求失败")
		}
		req.Header.Set("X-Emby-Token", token)
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			t.Fatal("只读 JavDB 验收请求失败或超时")
		}
		defer resp.Body.Close()
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if readErr != nil || bytes.Contains(raw, []byte(apiKey)) {
			t.Fatal("只读验收响应读取失败或包含凭证")
		}
		return resp, raw
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
		TotalRecordCount            int
		TotalRecordCountIsEstimated bool
		TotalRecordCountIsKnown     bool
	}
	firstIDs := []string{}
	for _, query := range []string{"&Limit=2", "&Limit=2&StartIndex=2", "&Limit=2&SortBy=PremiereDate&SortOrder=Ascending", "&Limit=2&SearchTerm=" + url.QueryEscape("DASS-385")} {
		start := time.Now()
		resp, raw := get(base + query)
		if resp.StatusCode != 200 || json.Unmarshal(raw, &items) != nil || len(items.Items) == 0 || !items.TotalRecordCountIsEstimated || items.TotalRecordCountIsKnown {
			t.Fatalf("live browse %s: status=%d", query, resp.StatusCode)
		}
		for _, item := range items.Items {
			if item.ProviderIds["DVD"] == "" || len(item.MediaSources) != 0 {
				t.Fatal("真实只读影片缺少番号或出现未经配置的播放源")
			}
			_, videoID, valid := parseR18ItemID(item.ID)
			if !valid {
				t.Fatal("JavDB 影片未使用既有内部播放标识格式")
			}
			if _, service, decodeErr := r18.DecodeID(videoID); decodeErr != nil || service != "javdb" {
				t.Fatal("影片标识没有使用 JavDB 来源")
			}
		}
		if query == "&Limit=2" {
			if len(items.Items) != 2 {
				t.Fatal("真实来源没有返回足够的分页验收影片")
			}
			firstIDs = []string{items.Items[0].ID, items.Items[1].ID}
			if items.Items[0].PremiereDate == "" || items.Items[0].PremiereDate < items.Items[1].PremiereDate {
				t.Fatal("live default not release-date descending", items.Items)
			}
		}
		t.Logf("live query %s: total=%d, items=%d, duration=%s", query, items.TotalRecordCount, len(items.Items), time.Since(start).Round(time.Millisecond))
	}
	resp, raw := get("/emby/Users/1/Items/" + items.Items[0].ID)
	if resp.StatusCode != 200 || !bytes.Contains(raw, []byte(`"IsPlaceHolder":true`)) {
		t.Fatal("live detail status", resp.StatusCode)
	}
	resp, raw = get("/emby/Users/1/Items/Latest?ParentId=" + externalLibraryID(l.ID) + "&Limit=2")
	var latest []struct{ ID string }
	if resp.StatusCode != 200 || json.Unmarshal(raw, &latest) != nil || len(latest) != 2 || latest[0].ID != firstIDs[0] || latest[1].ID != firstIDs[1] {
		t.Fatal("live Latest disagrees with release-date default; status", resp.StatusCode)
	}
}

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type fakeR18 struct {
	reads         int
	video         r18.Video
	checkError    error
	checkDeadline time.Duration
	listQueries   []r18.Query
}

func TestR18ErrorRedactsEncodedAPIKeys(t *testing.T) {
	apiKey := "fixture:/? @secret"
	for _, secret := range []string{apiKey, url.QueryEscape(apiKey), url.PathEscape(apiKey), strings.TrimPrefix(url.UserPassword("", apiKey).String(), ":")} {
		if got := redactR18Error("connection failed: "+secret, apiKey); got != "connection failed: [redacted]" {
			t.Fatal("credential leaked", got)
		}
	}
}

func (f *fakeR18) Check(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		f.checkDeadline = time.Until(deadline)
	}
	return f.checkError
}
func (f *fakeR18) Close() error { return nil }
func (f *fakeR18) List(_ context.Context, q r18.Query) (r18.Page, error) {
	f.reads++
	f.listQueries = append(f.listQueries, q)
	return r18.Page{Items: []r18.Video{f.video}, Page: q.Page, PageSize: q.Limit, HasPrevious: q.Page > 1}, nil
}
func (f *fakeR18) Get(context.Context, string) (r18.Video, error) { f.reads++; return f.video, nil }
func (f *fakeR18) CatalogPage(context.Context, r18.CatalogQuery) (r18.CatalogPage, error) {
	f.reads++
	return r18.CatalogPage{Items: []r18.Video{f.video}}, nil
}
func (f *fakeR18) Resources(context.Context, r18.Video, int) (r18.ResourcePage, error) {
	f.reads++
	return r18.ResourcePage{Items: []r18.Resource{{ID: "1", Title: "Resource", Number: "TEST-001", Magnet: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", Tags: []string{}}}}, nil
}

func (f *fakeR18) Resource(ctx context.Context, v r18.Video, id string) (r18.Resource, error) {
	page, err := f.Resources(ctx, v, 0)
	for _, r := range page.Items {
		if r.ID == id {
			return r, err
		}
	}
	return r18.Resource{}, sql.ErrNoRows
}

func r18TestCall(t *testing.T, baseURL, method, path, auth string, body any, want int) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, baseURL+path, bytes.NewReader(raw))
	req.Header.Set("X-Emby-Token", auth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if resp.StatusCode != 204 {
		_ = json.NewDecoder(resp.Body).Decode(&out)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d (%v)", method, path, resp.StatusCode, out)
	}
	return out
}

func TestR18RoutesReadOnlyCredentialsAndUserAccess(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("test001", "javdb"), ContentID: "test001", ServiceCode: "javdb", Number: "TEST-001", Title: "Catalog item", Images: []string{}}}
	var lastConfig r18.Config
	a.r18Open = func(c r18.Config) (r18Source, error) { lastConfig = c; return fake, nil }
	call := func(method, path, auth string, body any, want int) map[string]any {
		t.Helper()
		return r18TestCall(t, server.URL, method, path, auth, body, want)
	}
	created := call("POST", "/api/admin/r18/libraries", token, map[string]any{"name": "External", "base_url": "https://api.invalid", "api_key": "fixture-secret"}, 201)
	id := int64(created["Id"].(float64))
	sid := strconv.FormatInt(id, 10)
	base := "/api/r18/libraries/" + sid + "/videos"
	call("GET", base, "", nil, 401)
	page := call("GET", base, token, nil, 200)
	if page["page"] != float64(1) || page["page_size"] != float64(24) || page["has_previous"] != false {
		t.Fatal("API page metadata is missing", page)
	}
	for _, key := range []string{"estimated_total", "total_known", "next_cursor", "previous_cursor"} {
		if _, ok := page[key]; ok {
			t.Fatal("API page still exposes obsolete pagination metadata", key)
		}
	}
	if query := fake.listQueries[0]; query.Filter != "magnets" || query.Type != "all" || query.Sort != "release" || query.Order != "desc" || query.Page != 1 || query.Limit != 24 {
		t.Fatal("API browse defaults differ from the online catalog", query)
	}
	call("GET", base+"/"+fake.video.ID, token, nil, 200)
	call("GET", base+"/"+fake.video.ID+"/resources", token, nil, 200)
	before := fake.reads
	call("GET", base+"?page=bad", token, nil, 400)
	call("GET", base+"/bad/resources", token, nil, 400)
	if fake.reads != before {
		t.Fatal("invalid identity queried source")
	}
	cfgPath := "/api/admin/r18/libraries/" + sid
	redacted := call("GET", cfgPath, token, nil, 200)
	connection := redacted["connection"].(map[string]any)
	if strings.Contains(mustJSON(t, redacted), "fixture-secret") || connection["has_api_key"] != true {
		t.Fatal("API key exposed or configuration flag missing")
	}
	call("PUT", cfgPath, token, map[string]any{"name": "Updated", "base_url": "https://api.invalid", "revision": 1}, 200)
	if lastConfig.APIKey != "fixture-secret" {
		t.Fatal("omitted edit discarded API key")
	}
	call("PUT", cfgPath, token, map[string]any{"name": "Updated again", "base_url": "https://api.invalid", "api_key": "  ", "revision": 2}, 200)
	if lastConfig.APIKey != "fixture-secret" {
		t.Fatal("blank edit discarded API key")
	}
	call("PUT", cfgPath, token, map[string]any{"name": "Replaced", "base_url": "https://api.invalid", "api_key": "replacement-fixture", "revision": 3}, 200)
	if lastConfig.APIKey != "replacement-fixture" {
		t.Fatal("API key replacement failed")
	}
	call("PUT", cfgPath, token, map[string]any{"name": "Stale", "base_url": "https://api.invalid", "api_key": "stale-fixture", "revision": 3}, 409)
	stored, err := a.db.R18Connection(id)
	if err != nil || stored.APIKey != "replacement-fixture" {
		t.Fatal("stale configuration overwrote API key", err)
	}
	user, e := a.db.CreateUser("ordinary", "password-1234")
	if e != nil {
		t.Fatal(e)
	}
	if e = a.db.SaveUserToken("ordinary-token", user.ID); e != nil {
		t.Fatal(e)
	}
	before = fake.reads
	call("GET", base, "ordinary-token", nil, 404)
	call("GET", base+"/"+fake.video.ID+"/resources", "ordinary-token", nil, 404)
	call("GET", cfgPath, "ordinary-token", nil, 403)
	if fake.reads != before {
		t.Fatal("unauthorized access contacted API")
	}
	if e = a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{id}}); e != nil {
		t.Fatal(e)
	}
	call("GET", base, "ordinary-token", nil, 200)
	detail := call("GET", base+"/"+fake.video.ID, "ordinary-token", nil, 200)
	if detail["playable"] != false {
		t.Fatal("catalog claims playback")
	}
	call("DELETE", "/api/admin/libraries/"+sid, token, nil, 204)
	call("GET", base, token, nil, 404)
}

func TestR18BrowseQueryValidationAndPageNavigation(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("query001", "javdb"), Images: []string{}}}
	opens := 0
	a.r18Open = func(r18.Config) (r18Source, error) { opens++; return fake, nil }
	lib, err := a.db.CreateR18Library("Online", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/r18/libraries/" + strconv.FormatInt(lib.ID, 10) + "/videos"
	for _, query := range []string{
		"page=0", "page=-1", "page=10000", "page=9223372036854775807", "page=oops", "page=2.5",
		"limit=0", "limit=-1", "limit=51", "limit=oops", "type=digital", "filter=invalid", "sort=SortName",
		"order=invalid", "year=0", "year=10000", "year=oops", "search=" + strings.Repeat("x", 121),
	} {
		r18TestCall(t, server.URL, "GET", base+"?"+query, token, nil, 400)
	}
	if opens != 0 || fake.reads != 0 {
		t.Fatal("invalid query contacted the source", opens, fake.reads)
	}
	out := r18TestCall(t, server.URL, "GET", base+"?page=2&limit=12&type=1&filter=subtitle&sort=score&order=asc&year=2024", token, nil, 200)
	q := fake.listQueries[len(fake.listQueries)-1]
	if q.Page != 2 || q.Limit != 12 || q.Type != "1" || q.Filter != "subtitle" || q.Sort != "score" || q.Order != "asc" || q.Year != "2024" {
		t.Fatal("query parameters were not preserved", q)
	}
	if out["page"] != float64(2) || out["page_size"] != float64(12) || out["has_previous"] != true {
		t.Fatal("page navigation metadata was not preserved", out)
	}
	r18TestCall(t, server.URL, "GET", base+"?search="+url.QueryEscape("  TEST-001  "), token, nil, 200)
	q = fake.listQueries[len(fake.listQueries)-1]
	if q.Search != "TEST-001" || q.Sort != "relevance" || q.Filter != "magnets" || q.Page != 1 {
		t.Fatal("keyword search does not default to relevance", q)
	}
	r18TestCall(t, server.URL, "GET", base+"?search=TEST&sort=release&filter=all", token, nil, 400)
	r18TestCall(t, server.URL, "GET", base+"?search=TEST&sort=relevance&filter=all", token, nil, 200)
	q = fake.listQueries[len(fake.listQueries)-1]
	if q.Sort != "relevance" || q.Filter != "all" {
		t.Fatal("explicit search sorting/filter was overwritten", q)
	}
}

func TestR18UnconfiguredMigratedLibraryCanSaveAPIConfiguration(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	fake := &fakeR18{}
	a.r18Open = func(c r18.Config) (r18Source, error) { return fake, nil }
	lib, err := a.db.CreateR18Library("Migrated", r18.Config{BaseURL: "https://api.invalid", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	// 仅删除临时夹具的 API 行，模拟旧 PostgreSQL 库迁移后尚未配置 API。
	fixtureDB, err := sql.Open("sqlite", a.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixtureDB.Exec("DELETE FROM r18_api_connections WHERE library_id=?", lib.ID)
	fixtureDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/r18/libraries/" + strconv.FormatInt(lib.ID, 10)
	out := r18TestCall(t, server.URL, "GET", path, token, nil, 200)
	connection := out["connection"].(map[string]any)
	if connection["base_url"] != "" || connection["has_api_key"] != false {
		t.Fatal("unconfigured library exposed a legacy connection", connection)
	}
	r18TestCall(t, server.URL, "GET", "/api/r18/libraries/"+strconv.FormatInt(lib.ID, 10)+"/videos", token, nil, 502)
	if fake.reads != 0 {
		t.Fatal("unconfigured library contacted a source")
	}
	r18TestCall(t, server.URL, "PUT", path, token, map[string]any{"name": "JavDB", "base_url": "https://api.invalid", "revision": lib.Revision}, 400)
	r18TestCall(t, server.URL, "PUT", path, token, map[string]any{"name": "JavDB", "base_url": "https://api.invalid", "api_key": "new-fixture-secret", "revision": lib.Revision}, 200)
	stored, err := a.db.R18Connection(lib.ID)
	if err != nil || stored.APIKey != "new-fixture-secret" || stored.BaseURL != "https://api.invalid" {
		t.Fatal("initial API configuration was not saved", err)
	}
}

func TestR18FailedAPICheckDoesNotExposeUpstreamBodyOrKey(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	before, err := a.db.AllLibraries()
	if err != nil {
		t.Fatal(err)
	}
	key := "private-api-fixture"
	fake := &fakeR18{checkError: errors.New("raw-upstream-body-fixture " + key)}
	a.r18Open = func(c r18.Config) (r18Source, error) { return fake, nil }
	var logs bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	out := r18TestCall(t, server.URL, "POST", "/api/admin/r18/libraries", token, map[string]any{"name": "Failure", "base_url": "https://api.invalid", "api_key": key}, 400)
	raw := mustJSON(t, out)
	if strings.Contains(raw, key) || strings.Contains(raw, "raw-upstream-body-fixture") || strings.Contains(logs.String(), key) {
		t.Fatal("API check exposed upstream body or credential")
	}
	if fake.checkDeadline <= 19*time.Second || fake.checkDeadline > 20*time.Second {
		t.Fatal("API check did not use the configured timeout", fake.checkDeadline)
	}
	libs, err := a.db.AllLibraries()
	if err != nil || len(libs) != len(before) {
		t.Fatal("failed API check persisted a library", libs, err)
	}
}

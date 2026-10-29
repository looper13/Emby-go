package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type fakeR18 struct {
	reads int
	video r18.Video
}

func TestR18ErrorRedactsEncodedPasswords(t *testing.T) {
	password := "fixture:/? @secret"
	for _, secret := range []string{password, url.QueryEscape(password), url.PathEscape(password), strings.TrimPrefix(url.UserPassword("", password).String(), ":")} {
		if got := redactR18Error("connection failed: "+secret, password); got != "connection failed: [redacted]" {
			t.Fatal("credential leaked", got)
		}
	}
}

func (f *fakeR18) Check(context.Context) error             { return nil }
func (f *fakeR18) Close() error                            { return nil }
func (f *fakeR18) Estimate(context.Context) (int64, error) { return 1900000, nil }
func (f *fakeR18) List(_ context.Context, q r18.Query) (r18.Page, error) {
	f.reads++
	return r18.Page{Items: []r18.Video{f.video}, NextCursor: f.video.ID, PreviousCursor: f.video.ID}, nil
}
func (f *fakeR18) Get(context.Context, string) (r18.Video, error) { f.reads++; return f.video, nil }
func (f *fakeR18) CatalogCount(context.Context, r18.CatalogQuery) (int64, error) {
	f.reads++
	return 1, nil
}
func (f *fakeR18) CatalogList(context.Context, r18.CatalogQuery) ([]r18.Video, error) {
	f.reads++
	return []r18.Video{f.video}, nil
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

func TestR18RoutesReadOnlyCredentialsAndUserAccess(t *testing.T) {
	a, server, token := newProbeTestApp(t, t.TempDir())
	fake := &fakeR18{video: r18.Video{ID: r18.EncodeID("test001", "digital"), ContentID: "test001", ServiceCode: "digital", Number: "TEST-001", Title: "Catalog item", Images: []string{}}}
	var lastConfig r18.Config
	a.r18Open = func(c r18.Config) (r18Source, error) { lastConfig = c; return fake, nil }
	call := func(method, path, auth string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("X-Emby-Token", auth)
		req.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
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
	created := call("POST", "/api/admin/r18/libraries", token, map[string]any{"name": "External", "host": "db.invalid", "database": "catalog", "username": "reader", "password": "fixture-secret"}, 201)
	id := int64(created["Id"].(float64))
	sid := strconv.FormatInt(id, 10)
	base := "/api/r18/libraries/" + sid + "/videos"
	call("GET", base, "", nil, 401)
	call("GET", base, token, nil, 200)
	call("GET", base+"/"+fake.video.ID, token, nil, 200)
	call("GET", base+"/"+fake.video.ID+"/resources", token, nil, 200)
	before := fake.reads
	call("GET", base+"?cursor=bad", token, nil, 400)
	call("GET", base+"/bad/resources", token, nil, 400)
	if fake.reads != before {
		t.Fatal("invalid identity queried source")
	}
	cfgPath := "/api/admin/r18/libraries/" + sid
	redacted := call("GET", cfgPath, token, nil, 200)
	if strings.Contains(mustJSON(t, redacted), "fixture-secret") {
		t.Fatal("password exposed")
	}
	call("PUT", cfgPath, token, map[string]any{"name": "Updated", "host": "db.invalid", "database": "catalog", "username": "reader", "revision": 1}, 200)
	if lastConfig.Password != "fixture-secret" {
		t.Fatal("blank edit discarded password")
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
		t.Fatal("unauthorized access contacted PostgreSQL")
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

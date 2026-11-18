package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type javdbListFixture struct {
	app    *App
	server *httptest.Server
	token  string
	local  store.Library
	source store.Library
	parent int64
}

func newJavdbListFixture(t *testing.T, movies http.HandlerFunc) javdbListFixture {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.Header.Get("X-API-Key") != "list-fixture-key" || req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Cookie") != "" {
			t.Error("清单读取不是只读或转发了客户端凭据")
		}
		var data any
		switch req.URL.Path {
		case "/api/v1/javdb/search":
			data = map[string]any{"movies": []any{map[string]any{"id": "parent", "number": "TEST-001"}}, "has_next": false}
		case "/api/v1/javdb/lists/related":
			id := "allowed"
			if req.URL.Query().Get("page") == "2" {
				id = "page-two"
			}
			data = map[string]any{"lists": []any{map[string]any{"id": id, "name": "关联清单"}}, "has_next": false}
		default:
			movies(w, req)
			return
		}
		writeJavdbListData(w, data)
	}))
	t.Cleanup(upstream.Close)
	app, server, token := newProbeTestApp(t, t.TempDir())
	local, err := app.db.AddLibrary("当前目录库", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := app.db.CreateR18Library("JavDB", r18.Config{BaseURL: upstream.URL, APIKey: "list-fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := app.db.UpsertMovie(store.Movie{LibraryID: local.ID, SourcePath: filepath.Join(local.Path, "parent.strm"), Status: "success", Number: "TEST-001", Title: "当前本库影片"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return javdbListFixture{app: app, server: server, token: token, local: local, source: source, parent: parent}
}

func writeJavdbListData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func (f javdbListFixture) path(id string) string {
	return "/api/r18/items/" + strconv.FormatInt(f.parent, 10) + "/related-lists/" + id + "/movies?library_id=" + strconv.FormatInt(f.source.ID, 10)
}

func TestR18CommunityRequiresBothLibrariesAndCurrentIndexedIdentity(t *testing.T) {
	var searches, reviews atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" || req.Header.Get("X-API-Key") != "community-fixture-key" || req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Cookie") != "" {
			t.Error("社区请求不是只读或转发了客户端凭据")
		}
		data := map[string]any{}
		switch req.URL.Path {
		case "/api/v1/javdb/search":
			searches.Add(1)
			number := req.URL.Query().Get("q")
			id := "film1"
			if number == "TEST-002" {
				id = "film2"
			}
			data = map[string]any{"movies": []any{map[string]any{"id": id, "number": number, "title": "匹配的影片"}}, "has_next": false}
		case "/api/v1/javdb/movies/film1/reviews", "/api/v1/javdb/movies/film2/reviews":
			reviews.Add(1)
			data = map[string]any{"reviews": []any{map[string]any{"id": "review", "username": "作者", "content": req.URL.Path}}, "has_next": false}
		default:
			t.Error("请求了未批准的上游接口", req.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer upstream.Close()
	root := t.TempDir()
	a, server, token := newProbeTestApp(t, root)
	directory, err := a.db.AddLibrary("目录电影库", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.db.CreateR18Library("JavDB", r18.Config{BaseURL: upstream.URL, APIKey: "community-fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	movie := store.Movie{LibraryID: directory.ID, SourcePath: filepath.Join(directory.Path, "film.strm"), Status: "success", Number: "TEST-001", Title: "本地影片"}
	id, err := a.db.UpsertMovie(movie, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/r18/items/" + strconv.FormatInt(id, 10)
	query := "?library_id=" + strconv.FormatInt(library.ID, 10)
	call := func(path, auth string, status int) map[string]any {
		t.Helper()
		return r18TestCall(t, server.URL, "GET", base+path, auth, nil, status)
	}
	call("/match"+query, "", 401)
	viewer, err := a.db.CreateUser("community-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.SaveUserToken("community-viewer-token", viewer.ID); err != nil {
		t.Fatal(err)
	}
	grant := func(ids ...int64) {
		t.Helper()
		if err := a.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	grant(directory.ID)
	call("/match"+query, "community-viewer-token", 404)
	grant(library.ID)
	call("/match"+query, "community-viewer-token", 404)
	if searches.Load() != 0 {
		t.Fatal("越权请求联系了上游")
	}
	grant(directory.ID, library.ID)
	matched := call("/match"+query+"&number=FAKE-999&movie_id=unrelated", "community-viewer-token", 200)
	if matched["status"] != "matched" || matched["number"] != "TEST-001" {
		t.Fatal("使用了客户端提供的替代番号", matched)
	}
	page := call("/reviews"+query+"&movie_id=unrelated", "community-viewer-token", 200)
	if !strings.Contains(mustJSON(t, page), "film1/reviews") || page["number"] != "TEST-001" || page["video_id"] != r18.EncodeID("film1", "javdb") {
		t.Fatal("评论没有绑定匹配影片", page)
	}
	before := searches.Load() + reviews.Load()
	call("/reviews"+query+"&page=0", token, 400)
	call("/related-lists"+query+"&limit=51", token, 400)
	call("/match?library_id="+strconv.FormatInt(directory.ID, 10), token, 404)
	grant(directory.ID)
	call("/reviews"+query, "community-viewer-token", 404)
	if searches.Load()+reviews.Load() != before {
		t.Fatal("无效或暖缓存撤权请求联系了上游")
	}
	movie.Number = "TEST-002"
	updatedID, err := a.db.UpsertMovie(movie, 1, time.Now())
	if err != nil || updatedID != id {
		t.Fatal("夹具影片更新失败", err)
	}
	page = call("/reviews"+query, token, 200)
	if !strings.Contains(mustJSON(t, page), "film2/reviews") || page["number"] != "TEST-002" || page["video_id"] != r18.EncodeID("film2", "javdb") {
		t.Fatal("入库番号更新后仍返回旧影片评论", page)
	}
}

func TestR18CommunityListsSurviveIndependentReviewFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var data any
		switch req.URL.Path {
		case "/api/v1/javdb/search":
			data = map[string]any{"movies": []any{map[string]any{"id": "film", "number": "TEST-001"}}, "has_next": false}
		case "/api/v1/javdb/movies/film/reviews":
			w.WriteHeader(401)
			_, _ = w.Write([]byte("private-upstream-body"))
			return
		case "/api/v1/javdb/lists/related":
			data = map[string]any{"lists": []any{map[string]any{"id": "public-list", "name": "公开清单", "movies_count": 12}}, "has_next": false}
		default:
			t.Error("意外上游接口", req.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer upstream.Close()
	root := t.TempDir()
	a, server, token := newProbeTestApp(t, root)
	directory, err := a.db.AddLibrary("目录库", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.db.CreateR18Library("JavDB", r18.Config{BaseURL: upstream.URL, APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.db.UpsertMovie(store.Movie{LibraryID: directory.ID, SourcePath: filepath.Join(directory.Path, "film.strm"), Status: "success", Number: "TEST-001"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/r18/items/" + strconv.FormatInt(id, 10)
	query := "?library_id=" + strconv.FormatInt(library.ID, 10)
	failed := r18TestCall(t, server.URL, "GET", base+"/reviews"+query, token, nil, 502)
	if strings.Contains(mustJSON(t, failed), "private-upstream-body") {
		t.Fatal("上游错误正文泄漏")
	}
	lists := r18TestCall(t, server.URL, "GET", base+"/related-lists"+query, token, nil, 200)
	if !strings.Contains(mustJSON(t, lists), "公开清单") || !strings.Contains(mustJSON(t, lists), "public-list") {
		t.Fatal("评论失败影响了清单读取", lists)
	}
}

func TestR18CommunityListMoviesMatchCurrentLibraryAndRecheckWarmIndex(t *testing.T) {
	var calls atomic.Int64
	f := newJavdbListFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		q := req.URL.Query()
		if req.URL.Path != "/api/v1/javdb/movies/tags" || q.Get("list_id") != "allowed" || q.Get("limit") != "50" {
			t.Error("错误的清单影片请求", req.URL)
		}
		rows := []any{map[string]any{"id": "first", "number": "TEST-001", "title": "上游父影片"}, map[string]any{"id": "second", "number": "TEST-002", "title": "上游影片标题"}, map[string]any{"id": "foreign", "number": "OTHER-999"}, map[string]any{"id": "duplicate", "number": "TEST-002"}}
		more := true
		if q.Get("page") == "2" {
			rows = []any{map[string]any{"id": "missing", "number": "MISSING-001"}}
			more = false
		}
		writeJavdbListData(w, map[string]any{"movies": rows, "has_next": more})
	})
	other, err := f.app.db.AddLibrary("其他可访问目录库", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	add := func(library store.Library, name, number, status string, tv *store.Television) int64 {
		t.Helper()
		id, err := f.app.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(library.Path, name+".strm"), Number: number, Title: name, Status: status, Television: tv}, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	owned := add(f.local, "本库标题", "test_002", "manual", nil)
	add(other, "其他库同番号", "TEST-002", "success", nil)
	add(other, "其他库独有番号", "OTHER-999", "success", nil)
	add(other, "另一个外库条目", "MISSING-001", "success", nil)
	add(f.local, "相似版本", "TEST-002-C", "success", nil)
	add(f.local, "不同前导零", "TEST-0002", "success", nil)
	add(f.local, "未就绪影片", "TEST-002", "pending", nil)
	add(f.local, "剧集同号", "TEST-002", "success", &store.Television{SeriesPath: "show", Season: 1, Episode: 1})
	path := f.path("allowed") + "&local_library_id=" + strconv.FormatInt(other.ID, 10) + "&number=OTHER-999"
	result := r18TestCall(t, f.server.URL, "GET", path, f.token, nil, 200)
	items := result["items"].([]any)
	if len(items) != 2 || result["local_library_id"] != float64(f.local.ID) || result["next_page"] != float64(3) || result["has_more"] != false {
		t.Fatal("本库匹配或清单翻页错误", result)
	}
	if items[0].(map[string]any)["id"] != float64(f.parent) || items[1].(map[string]any)["id"] != float64(owned) || items[1].(map[string]any)["title"] != "本库标题" {
		t.Fatal("没有使用本库条目与本库标题", items)
	}
	if strings.Contains(mustJSON(t, result), "source_path") || strings.Contains(mustJSON(t, result), "其他库") || strings.Contains(mustJSON(t, result), "未就绪影片") {
		t.Fatal("匹配结果包含其他范围或私人路径", result)
	}
	if calls.Load() != 2 {
		t.Fatal("上游分页请求数量错误", calls.Load())
	}
	current, err := f.app.db.Movie(owned)
	if err != nil {
		t.Fatal(err)
	}
	current.Number = "TEST-004"
	if _, err := f.app.db.UpsertMovie(current, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	result = r18TestCall(t, f.server.URL, "GET", path, f.token, nil, 200)
	if len(result["items"].([]any)) != 1 || calls.Load() != 2 {
		t.Fatal("暖上游缓存没有重新匹配当前本库索引", result, calls.Load())
	}
	viewer, err := f.app.db.CreateUser("list-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.SaveUserToken("list-viewer-token", viewer.ID); err != nil {
		t.Fatal(err)
	}
	grants := []int64{f.local.ID, other.ID, f.source.ID}
	if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: grants}); err != nil {
		t.Fatal(err)
	}
	result = r18TestCall(t, f.server.URL, "GET", path, "list-viewer-token", nil, 200)
	if len(result["items"].([]any)) != 1 {
		t.Fatal("普通用户获得其他库授权后发生跨库匹配", result)
	}
	if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.local.ID, other.ID}}); err != nil {
		t.Fatal(err)
	}
	r18TestCall(t, f.server.URL, "GET", path, "list-viewer-token", nil, 404)
	r18TestCall(t, f.server.URL, "GET", path, "", nil, 401)
	if calls.Load() != 2 {
		t.Fatal("暖缓存越权查询联系了上游")
	}
}

func TestR18CommunityListMoviesContinueAfterEntirelyUnmatchedPages(t *testing.T) {
	var calls atomic.Int64
	f := newJavdbListFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		number := "UNOWNED-" + strconv.Itoa(page)
		more := true
		if page == 5 {
			number = "TEST-002"
			more = false
		}
		writeJavdbListData(w, map[string]any{"movies": []any{map[string]any{"id": "entry" + strconv.Itoa(page), "number": number}}, "has_next": more})
	})
	owned, err := f.app.db.UpsertMovie(store.Movie{LibraryID: f.local.ID, SourcePath: filepath.Join(f.local.Path, "owned.strm"), Number: "TEST-002", Title: "第五页的本库影片", Status: "success"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	result := r18TestCall(t, f.server.URL, "GET", f.path("allowed"), f.token, nil, 200)
	if len(result["items"].([]any)) != 0 || result["has_more"] != true || result["next_page"] != float64(5) || calls.Load() != 4 {
		t.Fatal("未匹配页被误判为整个清单结束", result, calls.Load())
	}
	result = r18TestCall(t, f.server.URL, "GET", f.path("allowed")+"&page=5", f.token, nil, 200)
	if len(result["items"].([]any)) != 1 || result["items"].([]any)[0].(map[string]any)["id"] != float64(owned) || result["has_more"] != false || calls.Load() != 5 {
		t.Fatal("继续匹配没有找到后续本库影片", result, calls.Load())
	}
}

func TestR18CommunityListMoviesValidateAssociationAndDoNotHideUpstreamFailures(t *testing.T) {
	var calls atomic.Int64
	f := newJavdbListFixture(t, func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("private-upstream-error"))
			return
		}
		writeJavdbListData(w, map[string]any{"movies": []any{}, "has_next": false})
	})
	r18TestCall(t, f.server.URL, "GET", f.path("arbitrary-list"), f.token, nil, 404)
	r18TestCall(t, f.server.URL, "GET", f.path("allowed")+"&page=0", f.token, nil, 400)
	r18TestCall(t, f.server.URL, "GET", f.path("allowed")+"&related_page=0", f.token, nil, 400)
	r18TestCall(t, f.server.URL, "GET", f.path("page-two"), f.token, nil, 404)
	if calls.Load() != 0 {
		t.Fatal("未关联或非法清单查询访问了上游影片")
	}
	failed := r18TestCall(t, f.server.URL, "GET", f.path("page-two")+"&related_page=2", f.token, nil, 502)
	if strings.Contains(mustJSON(t, failed), "private-upstream-error") {
		t.Fatal("上游失败泄露正文")
	}
	result := r18TestCall(t, f.server.URL, "GET", f.path("page-two")+"&related_page=2&refresh=1", f.token, nil, 200)
	if calls.Load() != 2 || len(result["items"].([]any)) != 0 || result["has_more"] != false {
		t.Fatal("清单读取失败被缓存或不能重试", result, calls.Load())
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	writeFile(t, filepath.Join(local.Path, "parent.strm"), "https://media.fixture.invalid/parent.mp4")
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
		rows := []any{map[string]any{"id": "first", "number": "TEST-001", "title": "上游父影片", "cover_url": "https://images.fixture.invalid/first.jpg"}, map[string]any{"id": "second", "number": "TEST-002", "title": "上游影片标题", "cover_url": "https://images.fixture.invalid/second.jpg"}, map[string]any{"id": "foreign", "number": "OTHER-999", "title": "尚未入本库的清单影片"}, map[string]any{"id": "duplicate", "number": "TEST-002"}}
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
		writeFile(t, filepath.Join(library.Path, name+".strm"), "https://media.fixture.invalid/movie.mp4")
		id, err := f.app.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(library.Path, name+".strm"), Number: number, Title: name, Status: status, Television: tv}, 1, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	owned := add(f.local, "本库标题", "test_002", "manual", nil)
	version := add(f.local, "本库第二版本", "TEST-002", "success", nil)
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
	if len(items) != 4 || result["local_library_id"] != float64(f.local.ID) || result["next_page"] != float64(2) || result["has_more"] != true {
		t.Fatal("清单过滤了未匹配影片或翻页错误", result)
	}
	first, second, foreign, duplicate := items[0].(map[string]any), items[1].(map[string]any), items[2].(map[string]any), items[3].(map[string]any)
	if first["id"] != r18.EncodeID("first", "javdb") || second["title"] != "上游影片标题" || foreign["title"] != "尚未入本库的清单影片" || first["has_local"] != true || second["has_local"] != true || foreign["has_local"] != false || duplicate["has_local"] != true {
		t.Fatal("没有保留清单身份、标题或当前库播放角标", items)
	}
	local := second["local_movies"].([]any)
	if len(local) != 2 || local[0].(map[string]any)["id"] != float64(owned) || local[1].(map[string]any)["id"] != float64(version) || local[0].(map[string]any)["title"] != "本库标题" || len(foreign["local_movies"].([]any)) != 0 {
		t.Fatal("没有保留本库播放版本或把其他库当作本地匹配", items)
	}
	wantCover := "/api/r18/libraries/" + strconv.FormatInt(f.source.ID, 10) + "/videos/" + r18.EncodeID("second", "javdb") + "/images/poster"
	cached, known := f.app.cachedR18Video(f.source, r18.EncodeID("second", "javdb"), false)
	if second["cover_url"] != wantCover || !known || cached.CoverURL != "https://images.fixture.invalid/second.jpg" {
		t.Fatal("清单封面没有使用图片代理或丢失源图资料", second)
	}
	if strings.Contains(mustJSON(t, result), "source_path") || strings.Contains(mustJSON(t, result), "其他库") || strings.Contains(mustJSON(t, result), "未就绪影片") || strings.Contains(mustJSON(t, result), "https://images.fixture.invalid") {
		t.Fatal("匹配结果包含其他范围或私人路径", result)
	}
	result = r18TestCall(t, f.server.URL, "GET", path+"&page=2", f.token, nil, 200)
	if len(result["items"].([]any)) != 1 || result["items"].([]any)[0].(map[string]any)["has_local"] != false || result["next_page"] != float64(3) || result["has_more"] != false {
		t.Fatal("清单末页的未匹配影片被过滤", result)
	}
	if calls.Load() != 2 {
		t.Fatal("上游分页请求数量错误", calls.Load())
	}
	for _, id := range []int64{owned, version} {
		current, err := f.app.db.Movie(id)
		if err != nil {
			t.Fatal(err)
		}
		current.Number = "TEST-004"
		if _, err := f.app.db.UpsertMovie(current, 1, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	result = r18TestCall(t, f.server.URL, "GET", path, f.token, nil, 200)
	if len(result["items"].([]any)) != 4 || result["items"].([]any)[1].(map[string]any)["has_local"] != false || len(result["items"].([]any)[1].(map[string]any)["local_movies"].([]any)) != 0 || calls.Load() != 2 {
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
	if len(result["items"].([]any)) != 4 || result["items"].([]any)[1].(map[string]any)["has_local"] != false {
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

func TestR18CommunityListMoviesRetainEntirelyUnmatchedPages(t *testing.T) {
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
	writeFile(t, filepath.Join(f.local.Path, "owned.strm"), "https://media.fixture.invalid/owned.mp4")
	owned, err := f.app.db.UpsertMovie(store.Movie{LibraryID: f.local.ID, SourcePath: filepath.Join(f.local.Path, "owned.strm"), Number: "TEST-002", Title: "第五页的本库影片", Status: "success"}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 5; page++ {
		result := r18TestCall(t, f.server.URL, "GET", f.path("allowed")+"&page="+strconv.Itoa(page), f.token, nil, 200)
		items := result["items"].([]any)
		if len(items) != 1 || result["has_more"] != (page < 5) || result["next_page"] != float64(page+1) || calls.Load() != int64(page) {
			t.Fatal("未匹配影片被跳过或清单分页不正确", result, calls.Load())
		}
		item := items[0].(map[string]any)
		if item["id"] != r18.EncodeID("entry"+strconv.Itoa(page), "javdb") || item["has_local"] != (page == 5) {
			t.Fatal("清单条目身份或本地播放角标错误", item)
		}
		if page == 5 && item["local_movies"].([]any)[0].(map[string]any)["id"] != float64(owned) {
			t.Fatal("后续匹配没有保留本库详情入口", item)
		}
	}
}

func TestR18CommunityListFavoritesUseAuthenticatedAVdbReferences(t *testing.T) {
	f := newJavdbListFixture(t, func(w http.ResponseWriter, req *http.Request) {
		writeJavdbListData(w, map[string]any{"movies": []any{}, "has_next": false})
	})
	base := "/api/r18/items/" + strconv.FormatInt(f.parent, 10) + "/related-lists"
	query := "?library_id=" + strconv.FormatInt(f.source.ID, 10)
	lists := r18TestCall(t, f.server.URL, "GET", base+query+"&page=2", f.token, nil, 200)
	item := lists["items"].([]any)[0].(map[string]any)
	if item["id"] != "page-two" || item["favorite"] != false {
		t.Fatal("关联清单收藏初始状态错误", item)
	}
	path := base + "/page-two/favorite" + query + "&page=2"
	result := r18TestCall(t, f.server.URL, "POST", path, f.token, nil, 200)
	if result["favorite"] != true || result["library_id"] != float64(f.source.ID) || result["list_id"] != "page-two" {
		t.Fatal("收藏响应不匹配", result)
	}
	lists = r18TestCall(t, f.server.URL, "GET", base+query+"&page=2", f.token, nil, 200)
	if lists["items"].([]any)[0].(map[string]any)["favorite"] != true {
		t.Fatal("关联清单状态未更新", lists)
	}
	favorites := r18TestCall(t, f.server.URL, "GET", "/api/r18/list-favorites?search=关联&offset=0&limit=48", f.token, nil, 200)
	items := favorites["items"].([]any)
	if favorites["total"] != float64(1) || len(items) != 1 {
		t.Fatal("AVdb 清单收藏没有出现在用户清单中", favorites)
	}
	saved := items[0].(map[string]any)
	if saved["library_id"] != float64(f.source.ID) || saved["movie_id"] != float64(f.parent) || saved["list_id"] != "page-two" || saved["related_page"] != float64(2) || saved["name"] != "关联清单" {
		t.Fatal("保存的 AVdb 清单上下文错误", saved)
	}
	if strings.Contains(mustJSON(t, favorites), "list-fixture-key") || strings.Contains(mustJSON(t, favorites), f.source.Path) {
		t.Fatal("清单收藏暴露了 AVdb 凭据或连接地址", favorites)
	}
	viewer, err := f.app.db.CreateUser("favorite-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.SaveUserToken("favorite-viewer-token", viewer.ID); err != nil {
		t.Fatal(err)
	}
	other := r18TestCall(t, f.server.URL, "GET", "/api/r18/list-favorites", "favorite-viewer-token", nil, 200)
	if other["total"] != float64(0) || len(other["items"].([]any)) != 0 {
		t.Fatal("AVdb 清单收藏泄露给其他用户", other)
	}
	r18TestCall(t, f.server.URL, "DELETE", "/api/r18/list-favorites/"+strconv.FormatInt(f.source.ID, 10)+"/page-two", "favorite-viewer-token", nil, 204)
	remaining := r18TestCall(t, f.server.URL, "GET", "/api/r18/list-favorites", f.token, nil, 200)
	if remaining["total"] != float64(1) {
		t.Fatal("其他用户取消了此账号的清单收藏", remaining)
	}
	r18TestCall(t, f.server.URL, "POST", base+"/unknown/favorite"+query+"&page=2", f.token, nil, 404)
	r18TestCall(t, f.server.URL, "DELETE", "/api/r18/list-favorites/not-a-library/page-two", f.token, nil, 400)
}

func TestR18CommunityListMoviesPlaybackFlagRechecksActualSTRM(t *testing.T) {
	var calls atomic.Int64
	f := newJavdbListFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		writeJavdbListData(w, map[string]any{"movies": []any{map[string]any{"id": "first", "number": "TEST-001", "title": "有本库索引的影片"}, map[string]any{"id": "missing", "number": "MISSING-001", "title": "未入库影片"}}, "has_next": false})
	})
	assertLocal := func(want bool) {
		t.Helper()
		result := r18TestCall(t, f.server.URL, "GET", f.path("allowed"), f.token, nil, 200)
		items := result["items"].([]any)
		if len(items) != 2 || items[0].(map[string]any)["has_local"] != want || items[1].(map[string]any)["has_local"] != false {
			t.Fatal("可播放角标未核验实际 STRM 或隐藏了未匹配影片", result)
		}
		local := items[0].(map[string]any)["local_movies"].([]any)
		if want && (len(local) != 1 || local[0].(map[string]any)["id"] != float64(f.parent)) || !want && len(local) != 0 {
			t.Fatal("本库详情入口与可播放状态不一致", local)
		}
	}
	assertLocal(true)
	file := filepath.Join(f.local.Path, "parent.strm")
	writeFile(t, file, "not a media URL")
	assertLocal(false)
	writeFile(t, file, "https://media.fixture.invalid/restored.mp4")
	assertLocal(true)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	assertLocal(false)
	if calls.Load() != 1 {
		t.Fatal("本地状态校验没有复用只读上游清单缓存", calls.Load())
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

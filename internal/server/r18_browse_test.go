package server

import (
	"context"
	"encoding/json"
	"errors"
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

type r18BrowseTestSource struct {
	fakeR18
	listDeadline time.Duration
	items        []r18.Video
	total        *int
}

func (f *r18BrowseTestSource) List(ctx context.Context, q r18.Query) (r18.Page, error) {
	if deadline, ok := ctx.Deadline(); ok {
		f.listDeadline = time.Until(deadline)
	}
	f.reads++
	f.listQueries = append(f.listQueries, q)
	return r18.Page{Items: append([]r18.Video{}, f.items...), Page: q.Page, PageSize: q.Limit,
		HasMore: q.Page == 1, HasPrevious: q.Page > 1, Total: f.total}, nil
}

func newR18BrowseTestFixture(t *testing.T) (*r18LocalHTTPFixture, *r18BrowseTestSource) {
	t.Helper()
	f := newR18LocalHTTPFixture(t)
	source := &r18BrowseTestSource{items: []r18.Video{f.fake.video}}
	f.app.r18Open = func(r18.Config) (r18Source, error) { return source, nil }
	return f, source
}

func TestR18BrowseNewModesInvalidQueriesNeverOpenSource(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	opens := 0
	f.app.r18Open = func(r18.Config) (r18Source, error) { opens++; return source, nil }
	for _, query := range []string{
		"mode=invalid", "mode=postgres", "refresh=true", "refresh=2", "refresh=-1",
		"mode=top250&sort=score", "mode=top250&search=TEST", "mode=top250&type=0&year=2024",
		"mode=top250&period=daily", "mode=top250&filter=magnets", "mode=top250&order=asc",
		"mode=top250&page=8&limit=40", "mode=top250&page=6&limit=50", "mode=top250&page=12&limit=24",
		"mode=ranking&period=invalid", "mode=ranking&year=2024", "mode=ranking&tag_ids=0:1",
		"mode=ranking&type=all", "mode=ranking&type=4", "mode=categories&month=0", "mode=categories&month=13",
		"mode=playback&type=0", "mode=popular&type=1", "mode=playback&period=yearly",
		"mode=categories&tag_ids=oops", "mode=categories&tag_ids=0", "mode=categories&tag_ids=5:1",
		"mode=categories&tag_ids=0:1,0:2,0:3,0:4,0:5,0:6", "mode=categories&main_filter=invalid",
		"month=0", "month=13", "month=oops", "duration=45", "ignore_watched=oops", "ignore_watched=true",
	} {
		t.Run(query, func(t *testing.T) {
			r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?"+query, f.token, "", http.StatusBadRequest)
		})
	}
	if opens != 0 || source.reads != 0 {
		t.Fatal("非法浏览条件打开或查询了资料源", opens, source.reads)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseRankingAuthorizationAndRequestBudget(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	viewer, err := f.app.db.CreateUser("browse-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-browse-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"latest", "categories", "top250", "ranking", "playback", "popular"} {
		r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode="+mode, "", "", http.StatusUnauthorized)
		r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode="+mode, token, "", http.StatusNotFound)
	}
	if source.reads != 0 {
		t.Fatal("无权浏览榜单仍查询了资料源", source.reads)
	}
	if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode=top250", token, "", http.StatusOK)
	if source.listDeadline <= 0 || source.listDeadline > 20*time.Second {
		t.Fatal("列表请求未提供最多 20 秒的上游预算", source.listDeadline)
	}
	before := source.reads
	r18LocalSetLibraryEnabled(t, f, f.library.ID, false)
	r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode=top250", token, "", http.StatusNotFound)
	if source.reads != before {
		t.Fatal("停用资料库后仍查询了榜单")
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseLocalFlagsAreFreshAcrossModesUsersAndPermissionChanges(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/movie.mp4")
	r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	viewer, err := f.app.db.CreateUser("browse-local-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-browse-local-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	grant := func(ids ...int64) {
		t.Helper()
		if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	assertLocal := func(mode, auth string, want bool) {
		t.Helper()
		out := r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode="+mode, auth, "", http.StatusOK)
		items := out["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["has_local"] != want {
			t.Fatal("列表本地标记未按当前用户和文件状态重新核验", mode, want, out)
		}
	}
	for _, mode := range []string{"latest", "categories", "top250", "ranking", "playback", "popular"} {
		assertLocal(mode, f.token, true)
		grant(f.library.ID)
		assertLocal(mode, token, false)
		grant(f.library.ID, f.directory.ID)
		assertLocal(mode, token, true)
		grant(f.library.ID)
		assertLocal(mode, token, false)
	}
	// 同一个上游资料对象可复用，但实际 STRM 状态不得从管理员响应传播给其它用户。
	if source.items[0].HasLocal {
		t.Fatal("列表修饰修改了可复用的上游影片对象")
	}
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "not a media URL")
	assertLocal("top250", f.token, false)
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseTagCatalogPermissionsValidationAndDeadline(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	opens := 0
	f.app.r18Open = func(r18.Config) (r18Source, error) { opens++; return source, nil }
	base := "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/tags"
	viewer, err := f.app.db.CreateUser("tag-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-tag-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, http.MethodGet, base, "", "", http.StatusUnauthorized)
	r18LocalHTTPJSON(t, f, http.MethodGet, base+"?type=0", token, "", http.StatusNotFound)
	for _, query := range []string{"type=5", "type=digital", "type=-1", "refresh=true", "refresh=2"} {
		r18LocalHTTPJSON(t, f, http.MethodGet, base+"?"+query, f.token, "", http.StatusBadRequest)
	}
	if opens != 0 || len(source.tagTypes) != 0 {
		t.Fatal("非法标签条件或无权访问仍打开资料源", opens, source.tagTypes)
	}
	if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, movieType := range []string{"all", "0", "1", "2", "3", "4"} {
		query := "?type=" + movieType
		if movieType == "all" {
			query = ""
		}
		out := r18LocalHTTPJSON(t, f, http.MethodGet, base+query, token, "", http.StatusOK)
		if out["categories"] == nil || len(out["categories"].([]any)) != 0 || source.tagTypes[len(source.tagTypes)-1] != movieType {
			t.Fatal("标签类型未透传或空类别不是数组", movieType, out)
		}
	}
	if source.tagDeadline <= 0 || source.tagDeadline > 20*time.Second {
		t.Fatal("标签目录缺少最多 20 秒的预算", source.tagDeadline)
	}
	before := len(source.tagTypes)
	r18LocalSetLibraryEnabled(t, f, f.library.ID, false)
	r18LocalHTTPJSON(t, f, http.MethodGet, base, token, "", http.StatusNotFound)
	if len(source.tagTypes) != before {
		t.Fatal("停用资料库后仍查询标签")
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseModeDefaultsAndCategoryParameters(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	for _, tc := range []struct {
		query, mode, movieType, filter, sort string
	}{
		{"", "latest", "all", "magnets", "release"},
		{"mode=categories", "categories", "all", "magnets", "release"},
		{"mode=top250", "top250", "all", "all", ""},
		{"mode=ranking", "ranking", "0", "all", ""},
		{"mode=playback", "playback", "all", "all", ""},
		{"mode=popular", "popular", "all", "all", ""},
	} {
		out := r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?"+tc.query, f.token, "", http.StatusOK)
		q := source.listQueries[len(source.listQueries)-1]
		if q.Mode != tc.mode || q.Type != tc.movieType || q.Filter != tc.filter || q.Sort != tc.sort || q.Order != "desc" || q.Page != 1 || q.Limit != 24 {
			t.Fatal("不同模式的默认值不正确，榜单不能冒充评分排序", tc.query, q)
		}
		if _, exists := out["total"]; exists {
			t.Fatal("未知总数不应伪造为零", out)
		}
	}
	out := r18LocalHTTPJSON(t, f, http.MethodGet,
		f.base()+"?mode=categories&type=2&filter=subtitle&sort=score&order=asc&year=2024&month=12&duration=45-90&tag_ids=0:23,0:1,0:23&main_filter=p,m,p&page=7&limit=40", f.token, "", http.StatusOK)
	q := source.listQueries[len(source.listQueries)-1]
	if q.Mode != "categories" || q.Type != "2" || q.Filter != "subtitle" || q.Sort != "score" || q.Order != "asc" ||
		q.Year != "2024" || q.Month != "12" || q.Duration != "45-90" || q.TagIDs != "0:1,0:23" || q.MainFilter != "m,p" || q.Page != 7 || q.Limit != 40 {
		t.Fatal("分类条件丢失或重复值未规范化", q)
	}
	if out["page"] != float64(7) || out["page_size"] != float64(40) || out["has_previous"] != true {
		t.Fatal("分类查询未保留分页元数据", out)
	}
	for _, duration := range []string{"lt-45", "45-90", "90-120", "gt-120"} {
		r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode=categories&duration="+duration, f.token, "", http.StatusOK)
	}
	for _, mode := range []string{"ranking", "playback", "popular"} {
		for _, period := range []string{"daily", "weekly", "monthly"} {
			r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode="+mode+"&period="+period, f.token, "", http.StatusOK)
			if q := source.listQueries[len(source.listQueries)-1]; q.Mode != mode || q.Period != period {
				t.Fatal("榜单周期未透传", q)
			}
		}
	}
	for _, value := range []string{"true", "1", "false", "0"} {
		r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode=top250&type=all&year=2024&ignore_watched="+value, f.token, "", http.StatusOK)
		if q := source.listQueries[len(source.listQueries)-1]; q.IgnoreWatched != (value == "true" || value == "1") || q.Year != "2024" {
			t.Fatal("TOP250 忽略已看或年份未透传", value, q)
		}
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseRankingPreservesOrderRanksTotalsAndSafePresentation(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	first, second := source.items[0], source.items[0]
	first.Rank, first.Score = 41, 3
	second.Rank, second.Score = 42, 9
	second.ID, second.ContentID, second.Number = r18.EncodeID("rank002", "javdb"), "rank002", "TEST-002"
	first.CoverURL, second.CoverURL = "https://images.fixture.invalid/first.jpg", "https://images.fixture.invalid/second.jpg"
	source.items = []r18.Video{first, second}
	total := 250
	source.total = &total
	out := r18LocalHTTPJSON(t, f, http.MethodGet, f.base()+"?mode=top250&page=2&limit=40", f.token, "", http.StatusOK)
	items := out["items"].([]any)
	if len(items) != 2 || out["total"] != float64(250) || out["page"] != float64(2) || out["page_size"] != float64(40) || out["has_previous"] != true {
		t.Fatal("榜单总数或分页丢失", out)
	}
	for i, want := range source.items {
		item := items[i].(map[string]any)
		if item["id"] != want.ID || item["rank"] != float64(want.Rank) || item["cover_url"] != f.base()+"/"+want.ID+"/images/poster" {
			t.Fatal("榜单被二次排序、排名丢失或图片未第一方化", i, item)
		}
	}
	if strings.Contains(mustJSON(t, out), "images.fixture.invalid") || strings.Contains(mustJSON(t, out), "fixture-key") {
		t.Fatal("榜单响应泄漏了上游地址或凭据")
	}
	source.items[0].Rank = 0
	source.total = nil
	out = r18LocalHTTPJSON(t, f, http.MethodGet, f.base(), f.token, "", http.StatusOK)
	if _, present := out["items"].([]any)[0].(map[string]any)["rank"]; present {
		t.Fatal("普通浏览不应显示伪造零排名", out)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseTagFailuresDoNotExposeProviderSecrets(t *testing.T) {
	f, source := newR18BrowseTestFixture(t)
	source.tagError = errors.New("provider-body-fixture-key https://avdb.fixture.invalid/private")
	base := "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/tags"
	out := r18LocalHTTPJSON(t, f, http.MethodGet, base, f.token, "", http.StatusBadGateway)
	for _, secret := range []string{"fixture-key", "provider-body", "avdb.fixture.invalid"} {
		if strings.Contains(mustJSON(t, out), secret) {
			t.Fatal("标签查询失败泄漏了上游正文、地址或凭据", out)
		}
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18BrowseRefreshBypassesRawCacheButLocalFlagsStayFresh(t *testing.T) {
	var listHits, tagHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.Header.Get("X-API-Key") != "fixture-key" {
			t.Error("隔离上游收到非法方法或未使用夹具凭据")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/v1/javdb/movies/latest":
			listHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"movies": []map[string]any{{"id": "cached001", "number": "TEST-001", "title": "缓存列表测试"}}, "current_page": 1, "has_next": false,
			}})
		case "/api/v1/javdb/tags":
			tagHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"categories": []any{}}})
		default:
			t.Errorf("普通列表提前读取了详情、磁力或其它上游接口：%s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	f := newR18LocalHTTPFixture(t)
	// 只为本次临时实例安装真实客户端，网络目标固定为本地 httptest 服务。
	_, err := f.app.db.UpdateR18Connection(f.library.ID, f.library.Revision, f.library.Name, r18.Config{BaseURL: upstream.URL, APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	f.app.r18Open = nil
	base := f.base()
	tags := "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/tags"
	call := func(target string, wantLocal bool) {
		t.Helper()
		resp, raw := embyRaw(t, f.server, http.MethodGet, target, f.token, "")
		var out struct {
			Items []r18.Video `json:"items"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || len(out.Items) != 1 || out.Items[0].HasLocal != wantLocal {
			t.Fatalf("原始页缓存阻止了本地文件复查：status=%d wantLocal=%t", resp.StatusCode, wantLocal)
		}
		timing := resp.Header.Get("Server-Timing")
		for _, metric := range []string{"r18_upstream", "r18_local", "r18_total"} {
			if !strings.Contains(timing, metric+";dur=") {
				t.Fatal("列表未给出无隐私的阶段计时", timing)
			}
		}
		if strings.Contains(timing, "fixture") || strings.Contains(timing, upstream.URL) {
			t.Fatal("阶段计时泄漏上游配置", timing)
		}
	}
	call(base, false)
	writeFile(t, filepath.Join(f.directory.Path, "cached.strm"), "https://media.fixture.invalid/cached.mp4")
	r18LocalAddMovie(t, f, f.directory, "cached.strm", "TEST-001", nil)
	call(base, true)
	if listHits.Load() != 1 {
		t.Fatal("相同列表条件未复用原始页缓存", listHits.Load())
	}
	call(base+"?refresh=0", true)
	if listHits.Load() != 1 {
		t.Fatal("refresh=0 不应绕过原始页缓存", listHits.Load())
	}
	call(base+"?refresh=1", true)
	if listHits.Load() != 2 {
		t.Fatal("refresh=1 未重新请求上游列表", listHits.Load())
	}
	writeFile(t, filepath.Join(f.directory.Path, "cached.strm"), "invalid STRM")
	call(base, false)
	if listHits.Load() != 2 {
		t.Fatal("复查本地文件又发起了不必要的上游列表查询", listHits.Load())
	}
	for i, query := range []string{"", "", "?refresh=0", "?refresh=1"} {
		r18LocalHTTPJSON(t, f, http.MethodGet, tags+query, f.token, "", http.StatusOK)
		want := int64(1)
		if i == 3 {
			want = 2
		}
		if tagHits.Load() != want {
			t.Fatal("标签目录缓存或手动刷新行为不正确", query, tagHits.Load(), want)
		}
	}
	r18LocalNoTasksOrCloud(t, f)
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type r18PlayableCatalog struct {
	fakeR18Catalog
	pageError      error
	errorStart     int
	emptyNextPage  bool
	shortNextPage  bool
	queryDeadlines []time.Duration
}

func (f *r18PlayableCatalog) CatalogPage(ctx context.Context, q r18.CatalogQuery) (r18.CatalogPage, error) {
	f.queries = append(f.queries, q)
	if deadline, ok := ctx.Deadline(); ok {
		f.queryDeadlines = append(f.queryDeadlines, time.Until(deadline))
	}
	if err := ctx.Err(); err != nil {
		return r18.CatalogPage{}, err
	}
	if q.Start >= f.errorStart {
		if f.pageError != nil {
			return r18.CatalogPage{}, f.pageError
		}
		if f.emptyNextPage {
			return r18.CatalogPage{Items: []r18.Video{}, HasMore: true}, nil
		}
		if f.shortNextPage {
			return r18.CatalogPage{Items: f.videos[q.Start : q.Start+1], HasMore: true}, nil
		}
	}
	from := min(q.Start, len(f.videos))
	to := from + min(q.Limit, len(f.videos)-from)
	return r18.CatalogPage{Items: f.videos[from:to], HasMore: to < len(f.videos)}, nil
}

type r18PlayableHTTPFixture struct {
	*r18LocalHTTPFixture
	catalog *r18PlayableCatalog
}

func newR18PlayableHTTPFixture(t *testing.T, count int) *r18PlayableHTTPFixture {
	t.Helper()
	f := &r18PlayableHTTPFixture{r18LocalHTTPFixture: newR18LocalHTTPFixture(t), catalog: &r18PlayableCatalog{}}
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("playable%04d", index)
		f.catalog.videos = append(f.catalog.videos, r18.Video{ID: r18.EncodeID(id, "javdb"), ContentID: id,
			ServiceCode: "javdb", Number: fmt.Sprintf("TEST-%04d", index), Title: fmt.Sprintf("影片 %04d", index),
			ReleaseDate: "2024-01-01", Images: []string{}})
	}
	if count > 0 {
		f.fake.video = f.catalog.videos[0]
	}
	f.app.r18Open = func(r18.Config) (r18Source, error) { return f.catalog, nil }
	return f
}

func (f *r18PlayableHTTPFixture) catalogPath() string {
	return "/Items?ParentId=" + externalLibraryID(f.library.ID)
}

func (f *r18PlayableHTTPFixture) videoID(index int) string {
	return r18ItemID(f.library.ID, f.catalog.videos[index].ID)
}

func (f *r18PlayableHTTPFixture) addLocal(t *testing.T, index int, suffix string) int64 {
	t.Helper()
	name := fmt.Sprintf("movie-%04d%s.strm", index, suffix)
	writeFile(t, filepath.Join(f.directory.Path, name), "https://media.fixture.invalid/movie.mp4")
	return r18LocalAddMovie(t, f.r18LocalHTTPFixture, f.directory, name, f.catalog.videos[index].Number, nil)
}

func (f *r18PlayableHTTPFixture) addReady(t *testing.T, index int) store.OfflineTask {
	t.Helper()
	conn, err := f.app.db.Cloud115Connection()
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%040x", index+1)
	task, err := f.app.db.CreateOfflineTask(conn.AccountID, conn.FolderID, "magnet:?xt=urn:btih:"+hash,
		hash, f.library, f.catalog.videos[index].ID, strconv.Itoa(index))
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "waiting_selection"
	task.Files = []cloud115.File{{ID: "file-" + strconv.Itoa(index), Name: "movie.mp4", PickCode: "fixture-code", Size: 100}}
	if err := f.app.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.SelectOfflineFile(f.library, f.catalog.videos[index].ID, task.ID, task.Files[0].ID, true); err != nil {
		t.Fatal(err)
	}
	task, err = f.app.db.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

type r18PlayablePage struct {
	Items                       []map[string]any
	TotalRecordCount            int
	StartIndex                  int
	TotalRecordCountIsEstimated *bool
	TotalRecordCountIsKnown     *bool
}

func r18PlayableGetPage(t *testing.T, f *r18PlayableHTTPFixture, path, token string) r18PlayablePage {
	t.Helper()
	resp, raw := embyRaw(t, f.server, http.MethodGet, path, token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("播放筛选请求失败：%s，状态=%d，响应=%s", path, resp.StatusCode, raw)
	}
	var page r18PlayablePage
	if err := json.Unmarshal(raw, &page); err != nil || page.Items == nil {
		t.Fatalf("播放筛选响应无效：%s，错误=%v", raw, err)
	}
	return page
}

func r18PlayableAssertIDs(t *testing.T, page r18PlayablePage, want ...string) {
	t.Helper()
	got := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		got = append(got, item["Id"].(string))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("筛选分页条目不符：实际=%v，预期=%v", got, want)
	}
}

func r18PlayableAssertUnknownCount(t *testing.T, page r18PlayablePage, total, start int) {
	t.Helper()
	if page.TotalRecordCount != total || page.StartIndex != start || page.TotalRecordCountIsEstimated == nil ||
		!*page.TotalRecordCountIsEstimated || page.TotalRecordCountIsKnown == nil || *page.TotalRecordCountIsKnown {
		t.Fatalf("播放筛选伪造了全库计数或偏移：%+v", page)
	}
}

func TestR18PlayableFilteredPaginationFindsLaterPagesAndPreservesVersions(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 140)
	f.addLocal(t, 55, "")
	f.addLocal(t, 55, "-other")
	f.addLocal(t, 80, "")
	f.addLocal(t, 125, "")
	first := r18PlayableGetPage(t, f, f.catalogPath()+"&IsPlaceHolder=false&Limit=1", f.token)
	r18PlayableAssertIDs(t, first, f.videoID(55))
	r18PlayableAssertUnknownCount(t, first, 2, 0)
	if len(first.Items[0]["MediaSources"].([]any)) != 2 {
		t.Fatal("可播放筛选丢失了本地版本", first)
	}
	second := r18PlayableGetPage(t, f, f.catalogPath()+"&IsPlaceHolder=false&StartIndex=1&Limit=1", f.token)
	r18PlayableAssertIDs(t, second, f.videoID(80))
	r18PlayableAssertUnknownCount(t, second, 3, 1)
	last := r18PlayableGetPage(t, f, f.catalogPath()+"&IsPlaceHolder=false&StartIndex=2&Limit=2", f.token)
	r18PlayableAssertIDs(t, last, f.videoID(125))
	r18PlayableAssertUnknownCount(t, last, 3, 2)
	tail := r18PlayableGetPage(t, f, f.catalogPath()+"&IsPlaceHolder=false&StartIndex=10&Limit=2", f.token)
	r18PlayableAssertIDs(t, tail)
	r18PlayableAssertUnknownCount(t, tail, 3, 10)
	for _, q := range f.catalog.queries {
		if q.Limit != 50 || q.Start%50 != 0 || q.Sort != "PremiereDate" || !q.Descending {
			t.Fatal("播放筛选改变了全局排序或未按连续批次读取", q)
		}
	}
	r18LocalNoTasksOrCloud(t, f.r18LocalHTTPFixture)
}

func TestR18PlayablePlaceholderTrueFiltersBeforePagination(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 90)
	for _, index := range []int{0, 49, 50} {
		f.addLocal(t, index, "")
	}
	path := "/emby/users/1/items?parentid=" + externalLibraryID(f.library.ID) + "&isplaceholder=TrUe&startindex=48&limit=3"
	page := r18PlayableGetPage(t, f, path, f.token)
	r18PlayableAssertIDs(t, page, f.videoID(51), f.videoID(52), f.videoID(53))
	r18PlayableAssertUnknownCount(t, page, 52, 48)
	for _, item := range page.Items {
		if item["IsPlaceHolder"] != true || len(item["MediaSources"].([]any)) != 0 {
			t.Fatal("占位筛选混入了可播放条目", item)
		}
	}
	if len(f.catalog.queries) != 2 || f.catalog.queries[0].Start != 0 || f.catalog.queries[1].Start != 50 {
		t.Fatal("占位筛选未从全局起点补齐跨页结果", f.catalog.queries)
	}
}

func TestR18PlayableLatestUsesFilteredGlobalStart(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 80)
	f.addLocal(t, 55, "")
	f.addLocal(t, 60, "")
	path := "/Users/1/Items/Latest?ParentId=" + externalLibraryID(f.library.ID) + "&IsPlaceHolder=FaLsE&StartIndex=10&Limit=1"
	resp, raw := embyRaw(t, f.server, http.MethodGet, path, f.token, "")
	var items []map[string]any
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &items) != nil || len(items) != 1 || items[0]["Id"] != f.videoID(55) {
		t.Fatalf("最新可播放列表没有从过滤后的起点取值：状态=%d，响应=%s", resp.StatusCode, raw)
	}
	if len(f.catalog.queries) != 2 || f.catalog.queries[0].Start != 0 || f.catalog.queries[0].Sort != "PremiereDate" || !f.catalog.queries[0].Descending {
		t.Fatal("最新列表未保持全局发行日期顺序", f.catalog.queries)
	}
}

func TestR18PlayableMixedIDsFilterBeforeFinitePagination(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 3)
	localID := f.addLocal(t, 1, "")
	local := strconv.FormatInt(localID, 10)
	ids := strings.Join([]string{f.videoID(0), f.videoID(1), local, f.videoID(2), f.videoID(1)}, ",")
	base := "/Items?Ids=" + url.QueryEscape(ids) + "&StartIndex=1&Limit=1"
	page := r18PlayableGetPage(t, f, base+"&IsPlaceHolder=false", f.token)
	r18PlayableAssertIDs(t, page, local)
	if page.TotalRecordCount != 2 || page.StartIndex != 1 {
		t.Fatal("混合 ID 查询未先按真实占位状态过滤并去重", page)
	}
	page = r18PlayableGetPage(t, f, base+"&IsPlaceHolder=true", f.token)
	r18PlayableAssertIDs(t, page, f.videoID(2))
	if page.TotalRecordCount != 2 {
		t.Fatal("有限占位 ID 集合数量错误", page)
	}
	if len(f.catalog.queries) != 0 {
		t.Fatal("显式 ID 查询错误拉取了全局目录", f.catalog.queries)
	}
}

func TestR18PlayableFilteringRevalidatesFilesPermissionsAndReadyBindings(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 4)
	f.addLocal(t, 0, "")
	f.addReady(t, 1)
	expired := f.addReady(t, 2)
	expired.ExpiresAt = time.Now().Add(-time.Second).Unix()
	if err := f.app.db.UpdateOfflineTask(expired, expired.Version); err != nil {
		t.Fatal(err)
	}
	missing := f.addReady(t, 3)
	missing.Files = nil
	if err := f.app.db.UpdateOfflineTask(missing, missing.Version); err != nil {
		t.Fatal(err)
	}
	path := f.catalogPath() + "&IsPlaceHolder=false&Limit=10"
	page := r18PlayableGetPage(t, f, path, f.token)
	r18PlayableAssertIDs(t, page, f.videoID(0), f.videoID(1))
	if err := os.Remove(filepath.Join(f.directory.Path, "movie-0000.strm")); err != nil {
		t.Fatal(err)
	}
	page = r18PlayableGetPage(t, f, path, f.token)
	r18PlayableAssertIDs(t, page, f.videoID(1))
	f.addLocal(t, 0, "")
	user, err := f.app.db.CreateUser("playable-reader", "fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	readerToken := "playable-reader-token"
	if err := f.app.db.SaveUserToken(readerToken, user.ID); err != nil {
		t.Fatal(err)
	}
	reader := f.app.db.ForUser(user.ID)
	if err := reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	page = r18PlayableGetPage(t, f, path, readerToken)
	r18PlayableAssertIDs(t, page, f.videoID(1))
	if err := reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID, f.directory.ID}}); err != nil {
		t.Fatal(err)
	}
	page = r18PlayableGetPage(t, f, path, readerToken)
	r18PlayableAssertIDs(t, page, f.videoID(0), f.videoID(1))
	if f.cloudOpens.Load() != 0 {
		t.Fatal("播放状态筛选打开了真实云盘提供器", f.cloudOpens.Load())
	}
}

func TestR18PlayableFilteringRejectsBudgetExhaustionInsteadOfEmptyPage(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 2001)
	resp, raw := embyRaw(t, f.server, http.MethodGet, f.catalogPath()+"&IsPlaceHolder=false&Limit=1", f.token, "")
	if resp.StatusCode != http.StatusBadGateway || bytes.Contains(raw, []byte(`"Items"`)) || !bytes.Contains(raw, []byte("关键词")) {
		t.Fatalf("扫描预算不足被伪装为正常空页：状态=%d，响应=%s", resp.StatusCode, raw)
	}
	if len(f.catalog.queries) != 40 || f.catalog.queries[39].Start != 1950 {
		t.Fatal("播放筛选越过了 40 批、2000 条的上限", f.catalog.queries)
	}
	for _, deadline := range f.catalog.queryDeadlines {
		if deadline <= 0 || deadline > 20*time.Second {
			t.Fatal("播放筛选未共享最大 20 秒的请求预算", deadline)
		}
	}
}

func TestR18PlayableFilteringBudgetBoundaryRequiresRealLookahead(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		locals  []int
		status  int
		total   int
		firstID int
	}{
		{"最后一批找到下一匹配", 2001, []int{1998, 1999}, http.StatusOK, 2, 1998},
		{"只有满页不能暗示末尾", 2001, []int{1999}, http.StatusBadGateway, 0, 0},
		{"最后一批确认真实末尾", 2000, []int{1999}, http.StatusOK, 1, 1999},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newR18PlayableHTTPFixture(t, test.count)
			for _, index := range test.locals {
				f.addLocal(t, index, "")
			}
			resp, raw := embyRaw(t, f.server, http.MethodGet, f.catalogPath()+"&IsPlaceHolder=false&Limit=1", f.token, "")
			if resp.StatusCode != test.status {
				t.Fatalf("预算边界处理错误：状态=%d，响应=%s", resp.StatusCode, raw)
			}
			if test.status == http.StatusOK {
				var page r18PlayablePage
				if err := json.Unmarshal(raw, &page); err != nil {
					t.Fatal(err)
				}
				r18PlayableAssertIDs(t, page, f.videoID(test.firstID))
				r18PlayableAssertUnknownCount(t, page, test.total, 0)
			} else if bytes.Contains(raw, []byte(`"Items"`)) {
				t.Fatal("预算用尽返回了误导性满页", string(raw))
			}
			if len(f.catalog.queries) != 40 {
				t.Fatal("预算边界未遵守原始条目扫描上限", len(f.catalog.queries))
			}
		})
	}
}

func TestR18PlayablePlaceholderLatestAndMixedIDsRespectAuthorization(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 3)
	localID := f.addLocal(t, 0, "")
	f.addReady(t, 2)
	latest := "/Users/1/Items/Latest?ParentId=" + externalLibraryID(f.library.ID) + "&IsPlaceHolder=true&Limit=1"
	resp, raw := embyRaw(t, f.server, http.MethodGet, latest, f.token, "")
	var items []map[string]any
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &items) != nil || len(items) != 1 || items[0]["Id"] != f.videoID(1) {
		t.Fatalf("最新占位列表混入已就绪来源：状态=%d，响应=%s", resp.StatusCode, raw)
	}
	user, err := f.app.db.CreateUser("filtered-ids-reader", "fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	token := "filtered-ids-reader-token"
	if err := f.app.db.SaveUserToken(token, user.ID); err != nil {
		t.Fatal(err)
	}
	reader := f.app.db.ForUser(user.ID)
	ids := strings.Join([]string{f.videoID(0), strconv.FormatInt(localID, 10), f.videoID(1), f.videoID(2)}, ",")
	base := "/Items?Ids=" + url.QueryEscape(ids) + "&Limit=10"
	if err := reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID}}); err != nil {
		t.Fatal(err)
	}
	page := r18PlayableGetPage(t, f, base+"&IsPlaceHolder=false", token)
	r18PlayableAssertIDs(t, page, f.videoID(2))
	if page.TotalRecordCount != 1 {
		t.Fatal("有限 ID 数量泄漏了无权访问的本地版本", page)
	}
	page = r18PlayableGetPage(t, f, base+"&IsPlaceHolder=true", token)
	r18PlayableAssertIDs(t, page, f.videoID(0), f.videoID(1))
	if err := reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.library.ID, f.directory.ID}}); err != nil {
		t.Fatal(err)
	}
	page = r18PlayableGetPage(t, f, base+"&IsPlaceHolder=false", token)
	r18PlayableAssertIDs(t, page, f.videoID(0), strconv.FormatInt(localID, 10), f.videoID(2))
	if page.TotalRecordCount != 3 {
		t.Fatal("授权后的完整有限 ID 集合计数错误", page)
	}
	if err := reader.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{f.directory.ID}}); err != nil {
		t.Fatal(err)
	}
	page = r18PlayableGetPage(t, f, base+"&IsPlaceHolder=false", token)
	r18PlayableAssertIDs(t, page, strconv.FormatInt(localID, 10))
	if page.TotalRecordCount != 1 || f.cloudOpens.Load() != 0 {
		t.Fatal("撤权后保留了外部条目或访问云盘提供器", page)
	}
}

func TestR18PlayableFilteringRejectsBrokenUpstreamWithoutPartialResult(t *testing.T) {
	for _, kind := range []string{"请求错误", "超时", "不支持的排序", "空下一页", "不完整下一页", "无效条目"} {
		t.Run(kind, func(t *testing.T) {
			f := newR18PlayableHTTPFixture(t, 100)
			f.addLocal(t, 0, "")
			f.catalog.errorStart = 50
			status := http.StatusBadGateway
			switch kind {
			case "请求错误":
				f.catalog.pageError = errors.New("https://avdb.fixture.invalid?key=fixture-key")
			case "超时":
				f.catalog.pageError = context.DeadlineExceeded
			case "不支持的排序":
				f.catalog.pageError = r18.ErrUnsupportedCatalogQuery
				status = http.StatusBadRequest
			case "空下一页":
				f.catalog.emptyNextPage = true
			case "不完整下一页":
				f.catalog.shortNextPage = true
			case "无效条目":
				f.catalog.videos[50].ID = "invalid-id"
			}
			resp, raw := embyRaw(t, f.server, http.MethodGet, f.catalogPath()+"&IsPlaceHolder=false&Limit=2", f.token, "")
			if resp.StatusCode != status || bytes.Contains(raw, []byte(`"Items"`)) || bytes.Contains(raw, []byte("fixture-key")) || bytes.Contains(raw, []byte("avdb.fixture.invalid")) {
				t.Fatalf("上游异常被当作部分正常页或泄漏凭据：状态=%d，响应=%s", resp.StatusCode, raw)
			}
			if kind == "超时" && (!bytes.Contains(raw, []byte("时间")) || !bytes.Contains(raw, []byte("关键词"))) {
				t.Fatalf("时间预算不足没有提示缩小查询范围：%s", raw)
			}
		})
	}
}

func TestR18PlayableFilteringValidatesBooleanAndPreservesUnfilteredOffset(t *testing.T) {
	f := newR18PlayableHTTPFixture(t, 100)
	for _, rawValue := range []string{"", "1", "yes", " false "} {
		for _, path := range []string{f.catalogPath(), "/Items?Ids=" + url.QueryEscape(f.videoID(0))} {
			resp, _ := embyRaw(t, f.server, http.MethodGet, path+"&IsPlaceHolder="+url.QueryEscape(rawValue), f.token, "")
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("无效 IsPlaceHolder 未被拒绝：值=%q，状态=%d", rawValue, resp.StatusCode)
			}
		}
	}
	if len(f.catalog.queries) != 0 || f.catalog.reads != 0 {
		t.Fatal("无效布尔值触发了上游访问", f.catalog)
	}
	page := r18PlayableGetPage(t, f, f.catalogPath()+"&StartIndex=55&Limit=3", f.token)
	r18PlayableAssertIDs(t, page, f.videoID(55), f.videoID(56), f.videoID(57))
	if len(f.catalog.queries) != 1 || f.catalog.queries[0].Start != 55 || f.catalog.queries[0].Limit != 3 {
		t.Fatal("未传占位筛选时改变了原上游分页", f.catalog.queries)
	}
}

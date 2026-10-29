package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type r18LocalTestSource struct {
	*fakeR18
	getError error
}

func (f *r18LocalTestSource) Get(ctx context.Context, id string) (r18.Video, error) {
	if f.getError != nil {
		f.reads++
		return r18.Video{}, f.getError
	}
	return f.fakeR18.Get(ctx, id)
}

type r18LocalHTTPFixture struct {
	app        *App
	server     *httptest.Server
	token      string
	library    store.Library
	directory  store.Library
	fake       *r18LocalTestSource
	cloudOpens atomic.Int64
}

func newR18LocalHTTPFixture(t *testing.T) *r18LocalHTTPFixture {
	t.Helper()
	a, server, token := newProbeTestApp(t, t.TempDir())
	a.offlineService.Close()
	f := &r18LocalHTTPFixture{app: a, server: server, token: token,
		fake: &r18LocalTestSource{fakeR18: &fakeR18{video: r18.Video{ID: r18.EncodeID("local001", "javdb"),
			ContentID: "local001", ServiceCode: "javdb", Number: "TEST-001", Title: "本地播放测试影片", Images: []string{}}}}}
	a.r18Open = func(r18.Config) (r18Source, error) { return f.fake, nil }
	a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider {
		f.cloudOpens.Add(1)
		return &cloudFixture{}
	}
	var err error
	f.directory, err = a.db.AddLibrary("本地目录", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.library, err = a.db.CreateR18Library("JavDB", r18.Config{BaseURL: "https://avdb.fixture.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	// 只在临时数据库保存夹具凭据，证明本地播放不依赖已配置的 115 提供器。
	vault, err := a.cloudVault()
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.Seal(cloud115.Tokens{Cookie: "UID=fixture; CID=fixture; SEID=fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.SaveCloud115Connection(store.Cloud115Connection{AccountID: "fixture-account", FolderID: "fixture-folder", Secrets: secrets}, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *r18LocalHTTPFixture) base() string {
	return "/api/r18/libraries/" + strconv.FormatInt(f.library.ID, 10) + "/videos"
}

func (f *r18LocalHTTPFixture) detail() string { return f.base() + "/" + f.fake.video.ID }

func (f *r18LocalHTTPFixture) item() string { return r18ItemID(f.library.ID, f.fake.video.ID) }

func r18LocalHTTPJSON(t *testing.T, f *r18LocalHTTPFixture, method, path, token, body string, status int) map[string]any {
	t.Helper()
	resp, raw := embyRaw(t, f.server, method, path, token, body)
	if resp.StatusCode != status {
		t.Fatalf("本地播放请求 %s %s：状态=%d，响应=%s", method, path, resp.StatusCode, raw)
	}
	out := map[string]any{}
	if status != http.StatusNoContent && json.Unmarshal(raw, &out) != nil {
		t.Fatalf("本地播放响应不是 JSON：%s", raw)
	}
	return out
}

func r18LocalAddMovie(t *testing.T, f *r18LocalHTTPFixture, library store.Library, name, number string, parts []string) int64 {
	t.Helper()
	id, err := f.app.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(library.Path, name),
		Number: number, Title: "本地影片", Status: "success", RuntimeSeconds: 120, AdditionalParts: parts}, 0, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func r18LocalNoTasksOrCloud(t *testing.T, f *r18LocalHTTPFixture) {
	t.Helper()
	if f.cloudOpens.Load() != 0 {
		t.Fatal("本地播放打开了 115 提供器", f.cloudOpens.Load())
	}
	db, err := sql.Open("sqlite", f.app.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tasks int
	if err := db.QueryRow("SELECT COUNT(*) FROM offline_tasks").Scan(&tasks); err != nil || tasks != 0 {
		t.Fatal("本地播放或本地失效自动创建了离线任务", tasks, err)
	}
}

func r18LocalSetLibraryEnabled(t *testing.T, f *r18LocalHTTPFixture, id int64, enabled bool) {
	t.Helper()
	db, err := sql.Open("sqlite", f.app.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE libraries SET enabled=? WHERE id=?", enabled, id); err != nil {
		t.Fatal(err)
	}
}

func r18LocalStreamRequest(t *testing.T, f *r18LocalHTTPFixture, method, path, token, byteRange string) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(method, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Token", token)
	req.Header.Set("Range", byteRange)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func TestR18LocalHTTPDiscoveryPlaybackAndStandardRange(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	var mediaReads atomic.Int64
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mediaReads.Add(1)
		if req.Header.Get("X-API-Key") != "" || req.Header.Get("X-Emby-Token") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Error("STRM 媒体请求携带了应用或 API 凭据")
		}
		http.ServeContent(w, req, "fixture.mp4", time.Unix(100, 0), bytes.NewReader([]byte("abcdefghij")))
	}))
	t.Cleanup(media.Close)
	rawURL := media.URL + "/movie.mp4?sign=fixture-only"
	sourcePath := filepath.Join(f.directory.Path, "movie.strm")
	writeFile(t, sourcePath, rawURL+"\n")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "test_001", nil)
	itemID := strconv.FormatInt(id, 10)
	sourceID := "strm-" + itemID
	page := r18LocalHTTPJSON(t, f, "GET", f.base(), f.token, "", 200)
	if page["items"].([]any)[0].(map[string]any)["has_local"] != true {
		t.Fatal("列表卡片未标记已有本地 STRM", page)
	}
	detail := r18LocalHTTPJSON(t, f, "GET", f.detail(), f.token, "", 200)
	local := detail["local_sources"].([]any)
	if detail["playable"] != true || detail["video"].(map[string]any)["has_local"] != true || len(local) != 1 || local[0].(map[string]any)["id"] != sourceID {
		t.Fatal("详情未提供可直接播放的本地版本", detail)
	}
	if strings.Contains(mustJSON(t, detail), sourcePath) || strings.Contains(mustJSON(t, detail), rawURL) {
		t.Fatal("R18 详情泄漏了 STRM 路径或内部媒体地址")
	}
	selected := r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id="+sourceID, f.token, "", 200)
	if selected["source"].(map[string]any)["item_id"] != itemID {
		t.Fatal("本地播放未返回标准目录条目标识", selected)
	}
	native := r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", f.token, "", 200)
	sources := native["MediaSources"].([]any)
	source := sources[0].(map[string]any)
	wantStream := streamURL(itemID) + "?MediaSourceId=mediasource_" + itemID + "&Static=true"
	if len(sources) != 1 || source["Id"] != "mediasource_"+itemID || source["ItemId"] != itemID || source["SourceType"] != "strm" || source["Default"] != true || source["DirectStreamUrl"] != wantStream || source["Path"] != wantStream {
		t.Fatal("原生 PlaybackInfo 没有默认使用标准本地媒体源", native)
	}
	item := r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item(), f.token, "", 200)
	if item["IsPlaceHolder"] != false || item["PlayAccess"] != "Full" || len(item["MediaSources"].([]any)) != 1 {
		t.Fatal("原生详情仍把本地影片视为占位条目", item)
	}
	standard := r18LocalHTTPJSON(t, f, "POST", "/Items/"+itemID+"/PlaybackInfo", f.token, "{}", 200)
	if standard["MediaSources"].([]any)[0].(map[string]any)["Id"] != "mediasource_"+itemID || mediaReads.Load() != 0 {
		t.Fatal("标准本地 PlaybackInfo 改走其它来源或提前访问了媒体", standard)
	}
	for _, method := range []string{"GET", "HEAD"} {
		resp, raw := r18LocalStreamRequest(t, f, method, wantStream, f.token, "")
		if resp.StatusCode != 302 || resp.Header.Get("Location") != rawURL || (method == "HEAD" && len(raw) != 0) {
			t.Fatal("标准 STRM 直播放未返回当前媒体地址", resp.StatusCode, resp.Header)
		}
	}
	resp, raw := r18LocalStreamRequest(t, f, "GET", "/Videos/"+itemID+"/proxy", f.token, "bytes=1-3")
	if resp.StatusCode != 206 || string(raw) != "bcd" || resp.Header.Get("Content-Range") != "bytes 1-3/10" {
		t.Fatal("标准代理未保留 Range 播放语义", resp.StatusCode, string(raw), resp.Header)
	}
	if _, err := f.app.db.UpdateLibraryPlaybackSettings(f.directory.ID, true, false, f.directory.Revision); err != nil {
		t.Fatal(err)
	}
	resp, raw = r18LocalStreamRequest(t, f, "GET", wantStream, f.token, "bytes=5-7")
	if resp.StatusCode != 206 || string(raw) != "fgh" {
		t.Fatal("目录库代理开关没有作用于本地 STRM 播放", resp.StatusCode, string(raw))
	}
	resp, raw = r18LocalStreamRequest(t, f, "HEAD", wantStream, f.token, "bytes=0-1")
	if resp.StatusCode != 206 || len(raw) != 0 || resp.Header.Get("Content-Range") != "bytes 0-1/10" {
		t.Fatal("本地代理 HEAD 的 Range 或响应体错误", resp.StatusCode, string(raw), resp.Header)
	}
	r18LocalHTTPJSON(t, f, "POST", "/Sessions/Playing/Progress", f.token, fmt.Sprintf(`{"ItemId":%q,"PositionTicks":12300000}`, itemID), 204)
	data, err := f.app.db.Data(id)
	if err != nil || data.PositionTicks != 12300000 {
		t.Fatal("本地播放未沿用目录影片的进度", data, err)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalHTTPDoubleLibraryPermissionsAndRevocation(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/movie.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	itemID, sourceID := strconv.FormatInt(id, 10), "strm-"+strconv.FormatInt(id, 10)
	viewer, err := f.app.db.CreateUser("local-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-local-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	grant := func(ids ...int64) {
		t.Helper()
		if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	selection := f.detail() + "/local-playback?source_id=" + sourceID
	before := f.fake.reads
	r18LocalHTTPJSON(t, f, "GET", f.detail(), token, "", 404)
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 404)
	if f.fake.reads != before {
		t.Fatal("未授权 R18 库仍查询了资料源")
	}
	grant(f.library.ID)
	assertNoLocal := func() {
		t.Helper()
		out := r18LocalHTTPJSON(t, f, "GET", f.detail(), token, "", 200)
		if out["video"].(map[string]any)["has_local"] != false || len(out["local_sources"].([]any)) != 0 || out["playable"] != false {
			t.Fatal("R18 库授权绕过了本地库授权", out)
		}
		r18LocalHTTPJSON(t, f, "GET", selection, token, "", 404)
		out = r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", token, "", 200)
		if len(out["MediaSources"].([]any)) != 0 || out["ErrorCode"] != "NoCompatibleStream" {
			t.Fatal("原生播放泄漏未授权本地源", out)
		}
		resp, _ := r18LocalStreamRequest(t, f, "HEAD", streamURL(itemID), token, "")
		if resp.StatusCode != 404 {
			t.Fatal("标准目录流忽略本地库撤权", resp.StatusCode)
		}
	}
	assertNoLocal()
	grant(f.directory.ID)
	before = f.fake.reads
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 404)
	if f.fake.reads != before {
		t.Fatal("仅授权本地目录即可访问 R18 条目")
	}
	grant(f.library.ID, f.directory.ID)
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 200)
	out := r18LocalHTTPJSON(t, f, "GET", f.detail(), token, "", 200)
	if out["video"].(map[string]any)["has_local"] != true {
		t.Fatal("双库授权后不能发现本地源", out)
	}
	grant(f.library.ID)
	assertNoLocal()
	grant(f.library.ID, f.directory.ID)
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 200)
	r18LocalSetLibraryEnabled(t, f, f.directory.ID, false)
	assertNoLocal()
	r18LocalSetLibraryEnabled(t, f, f.directory.ID, true)
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 200)
	r18LocalSetLibraryEnabled(t, f, f.library.ID, false)
	before = f.fake.reads
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 404)
	r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", token, "", 404)
	if f.fake.reads != before {
		t.Fatal("停用 R18 库后仍读取了上游影片")
	}
	r18LocalSetLibraryEnabled(t, f, f.library.ID, true)
	grant(f.directory.ID)
	r18LocalHTTPJSON(t, f, "GET", selection, token, "", 404)
	r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", token, "", 404)
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalHTTPSelectionCacheStillRevalidatesIndexAndFiles(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	file := filepath.Join(f.directory.Path, "movie.strm")
	writeFile(t, file, "https://media.fixture.invalid/movie.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	path := f.detail() + "/local-playback?source_id=strm-" + strconv.FormatInt(id, 10)
	r18LocalHTTPJSON(t, f, "GET", f.detail(), f.token, "", 200)
	before := f.fake.reads
	f.fake.getError = errors.New("隔离夹具上游暂不可用")
	r18LocalHTTPJSON(t, f, "GET", path, f.token, "", 200)
	if f.fake.reads != before {
		t.Fatal("详情已加载后本地播放仍等待上游资料")
	}
	for _, source := range []string{"strm-0", "strm--1", "strm-01", "strm-9223372036854775808", "strm-1-p1", "strm-1-p02", "strm-1-p9223372036854775808", "mediasource_1", "strm-1-extra"} {
		r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id="+url.QueryEscape(source), f.token, "", 400)
	}
	r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id=strm-999999", f.token, "", 404)
	writeFile(t, filepath.Join(f.directory.Path, "other.strm"), "https://media.fixture.invalid/other.mp4")
	other := r18LocalAddMovie(t, f, f.directory, "other.strm", "OTHER-001", nil)
	r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id=strm-"+strconv.FormatInt(other, 10), f.token, "", 404)
	movie, err := f.app.db.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	movie.Number = "CHANGED-001"
	if _, err := f.app.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, "GET", path, f.token, "", 404)
	movie.Number = "TEST-001"
	if _, err := f.app.db.UpsertMovie(movie, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, "GET", path, f.token, "", 404)
	if f.fake.reads != before {
		t.Fatal("缓存身份的本地复查访问了上游")
	}
	writeFile(t, file, "https://media.fixture.invalid/movie.mp4")
	if _, err := f.app.db.UpdateR18Connection(f.library.ID, f.library.Revision, "更换资料源", r18.Config{BaseURL: "https://new-avdb.fixture.invalid", APIKey: "new-fixture-key"}); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, "GET", path, f.token, "", 502)
	if f.fake.reads != before+1 {
		t.Fatal("更换资料源后仍复用了旧 revision 的影片身份缓存")
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalHTTPMultipleVersionsAndVirtualParts(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "z.strm"), "https://media.fixture.invalid/z.mp4")
	z := r18LocalAddMovie(t, f, f.directory, "z.strm", "TEST001", nil)
	main := filepath.Join(f.directory.Path, "a.strm")
	part := filepath.Join(f.directory.Path, "a-CD2.strm")
	badPart := filepath.Join(f.directory.Path, "a-CD3.strm")
	writeFile(t, main, "https://media.fixture.invalid/a.mp4")
	writeFile(t, part, "https://media.fixture.invalid/a-part.mp4")
	writeFile(t, badPart, "file:///not-a-media-url")
	a := r18LocalAddMovie(t, f, f.directory, "a.strm", "TEST-001", []string{part, badPart})
	secondLibrary, err := f.app.db.AddLibrary("第二目录", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secondFile := filepath.Join(secondLibrary.Path, "copy.strm")
	writeFile(t, secondFile, "https://media.fixture.invalid/second.mp4")
	second := r18LocalAddMovie(t, f, secondLibrary, "copy.strm", "TEST_001", nil)
	aID := strconv.FormatInt(a, 10)
	items := []string{aID, "part-" + aID + "-2", strconv.FormatInt(z, 10), strconv.FormatInt(second, 10)}
	wantIDs := []string{"strm-" + aID, "strm-" + aID + "-p2", "strm-" + strconv.FormatInt(z, 10), "strm-" + strconv.FormatInt(second, 10)}
	for n := 0; n < 2; n++ {
		out := r18LocalHTTPJSON(t, f, "GET", f.detail(), f.token, "", 200)
		got := []string{}
		for _, value := range out["local_sources"].([]any) {
			got = append(got, value.(map[string]any)["id"].(string))
		}
		if !reflect.DeepEqual(got, wantIDs) {
			t.Fatal("多版本未稳定排序或不可用分段被列入", got, wantIDs)
		}
	}
	for i, sourceID := range wantIDs {
		out := r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id="+sourceID, f.token, "", 200)
		if out["source"].(map[string]any)["item_id"] != items[i] {
			t.Fatal("选择本地版本改变了条目或分段标识", out)
		}
		out = r18LocalHTTPJSON(t, f, "POST", "/Items/"+f.item()+"/PlaybackInfo", f.token, fmt.Sprintf(`{"MediaSourceId":%q}`, "mediasource_"+items[i]), 200)
		sources := out["MediaSources"].([]any)
		if len(sources) != 1 || sources[0].(map[string]any)["ItemId"] != items[i] {
			t.Fatal("原生客户端未选中指定本地版本", out)
		}
		r18LocalHTTPJSON(t, f, "GET", "/Items/"+items[i]+"/PlaybackInfo", f.token, "", 200)
		resp, _ := r18LocalStreamRequest(t, f, "HEAD", streamURL(items[i]), f.token, "")
		if resp.StatusCode != 302 {
			t.Fatal("标准本地或分段流不可播放", items[i], resp.StatusCode)
		}
	}
	for _, requested := range []string{"mediasource_999999", "mediasource_part-" + aID + "-3", "mediasource_0", "strm-" + aID} {
		for _, method := range []string{"GET", "POST"} {
			path, body := "/Items/"+f.item()+"/PlaybackInfo", ""
			if method == "GET" {
				path += "?MediaSourceId=" + url.QueryEscape(requested)
			} else {
				body = fmt.Sprintf(`{"MediaSourceId":%q}`, requested)
			}
			out := r18LocalHTTPJSON(t, f, method, path, f.token, body, 200)
			if len(out["MediaSources"].([]any)) != 0 || out["ErrorCode"] != "NoCompatibleStream" {
				t.Fatal("原生客户端的假来源或不可用分段错误回退到默认版本", requested, out)
			}
		}
	}
	r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id=strm-"+aID+"-p3", f.token, "", 404)
	if err := os.Remove(main); err != nil {
		t.Fatal(err)
	}
	r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id=strm-"+aID, f.token, "", 404)
	out := r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", f.token, "", 200)
	if source := out["MediaSources"].([]any)[0].(map[string]any); source["ItemId"] != items[1] || source["Default"] != true {
		t.Fatal("主文件失效错误屏蔽了有效分段", out)
	}
	for _, file := range []string{part, filepath.Join(f.directory.Path, "z.strm"), secondFile} {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	out = r18LocalHTTPJSON(t, f, "GET", f.detail(), f.token, "", 200)
	if out["video"].(map[string]any)["has_local"] != false || out["playable"] != false {
		t.Fatal("全部本地版本失效后仍宣称可播放", out)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalHTTPRejectsUnsafeOrUnavailableSTRM(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	for index, tc := range []struct {
		name  string
		write func(*testing.T, string) string
	}{
		{"missing", func(_ *testing.T, file string) string { return file }},
		{"oversize", func(t *testing.T, file string) string {
			writeFile(t, file, "https://media.fixture.invalid/movie.mp4\n"+strings.Repeat("x", 64<<10))
			return file
		}},
		{"directory", func(t *testing.T, file string) string {
			if err := os.Mkdir(file, 0755); err != nil {
				t.Fatal(err)
			}
			return file
		}},
		{"invalid-url", func(t *testing.T, file string) string { writeFile(t, file, "javascript:alert(1)"); return file }},
		{"url-credentials", func(t *testing.T, file string) string {
			writeFile(t, file, "https://user:secret@media.fixture.invalid/movie.mp4")
			return file
		}},
		{"outside-root", func(t *testing.T, _ string) string {
			file := filepath.Join(t.TempDir(), "outside.strm")
			writeFile(t, file, "https://media.fixture.invalid/movie.mp4")
			return file
		}},
		{"symlink-outside", func(t *testing.T, file string) string {
			target := filepath.Join(t.TempDir(), "target.strm")
			writeFile(t, target, "https://media.fixture.invalid/movie.mp4")
			if err := os.Symlink(target, file); err != nil {
				t.Skipf("环境不能创建符号链接：%v", err)
			}
			return file
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.fake.video.ID = r18.EncodeID(fmt.Sprintf("unsafe%03d", index), "javdb")
			f.fake.video.Number = fmt.Sprintf("UNSAFE-%03d", index)
			file := tc.write(t, filepath.Join(f.directory.Path, tc.name+".strm"))
			id, err := f.app.db.UpsertMovie(store.Movie{LibraryID: f.directory.ID, SourcePath: file, Number: f.fake.video.Number, Status: "success", Title: "不安全文件夹具"}, 0, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			out := r18LocalHTTPJSON(t, f, "GET", f.detail(), f.token, "", 200)
			if out["video"].(map[string]any)["has_local"] != false || len(out["local_sources"].([]any)) != 0 || out["playable"] != false {
				t.Fatal("不安全或不可用 STRM 被列为可播放", out)
			}
			r18LocalHTTPJSON(t, f, "GET", f.detail()+"/local-playback?source_id=strm-"+strconv.FormatInt(id, 10), f.token, "", 404)
			out = r18LocalHTTPJSON(t, f, "GET", "/Items/"+f.item()+"/PlaybackInfo", f.token, "", 200)
			if len(out["MediaSources"].([]any)) != 0 || out["ErrorCode"] != "NoCompatibleStream" {
				t.Fatal("原生播放暴露不安全 STRM", out)
			}
		})
	}
	r18LocalNoTasksOrCloud(t, f)
}

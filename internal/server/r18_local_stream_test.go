package server

import (
	"database/sql"
	"errors"
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

	"emby-go/internal/cloud115"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

func r18LocalVirtualRequest(t *testing.T, f *r18LocalHTTPFixture, method, endpoint string, headers http.Header) (*http.Response, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(method, f.server.URL+endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = headers.Clone()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func r18LocalVirtualURL(f *r18LocalHTTPFixture, source string) string {
	return streamURL(f.item()) + "?MediaSourceId=" + url.QueryEscape(source)
}

func r18LocalVirtualDB(t *testing.T, f *r18LocalHTTPFixture) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", f.app.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestR18LocalVirtualStreamSelectionAliasesAndNo115(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	if _, err := r18LocalVirtualDB(t, f).Exec("DELETE FROM cloud115_connection"); err != nil {
		t.Fatal(err)
	}
	main, part := filepath.Join(f.directory.Path, "a.strm"), filepath.Join(f.directory.Path, "a-CD2.strm")
	writeFile(t, main, "https://media.fixture.invalid/a.mp4")
	writeFile(t, part, "https://media.fixture.invalid/a-part.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "a.strm", "TEST-001", []string{part})
	writeFile(t, filepath.Join(f.directory.Path, "z.strm"), "https://media.fixture.invalid/z.mp4")
	other := r18LocalAddMovie(t, f, f.directory, "z.strm", "TEST001", nil)
	mainID, partID := strconv.FormatInt(id, 10), "part-"+strconv.FormatInt(id, 10)+"-2"
	cases := []struct {
		method, path, key, item, location string
	}{
		{"GET", "/Videos/" + f.item() + "/stream", "MediaSourceId", mainID, "https://media.fixture.invalid/a.mp4"},
		{"HEAD", "/videos/" + f.item() + "/stream.mp4", "mediasourceid", partID, "https://media.fixture.invalid/a-part.mp4"},
		{"GET", "/emby/Videos/" + f.item() + "/stream.mkv", "MediaSourceID", strconv.FormatInt(other, 10), "https://media.fixture.invalid/z.mp4"},
		{"HEAD", "/emby/videos/" + f.item() + "/stream", "MEDIASOURCEID", mainID, "https://media.fixture.invalid/a.mp4"},
	}
	for _, test := range cases {
		t.Run(test.method+test.key, func(t *testing.T) {
			endpoint := test.path + "?" + test.key + "=" + url.QueryEscape("mediasource_"+test.item)
			resp, body := r18LocalVirtualRequest(t, f, test.method, endpoint, http.Header{"X-Emby-Token": {f.token}})
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != test.location || test.method == "HEAD" && len(body) != 0 {
				t.Fatalf("虚拟条目没有播放所选本地版本：状态=%d，地址=%q，响应=%s", resp.StatusCode, resp.Header.Get("Location"), body)
			}
		})
	}
	before := f.fake.reads
	f.fake.getError = errors.New("fixture upstream unavailable")
	resp, body := r18LocalVirtualRequest(t, f, "GET", r18LocalVirtualURL(f, "mediasource_"+partID), http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusFound || f.fake.reads != before {
		t.Fatalf("暖身份缓存未复用或无法播放当前有效文件：状态=%d，响应=%s", resp.StatusCode, body)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamAuthenticationAndLegacyAnonymous(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/movie.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	source := "mediasource_" + strconv.FormatInt(id, 10)
	endpoint := r18LocalVirtualURL(f, source)
	key, err := f.app.db.CreateAPIKey("local-client")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		headers    http.Header
		status     int
	}{
		{"anonymous", endpoint, nil, http.StatusUnauthorized},
		{"invalid-token", endpoint, http.Header{"X-Emby-Token": {"invalid-fixture-token"}}, http.StatusUnauthorized},
		{"cookie", endpoint, http.Header{"Cookie": {"emby_session=" + f.token}}, http.StatusFound},
		{"api-key", endpoint + "&api_key=" + key.Key, nil, http.StatusFound},
		{"api-key-header", endpoint, http.Header{"X-Emby-Token": {key.Key}}, http.StatusFound},
		{"local-ticket", f.app.cloudStreamURL(f.library, f.fake.video.ID, source), nil, http.StatusUnauthorized},
		{"115-ticket", strings.Replace(f.app.cloudStreamURL(f.library, f.fake.video.ID, "115-fixture-task"), "MediaSourceId=115-fixture-task", "MediaSourceId="+source, 1), nil, http.StatusUnauthorized},
		{"legacy-anonymous", streamURL(strconv.FormatInt(id, 10)), nil, http.StatusFound},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			resp, body := r18LocalVirtualRequest(t, f, "GET", test.path, test.headers)
			if resp.StatusCode != test.status {
				t.Fatalf("本地虚拟流鉴权错误：状态=%d，期望=%d，响应=%s", resp.StatusCode, test.status, body)
			}
		})
	}
	if err := f.app.db.DeleteAPIKey(key.Key); err != nil {
		t.Fatal(err)
	}
	resp, _ := r18LocalVirtualRequest(t, f, "HEAD", endpoint+"&api_key="+key.Key, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("删除 API Key 后仍可访问虚拟本地流", resp.StatusCode)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamPermissionsRevocationAndDisabledUser(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/movie.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	endpoint := r18LocalVirtualURL(f, "mediasource_"+strconv.FormatInt(id, 10))
	viewer, err := f.app.db.CreateUser("virtual-local-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-virtual-local-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	grant := func(ids ...int64) {
		t.Helper()
		if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
	request := func(status int) {
		t.Helper()
		resp, body := r18LocalVirtualRequest(t, f, "GET", endpoint, http.Header{"X-Emby-Token": {token}})
		if resp.StatusCode != status {
			t.Fatalf("双库授权检查错误：状态=%d，期望=%d，响应=%s", resp.StatusCode, status, body)
		}
	}
	grant(f.directory.ID)
	before := f.fake.reads
	request(http.StatusNotFound)
	if f.fake.reads != before {
		t.Fatal("没有 R18 授权仍查询了资料源")
	}
	grant(f.library.ID)
	request(http.StatusNotFound)
	grant(f.library.ID, f.directory.ID)
	request(http.StatusFound)
	grant(f.library.ID)
	request(http.StatusNotFound)
	grant(f.library.ID, f.directory.ID)
	r18LocalSetLibraryEnabled(t, f, f.directory.ID, false)
	request(http.StatusNotFound)
	r18LocalSetLibraryEnabled(t, f, f.directory.ID, true)
	r18LocalSetLibraryEnabled(t, f, f.library.ID, false)
	request(http.StatusNotFound)
	r18LocalSetLibraryEnabled(t, f, f.library.ID, true)
	request(http.StatusFound)
	if err := f.app.db.DeleteAccessToken(token); err != nil {
		t.Fatal(err)
	}
	request(http.StatusUnauthorized)
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.app.db.EditUser(viewer.ID, viewer.Name, "", true); err != nil {
		t.Fatal(err)
	}
	request(http.StatusUnauthorized)
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamRejectsFakeStaleAndUnrelatedSources(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	main, part := filepath.Join(f.directory.Path, "a.strm"), filepath.Join(f.directory.Path, "a-CD2.strm")
	writeFile(t, main, "https://media.fixture.invalid/a.mp4")
	writeFile(t, part, "https://media.fixture.invalid/part.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "a.strm", "TEST-001", []string{part})
	writeFile(t, filepath.Join(f.directory.Path, "unrelated.strm"), "https://media.fixture.invalid/unrelated.mp4")
	unrelated := r18LocalAddMovie(t, f, f.directory, "unrelated.strm", "OTHER-999", nil)
	idText := strconv.FormatInt(id, 10)
	for _, source := range []string{"mediasource_999999", "mediasource_0" + idText, "mediasource_part-0" + idText + "-2", "mediasource_part-" + idText + "-02", "mediasource_part-" + idText + "-3", "mediasource_" + strconv.FormatInt(unrelated, 10), "mediasource_", "mediasource_" + strings.Repeat("1", 150)} {
		resp, body := r18LocalVirtualRequest(t, f, "GET", r18LocalVirtualURL(f, source), http.Header{"X-Emby-Token": {f.token}})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("假来源或其它影片来源没有被拒绝：%q，状态=%d，响应=%s", source, resp.StatusCode, body)
		}
	}
	endpoint := r18LocalVirtualURL(f, "mediasource_"+idText)
	resp, _ := r18LocalVirtualRequest(t, f, "GET", endpoint+"&mediasourceid=mediasource_999999", http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatal("冲突的大小写来源参数没有被拒绝", resp.StatusCode)
	}
	resp, _ = r18LocalVirtualRequest(t, f, "GET", endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusFound {
		t.Fatal("有效本地源不能播放", resp.StatusCode)
	}
	if _, err := r18LocalVirtualDB(t, f).Exec("UPDATE movies SET number=? WHERE id=?", "OTHER-999", id); err != nil {
		t.Fatal(err)
	}
	resp, _ = r18LocalVirtualRequest(t, f, "HEAD", endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal("暖缓存错误保留了已换番号的本地源", resp.StatusCode)
	}
	if _, err := r18LocalVirtualDB(t, f).Exec("UPDATE movies SET number=? WHERE id=?", "TEST-001", id); err != nil {
		t.Fatal(err)
	}
	writeFile(t, main, "file:///invalid-source")
	resp, _ = r18LocalVirtualRequest(t, f, "GET", endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal("失效主文件错误回退其它分段或 115", resp.StatusCode)
	}
	writeFile(t, main, "https://media.fixture.invalid/a.mp4")
	if err := os.Remove(main); err != nil {
		t.Fatal(err)
	}
	resp, _ = r18LocalVirtualRequest(t, f, "HEAD", endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal("删除主文件后错误回退其它分段或 115", resp.StatusCode)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamProxyRangeAndHLS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:1,\nsegment.ts\n#EXT-X-ENDLIST\n")
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "movie.mp4", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer upstream.Close()
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), upstream.URL+"/movie.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	if _, err := f.app.db.UpdateLibraryPlaybackSettings(f.directory.ID, true, false, f.directory.Revision); err != nil {
		t.Fatal(err)
	}
	endpoint := r18LocalVirtualURL(f, "mediasource_"+strconv.FormatInt(id, 10))
	for _, method := range []string{"GET", "HEAD"} {
		resp, body := r18LocalVirtualRequest(t, f, method, endpoint, http.Header{"X-Emby-Token": {f.token}, "Range": {"bytes=2-5"}})
		if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != "bytes 2-5/10" || method == "GET" && string(body) != "2345" || method == "HEAD" && len(body) != 0 {
			t.Fatalf("虚拟本地流未保留代理 Range：状态=%d，响应=%s，头=%v", resp.StatusCode, body, resp.Header)
		}
	}
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), upstream.URL+"/master.m3u8")
	resp, body := r18LocalVirtualRequest(t, f, "GET", "/emby"+endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), upstream.URL) || !strings.Contains(string(body), "/emby/Videos/"+strconv.FormatInt(id, 10)+"/hls-proxy/") {
		t.Fatalf("HLS 清单没有成功改写：状态=%d，响应=%s", resp.StatusCode, body)
	}
	resource := hlsProxyURLInBody(t, string(body))
	if !strings.Contains(resource, "/Videos/"+strconv.FormatInt(id, 10)+"/hls-proxy/") || strings.Contains(resource, f.item()) {
		t.Fatal("HLS 会话错误绑定原虚拟条目而非真实本地条目", resource)
	}
	resp, body = r18LocalVirtualRequest(t, f, "GET", resource, http.Header{"Range": {"bytes=3-6"}})
	if resp.StatusCode != http.StatusPartialContent || string(body) != "3456" || resp.Header.Get("Content-Range") != "bytes 3-6/10" {
		t.Fatalf("转派后的 HLS 分片不可播放：状态=%d，响应=%s，头=%v", resp.StatusCode, body, resp.Header)
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamRestoresParamsWithCachedQuery(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	part := filepath.Join(f.directory.Path, "movie-CD2.strm")
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/main.mp4")
	writeFile(t, part, "https://media.fixture.invalid/part.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", []string{part})
	endpoint := streamURL(f.item()) + "?mediasourceid=" + url.QueryEscape("mediasource_part-"+strconv.FormatInt(id, 10)+"-2")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", endpoint, nil)
	c.Request.Header.Set("X-Emby-Token", f.token)
	c.Params = gin.Params{{Key: "id", Value: f.item()}}
	originalParams, originalQuery := c.Params, c.Request.URL.RawQuery
	_ = c.Query("MediaSourceId")
	if !f.app.authOK(c) {
		t.Fatal("夹具登录令牌不可用")
	}
	f.app.forRequest(c).stream(c)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://media.fixture.invalid/part.mp4" {
		t.Fatalf("Gin 已缓存查询时不能转派所选分段：状态=%d，响应=%s", recorder.Code, recorder.Body.String())
	}
	if c.Param("id") != f.item() || originalParams[0].Value != f.item() || c.Request.URL.RawQuery != originalQuery {
		t.Fatal("转派污染了路由参数或原始查询")
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18LocalVirtualStreamKeepsExplicit115AndDefaultTicket(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/local.mp4")
	r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	task, err := f.app.db.CreateOfflineTask("fixture-account", "fixture-folder", "magnet:?xt=urn:btih:"+strings.Repeat("a", 40), strings.Repeat("a", 40), f.library, f.fake.video.ID, "fixture-resource")
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.Files = "ready", []cloud115.File{{ID: "file", Name: "movie.mkv", PickCode: "fixture-code", SHA1: "file-sha", Size: 100}}
	if err := f.app.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{r18LocalVirtualURL(f, "115-"+task.ID), streamURL(f.item())} {
		resp, body := r18LocalVirtualRequest(t, f, "GET", endpoint, http.Header{"X-Emby-Token": {f.token}})
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://cdn.example/movie?sign=private" {
			t.Fatalf("既有显式或默认 115 行为改变：状态=%d，响应=%s", resp.StatusCode, body)
		}
	}
	ticket := f.app.cloudStreamURL(f.library, f.fake.video.ID, "115-"+task.ID)
	resp, body := r18LocalVirtualRequest(t, f, "HEAD", ticket, nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://cdn.example/movie?sign=private" {
		t.Fatalf("既有 115 票据失效：状态=%d，响应=%s", resp.StatusCode, body)
	}
	if f.cloudOpens.Load() != 1 {
		t.Fatal("既有 115 提供器未正常复用", f.cloudOpens.Load())
	}
	var tasks int
	if err := r18LocalVirtualDB(t, f).QueryRow("SELECT COUNT(*) FROM offline_tasks").Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("播放改变了既有离线任务数量：%d，%v", tasks, err)
	}
}

func TestR18LocalVirtualStreamRejectsMalformedSourceBeforeDefault115(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/local.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	task, err := f.app.db.CreateOfflineTask("fixture-account", "fixture-folder", "magnet:?xt=urn:btih:"+strings.Repeat("b", 40), strings.Repeat("b", 40), f.library, f.fake.video.ID, "fixture-resource")
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.Files = "ready", []cloud115.File{{ID: "file", Name: "movie.mkv", PickCode: "fixture-code", SHA1: "file-sha", Size: 100}}
	if err := f.app.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	source := "mediasource_" + strconv.FormatInt(id, 10)
	for _, malformed := range []string{" " + source, source + " ", "MEDIASOURCE_" + strconv.FormatInt(id, 10)} {
		for _, key := range []string{"MediaSourceId", "mediasourceid"} {
			t.Run(key+malformed, func(t *testing.T) {
				endpoint := streamURL(f.item()) + "?" + key + "=" + url.QueryEscape(malformed)
				resp, body := r18LocalVirtualRequest(t, f, "GET", endpoint, http.Header{"X-Emby-Token": {f.token}})
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("非规范本地来源错误回退默认 115：状态=%d，响应=%s", resp.StatusCode, body)
				}
			})
		}
	}
	if f.cloudOpens.Load() != 0 {
		t.Fatal("非规范本地来源访问了 115 提供器", f.cloudOpens.Load())
	}
}

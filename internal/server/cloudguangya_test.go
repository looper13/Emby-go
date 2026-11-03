package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type namedCloudFixture struct {
	cloudFixture
	account, provider string
	checkErr          error
	onCheck           func() error
	cacheInfoErr      error
	cacheInfo         *cloud115.File
	listErr           error
}

func (f *namedCloudFixture) Check(context.Context, string) (string, error) {
	if f.onCheck != nil {
		if err := f.onCheck(); err != nil {
			return "", err
		}
	}
	return f.account, f.checkErr
}

func (f *namedCloudFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	if id == "12" {
		f.infos++
		if f.cacheInfoErr != nil {
			return cloud115.File{}, f.cacheInfoErr
		}
		if f.cacheInfo != nil {
			return *f.cacheInfo, nil
		}
		return cloud115.File{ID: "12", ParentID: "0", Name: cloud115.CacheFolderName, Directory: true}, nil
	}
	return f.cloudFixture.Info(ctx, id)
}

func (f *namedCloudFixture) List(ctx context.Context, parent string, offset int) ([]cloud115.File, bool, error) {
	if f.listErr != nil {
		return nil, false, f.listErr
	}
	return f.cloudFixture.List(ctx, parent, offset)
}

func (f *namedCloudFixture) URL(ctx context.Context, code, ua string) (string, error) {
	_, err := f.cloudFixture.URL(ctx, code, ua)
	return "https://" + f.provider + ".example/movie?sign=private", err
}

type guangyaServerHarness struct {
	a               *App
	server          *httptest.Server
	client          *http.Client
	admin, ua       string
	legacy, guangya *namedCloudFixture
	received        cloudguangya.Tokens
	library         store.Library
	video           r18.Video
}

func newGuangyaServerHarness(t *testing.T) *guangyaServerHarness {
	t.Helper()
	a, server, admin := newProbeTestApp(t, t.TempDir())
	a.offlineService.Close()
	a.guangyaOffline.Close()
	h := &guangyaServerHarness{
		a: a, server: server, admin: admin, ua: "Guangya-Test-Player",
		client:  &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		legacy:  &namedCloudFixture{account: "shared-account", provider: "115"},
		guangya: &namedCloudFixture{account: "shared-account", provider: "guangya"},
		video:   r18.Video{ID: r18.EncodeID("test", "digital"), Number: "TEST-001", Title: "测试影片", Images: []string{}},
	}
	fake := &fakeR18{video: h.video}
	a.r18Open = func(r18.Config) (r18Source, error) { return fake, nil }
	a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider { return h.legacy }
	a.guangyaOpen = func(tokens cloudguangya.Tokens, _ func(cloudguangya.Tokens) error) cloud115.Provider {
		h.received = tokens
		return h.guangya
	}
	var err error
	h.library, err = a.db.CreateR18Library("JAVDB", r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *guangyaServerHarness) call(t *testing.T, method, path, token string, body any, want int) (http.Header, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Token", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", h.ua)
	req.Header.Set("Range", "bytes=100-200")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s 返回 %d，预期 %d：%s", method, path, response.StatusCode, want, raw)
	}
	for _, secret := range []string{"guangya-secret-access", "guangya-secret-refresh", "115-token-secret", "fresh-code", "private-code"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("网盘接口响应泄露凭据或文件内部标识")
		}
	}
	if method == http.MethodHead && len(raw) != 0 {
		t.Fatal("HEAD 响应包含正文")
	}
	out := map[string]any{}
	if len(raw) != 0 && response.StatusCode != http.StatusFound {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal("接口未返回有效 JSON", err)
		}
	}
	return response.Header, out
}

func cloudResponseList(t *testing.T, out map[string]any, key string) []any {
	t.Helper()
	list, ok := out[key].([]any)
	if !ok {
		t.Fatalf("响应缺少列表 %s", key)
	}
	return list
}

func cloudResponseSource(t *testing.T, out map[string]any, key, id string) map[string]any {
	t.Helper()
	for _, raw := range cloudResponseList(t, out, key) {
		source, ok := raw.(map[string]any)
		if ok && source["Id"] == id {
			return source
		}
	}
	t.Fatalf("响应缺少来源 %s", id)
	return nil
}

func (h *guangyaServerHarness) configure(t *testing.T) {
	t.Helper()
	h.call(t, http.MethodPut, "/api/admin/115/connection", h.admin, map[string]any{"cookie": "UID=115-token-secret; CID=fixture-cid; SEID=fixture-seid", "revision": 0}, http.StatusNoContent)
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"access_token": "guangya-secret-access", "refresh_token": "guangya-secret-refresh", "device_id": strings.Repeat("d", 32), "revision": 0}, http.StatusNoContent)
}

func (h *guangyaServerHarness) base(provider string) string {
	base := "/api/r18/libraries/" + strconv.FormatInt(h.library.ID, 10) + "/videos/" + h.video.ID + "/offline"
	if provider == "guangya" {
		base += "/guangya"
	}
	return base
}

func (h *guangyaServerHarness) submit(t *testing.T, provider string) store.OfflineTask {
	t.Helper()
	_, out := h.call(t, http.MethodPost, h.base(provider), h.admin, map[string]any{"resource_id": "1"}, http.StatusOK)
	tasks := cloudResponseList(t, out, "tasks")
	if len(tasks) != 1 {
		t.Fatal("首次提交未返回唯一任务", len(tasks))
	}
	id, _ := tasks[0].(map[string]any)["id"].(string)
	task, err := h.a.db.OfflineTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (h *guangyaServerHarness) ready(t *testing.T, provider string) store.OfflineTask {
	t.Helper()
	task := h.submit(t, provider)
	task.Status, task.ExpiresAt = "waiting_selection", time.Now().Add(time.Hour).Unix()
	task.Files = []cloud115.File{{ID: provider + "-file", Name: provider + "-movie.mkv", SHA1: "file-sha", PickCode: "private-code", Size: 100, Duration: 60}}
	if err := h.a.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	h.call(t, http.MethodPost, h.base(provider)+"/selection", h.admin, map[string]any{"task_id": task.ID, "file_id": provider + "-file", "default": true}, http.StatusOK)
	task, err := h.a.db.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (h *guangyaServerHarness) reader(t *testing.T, name string) (store.User, string) {
	t.Helper()
	user, err := h.a.db.CreateUser(name, "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := name + "-token"
	if err := h.a.db.SaveUserToken(token, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{h.library.ID}}); err != nil {
		t.Fatal(err)
	}
	return user, token
}

func TestCloudGuangyaConfigurationEncryptionRevisionAndAccountIsolation(t *testing.T) {
	h := newGuangyaServerHarness(t)
	h.call(t, http.MethodGet, "/api/admin/guangya/connection", "", nil, http.StatusUnauthorized)
	_, initial := h.call(t, http.MethodGet, "/api/admin/guangya/connection", h.admin, nil, http.StatusOK)
	if initial["configured"] != false || initial["revision"] != float64(0) {
		t.Fatal("未配置状态不正确", initial)
	}
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 0}, http.StatusBadRequest)
	h.configure(t)
	connection, err := h.a.db.CloudGuangyaConnection()
	if err != nil || connection.AccountID != "shared-account" || connection.Revision != 1 {
		t.Fatal("光鸭配置保存失败", connection.Revision, err)
	}
	for _, value := range []string{"guangya-secret-access", "guangya-secret-refresh", strings.Repeat("d", 32)} {
		if bytes.Contains(connection.Secrets, []byte(value)) {
			t.Fatal("光鸭数据库保存了明文凭据")
		}
	}
	vault, err := h.a.guangyaSecretsVault()
	if err != nil {
		t.Fatal(err)
	}
	var tokens cloudguangya.Tokens
	if err := vault.OpenValue(connection.Secrets, &tokens); err != nil || tokens != h.received {
		t.Fatal("加密凭据不能恢复", err)
	}
	legacyVault, err := h.a.cloudVault()
	if err != nil {
		t.Fatal(err)
	}
	if err := legacyVault.OpenValue(connection.Secrets, &tokens); err == nil {
		t.Fatal("115 密钥域能够解密光鸭凭据")
	}
	_, public := h.call(t, http.MethodGet, "/api/admin/guangya/connection", h.admin, nil, http.StatusOK)
	for _, field := range []string{"access_token", "refresh_token", "device_id", "secrets"} {
		if _, found := public[field]; found {
			t.Fatal("光鸭配置 GET 回显私有字段", field)
		}
	}
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 0}, http.StatusConflict)
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 1}, http.StatusNoContent)
	if h.received.Access != "guangya-secret-access" || h.received.Refresh != "guangya-secret-refresh" || h.received.DeviceID != strings.Repeat("d", 32) {
		t.Fatal("留空保存丢失了原有凭据")
	}
	legacy, err := h.a.db.Cloud115Connection()
	if err != nil || legacy.Revision != 1 {
		t.Fatal("光鸭修订影响了 115 配置", err)
	}
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"device_id": "invalid", "revision": 2}, http.StatusBadRequest)
	h.guangya.account = "another-account"
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 2}, http.StatusConflict)
	h.guangya.account = "shared-account"
	h.guangya.checkErr = errors.New("guangya-secret-access https://private.invalid/token")
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 2}, http.StatusBadRequest)
	connection, err = h.a.db.CloudGuangyaConnection()
	if err != nil || connection.AccountID != "shared-account" || connection.Revision != 2 {
		t.Fatal("拒绝更新后改变了既有光鸭配置", err)
	}
}

func TestCloudGuangyaConfigurationErrorStagesRedactAllCandidateTokens(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, scenario := range []struct {
			name, stage string
			fail        func(*namedCloudFixture, error)
		}{
			{"账号验证", "光鸭账号验证失败：", func(f *namedCloudFixture, err error) { f.checkErr = err }},
			{"缓存目录发现", "光鸭缓存目录创建或验证失败：", func(f *namedCloudFixture, err error) { f.listErr = err }},
			{"缓存目录信息", "光鸭缓存目录信息读取失败：", func(f *namedCloudFixture, err error) { f.cacheInfoErr = err }},
		} {
			state := "首次配置"
			if existing {
				state = "更新配置"
			}
			t.Run(state+"/"+scenario.name, func(t *testing.T) {
				h := newGuangyaServerHarness(t)
				var before store.CloudGuangyaConnection
				if existing {
					h.configure(t)
					var err error
					before, err = h.a.db.CloudGuangyaConnection()
					if err != nil {
						t.Fatal(err)
					}
				}
				access, refresh := "original-access-fixture", "original-refresh-fixture"
				first := cloudguangya.Tokens{Access: access + "-first-private-suffix", Refresh: refresh + "-first-private-suffix", DeviceID: strings.Repeat("b", 32)}
				last := cloudguangya.Tokens{Access: "latest-refreshed-access", Refresh: "latest-refreshed-refresh", DeviceID: strings.Repeat("c", 32)}
				cause := errors.New("光鸭 HTTP 请求失败（401），具体响应原因保留：" + strings.Join([]string{
					"Bearer " + access, access, refresh, first.Access, first.Refresh, last.Access, last.Refresh,
					first.DeviceID, last.DeviceID, "https://private.invalid/cache?token=remote-secret", "http://private.invalid/auth?key=another-secret",
				}, " "))
				scenario.fail(h.guangya, cause)
				h.a.guangyaOpen = func(tokens cloudguangya.Tokens, persist func(cloudguangya.Tokens) error) cloud115.Provider {
					h.received = tokens
					h.guangya.onCheck = func() error {
						if err := persist(first); err != nil {
							return err
						}
						return persist(last)
					}
					return h.guangya
				}
				_, out := h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{
					"access_token": " Bearer " + access + " ", "refresh_token": " " + refresh + " ", "device_id": "", "revision": before.Revision,
				}, http.StatusBadRequest)
				message, ok := out["error"].(string)
				if !ok || !strings.HasPrefix(message, scenario.stage) || !strings.Contains(message, "HTTP 请求失败（401）") {
					t.Fatal("错误未保留阶段和可诊断原因", message)
				}
				for _, secret := range []string{access, refresh, first.Access, first.Refresh, last.Access, last.Refresh, first.DeviceID, last.DeviceID, "first-private-suffix", "private.invalid", "remote-secret", "another-secret", "https://", "http://"} {
					if strings.Contains(message, secret) {
						t.Fatal("配置失败响应泄露原始或刷新后的秘密")
					}
				}
				if !strings.Contains(message, "[REDACTED]") || !strings.Contains(message, "[URL]") {
					t.Fatal("未对令牌和远端地址执行脱敏", message)
				}
				if len(h.received.DeviceID) != 32 {
					t.Fatal("首次设备 ID 留空未被规范化")
				}
				after, err := h.a.db.CloudGuangyaConnection()
				if !existing {
					if !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("失败的首次验证保存了连接", err)
					}
				} else if err != nil || after.AccountID != before.AccountID || after.FolderID != before.FolderID || after.Revision != before.Revision || !bytes.Equal(after.Secrets, before.Secrets) {
					t.Fatal("失败验证改变了已有连接或候选凭据", err)
				}
			})
		}
	}
}

func TestCloudGuangyaConfigurationRejectsMissingAccountAndCacheIdentityMismatch(t *testing.T) {
	for _, scenario := range []struct {
		name, reason string
		change       func(*namedCloudFixture)
	}{
		{"账号缺失", "未返回有效账号身份", func(f *namedCloudFixture) { f.account = "" }},
		{"账号为空白", "未返回有效账号身份", func(f *namedCloudFixture) { f.account = " \t " }},
		{"目录ID错误", "返回的目录 ID 与缓存目录不一致", func(f *namedCloudFixture) { f.cacheInfo.ID = "other-directory" }},
		{"路径不是目录", "返回的缓存路径不是文件夹", func(f *namedCloudFixture) { f.cacheInfo.Directory = false }},
		{"目录不在根下", "缓存目录不位于网盘根目录", func(f *namedCloudFixture) { f.cacheInfo.ParentID = "other-parent" }},
		{"目录名称错误", "缓存目录名称不匹配", func(f *namedCloudFixture) { f.cacheInfo.Name = "other-cache-name" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newGuangyaServerHarness(t)
			h.guangya.cacheInfo = &cloud115.File{ID: "12", ParentID: "0", Name: cloud115.CacheFolderName, Directory: true}
			scenario.change(h.guangya)
			_, out := h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{
				"access_token": "guangya-secret-access", "refresh_token": "guangya-secret-refresh", "device_id": "", "revision": 0,
			}, http.StatusBadRequest)
			message, ok := out["error"].(string)
			if !ok || !strings.Contains(message, scenario.reason) {
				t.Fatal("拒绝无效身份时未说明对应原因", message)
			}
			if strings.Contains(message, "other-directory") || strings.Contains(message, "other-parent") || strings.Contains(message, "other-cache-name") {
				t.Fatal("目录身份错误响应暴露远端内部字段")
			}
			if _, err := h.a.db.CloudGuangyaConnection(); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("身份核验失败仍保存了连接", err)
			}
		})
	}
}

func TestCloudGuangyaOfflineRoutesSeparateBindingsAndPermissions(t *testing.T) {
	h := newGuangyaServerHarness(t)
	h.configure(t)
	legacy := h.submit(t, "115")
	guangya := h.submit(t, "guangya")
	if legacy.ID == guangya.ID || legacy.InfoHash != guangya.InfoHash || legacy.AccountID != "shared-account" || guangya.AccountID != store.CloudGuangyaAccount("shared-account") {
		t.Fatal("相同账号和磁力未按网盘隔离")
	}
	h.call(t, http.MethodPost, h.base("guangya"), h.admin, map[string]any{"resource_id": "forged"}, http.StatusNotFound)
	h.call(t, http.MethodPost, h.base("guangya"), h.admin, map[string]any{"resource_id": "1", "magnet": "bad"}, http.StatusBadRequest)
	h.call(t, http.MethodPost, h.base("guangya"), h.admin, map[string]any{"magnet": "not-a-magnet"}, http.StatusBadRequest)
	for _, pair := range []struct {
		provider string
		other    store.OfflineTask
	}{{"115", guangya}, {"guangya", legacy}} {
		h.call(t, http.MethodPost, h.base(pair.provider)+"/selection", h.admin, map[string]any{"task_id": pair.other.ID, "file_id": "forged"}, http.StatusNotFound)
		h.call(t, http.MethodPost, h.base(pair.provider)+"/tasks/"+pair.other.ID+"/cancel", h.admin, map[string]any{"version": pair.other.Version}, http.StatusNotFound)
	}
	_, reader := h.reader(t, "guangya-reader")
	h.call(t, http.MethodGet, "/api/admin/guangya/connection", reader, nil, http.StatusForbidden)
	h.call(t, http.MethodPut, "/api/admin/guangya/connection", reader, map[string]any{"revision": 1}, http.StatusForbidden)
	h.call(t, http.MethodPost, h.base("guangya"), reader, map[string]any{"resource_id": "1"}, http.StatusForbidden)
	h.call(t, http.MethodPost, h.base("guangya")+"/selection", reader, map[string]any{"task_id": guangya.ID, "file_id": "guangya-file"}, http.StatusForbidden)
	h.call(t, http.MethodPost, h.base("guangya")+"/tasks/"+guangya.ID+"/cancel", reader, map[string]any{"version": guangya.Version}, http.StatusForbidden)
	_, pending := h.call(t, http.MethodGet, h.base("guangya"), reader, nil, http.StatusOK)
	if len(cloudResponseList(t, pending, "tasks")) != 0 || len(cloudResponseList(t, pending, "sources")) != 0 {
		t.Fatal("普通用户看到管理员任务或未完成来源")
	}
	h.call(t, http.MethodPost, h.base("guangya")+"/tasks/"+guangya.ID+"/cancel", h.admin, map[string]any{"version": guangya.Version}, http.StatusOK)
	current, err := h.a.db.OfflineTask(guangya.ID)
	if err != nil || current.Status != "cancelled" || !strings.Contains(current.Error, "光鸭") {
		t.Fatal("光鸭取消未操作正确队列", err)
	}
	h.call(t, http.MethodPost, h.base("guangya")+"/tasks/"+guangya.ID+"/retry", h.admin, map[string]any{"version": current.Version}, http.StatusOK)
	current, err = h.a.db.OfflineTask(legacy.ID)
	if err != nil || current.Status != "queued" || current.Version != legacy.Version {
		t.Fatal("光鸭操作影响了 115 任务", err)
	}
	guangya = h.ready(t, "guangya")
	legacy = h.ready(t, "115")
	_, ready := h.call(t, http.MethodGet, h.base("guangya"), reader, nil, http.StatusOK)
	if len(cloudResponseList(t, ready, "tasks")) != 0 || len(cloudResponseList(t, ready, "sources")) != 1 || cloudResponseSource(t, ready, "sources", "guangya-"+guangya.ID)["Provider"] != "guangya" {
		t.Fatal("光鸭只读来源混入管理员任务或 115 来源")
	}
	_, legacyOnly := h.call(t, http.MethodGet, h.base("115"), reader, nil, http.StatusOK)
	if len(cloudResponseList(t, legacyOnly, "sources")) != 1 || cloudResponseSource(t, legacyOnly, "sources", "115-"+legacy.ID)["Provider"] != "115" {
		t.Fatal("115 来源被光鸭接口改写")
	}
}

func TestCloudGuangyaEmbySourcesTicketsRedirectExpiryAndRevocation(t *testing.T) {
	h := newGuangyaServerHarness(t)
	h.configure(t)
	legacy := h.ready(t, "115")
	guangya := h.ready(t, "guangya")
	user, reader := h.reader(t, "ticket-reader")
	item := r18ItemID(h.library.ID, h.video.ID)
	_, combined := h.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", reader, nil, http.StatusOK)
	if len(cloudResponseList(t, combined, "MediaSources")) != 2 {
		t.Fatal("Emby 未汇总两盘就绪来源")
	}
	cloudResponseSource(t, combined, "MediaSources", "115-"+legacy.ID)
	source := cloudResponseSource(t, combined, "MediaSources", "guangya-"+guangya.ID)
	stream, _ := source["DirectStreamUrl"].(string)
	_, selected := h.call(t, http.MethodPost, "/Items/"+item+"/PlaybackInfo", reader, map[string]any{"MediaSourceId": "guangya-" + guangya.ID}, http.StatusOK)
	if len(cloudResponseList(t, selected, "MediaSources")) != 1 || cloudResponseSource(t, selected, "MediaSources", "guangya-"+guangya.ID)["Provider"] != "guangya" {
		t.Fatal("显式光鸭来源被其它网盘替代")
	}
	h.call(t, http.MethodGet, "/Videos/"+item+"/stream?MediaSourceId=guangya-"+guangya.ID, "", nil, http.StatusUnauthorized)
	h.ua = "Actual-Guangya-Player-UA"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		header, _ := h.call(t, method, stream, "", nil, http.StatusFound)
		if header.Get("Location") != "https://guangya.example/movie?sign=private" || header.Get("Cache-Control") != "no-store" || h.guangya.ua != h.ua || h.legacy.urls != 0 {
			t.Fatal("光鸭播放未即时解析、未使用实际 UA 或调用了 115")
		}
	}
	if h.guangya.urls != 2 {
		t.Fatal("GET/HEAD 未各自即时获取直链")
	}
	before := h.guangya.urls
	tampered, err := url.Parse(stream)
	if err != nil {
		t.Fatal(err)
	}
	query := tampered.Query()
	query.Set("MediaSourceId", "115-"+legacy.ID)
	tampered.RawQuery = query.Encode()
	h.call(t, http.MethodGet, tampered.String(), "", nil, http.StatusUnauthorized)
	query.Set("MediaSourceId", "guangya-forged")
	tampered.RawQuery = query.Encode()
	h.call(t, http.MethodGet, tampered.String(), "", nil, http.StatusUnauthorized)
	if h.guangya.urls != before || h.legacy.urls != 0 {
		t.Fatal("篡改票据访问了网盘")
	}
	guangya.ExpiresAt = time.Now().Add(-time.Second).Unix()
	if err := h.a.db.UpdateOfflineTask(guangya, guangya.Version); err != nil {
		t.Fatal(err)
	}
	h.call(t, http.MethodGet, stream, "", nil, http.StatusNotFound)
	_, expired := h.call(t, http.MethodGet, h.base("guangya"), reader, nil, http.StatusOK)
	if len(cloudResponseList(t, expired, "sources")) != 0 || h.guangya.urls != before {
		t.Fatal("过期光鸭来源仍可播放")
	}
	guangya, err = h.a.db.OfflineTask(guangya.ID)
	if err != nil {
		t.Fatal(err)
	}
	guangya.ExpiresAt = time.Now().Add(time.Hour).Unix()
	if err := h.a.db.UpdateOfflineTask(guangya, guangya.Version); err != nil {
		t.Fatal(err)
	}
	h.call(t, http.MethodPost, h.base("guangya")+"/selection", h.admin, map[string]any{"task_id": guangya.ID, "file_id": "guangya-file"}, http.StatusOK)
	if err := h.a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	h.call(t, http.MethodGet, stream, "", nil, http.StatusNotFound)
	if h.guangya.urls != before {
		t.Fatal("撤销媒体库权限后票据仍能解析网盘直链")
	}
	if err := h.a.db.ForUser(user.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: []int64{h.library.ID}}); err != nil {
		t.Fatal(err)
	}
	if err := h.a.db.EditUser(user.ID, user.Name, "", true); err != nil {
		t.Fatal(err)
	}
	h.call(t, http.MethodGet, stream, "", nil, http.StatusUnauthorized)
	if h.guangya.urls != before {
		t.Fatal("禁用用户的播放票据仍然生效")
	}
}

func TestCloudGuangyaPreparationIsSingleUseAndConnectionIsolated(t *testing.T) {
	for _, scenario := range []string{"single-use", "expired-proof", "115-revision", "guangya-revision", "wrong-user", "other-provider-proof"} {
		t.Run(scenario, func(t *testing.T) {
			h := newGuangyaServerHarness(t)
			h.configure(t)
			legacy := h.ready(t, "115")
			guangya := h.ready(t, "guangya")
			_, reader := h.reader(t, "prepare-reader")
			h.call(t, http.MethodGet, h.base("guangya")+"/prepare?source_id=115-"+legacy.ID, reader, nil, http.StatusBadRequest)
			h.call(t, http.MethodGet, h.base("115")+"/prepare?source_id=guangya-"+guangya.ID, reader, nil, http.StatusBadRequest)
			_, prepared := h.call(t, http.MethodGet, h.base("guangya")+"/prepare?source_id=guangya-"+guangya.ID, reader, nil, http.StatusOK)
			source := cloudResponseSource(t, prepared, "sources", "guangya-"+guangya.ID)
			stream, _ := source["DirectStreamUrl"].(string)
			parsed, err := url.Parse(stream)
			if err != nil {
				t.Fatal(err)
			}
			nonce := parsed.Query().Get("preparation_token")
			if len(nonce) != 36 || h.guangya.urls != 0 || len(cloudResponseList(t, prepared, "tasks")) != 0 {
				t.Fatal("预检未生成单次证明、提前获取直链或公开任务")
			}
			streamUser := ""
			switch scenario {
			case "expired-proof":
				data, ok := h.a.cloud115Prepared.Get(nonce)
				if !ok {
					t.Fatal("预检证明未缓存")
				}
				h.a.cloud115Prepared.Set(nonce, data, time.Nanosecond)
				time.Sleep(time.Millisecond)
			case "115-revision":
				h.call(t, http.MethodPut, "/api/admin/115/connection", h.admin, map[string]any{"revision": 1}, http.StatusNoContent)
			case "guangya-revision":
				h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin, map[string]any{"revision": 1}, http.StatusNoContent)
			case "wrong-user":
				_, streamUser = h.reader(t, "other-prepare-reader")
			case "other-provider-proof":
				_, other := h.call(t, http.MethodGet, h.base("115")+"/prepare?source_id=115-"+legacy.ID, reader, nil, http.StatusOK)
				otherURL, _ := url.Parse(cloudResponseSource(t, other, "sources", "115-"+legacy.ID)["DirectStreamUrl"].(string))
				query := parsed.Query()
				query.Set("preparation_token", otherURL.Query().Get("preparation_token"))
				parsed.RawQuery = query.Encode()
				stream = parsed.String()
			}
			before := h.guangya.infos
			h.call(t, http.MethodGet, stream, streamUser, nil, http.StatusFound)
			if h.guangya.urls != 1 || h.legacy.urls != 0 || h.guangya.ua != h.ua {
				t.Fatal("预检播放使用了错误网盘或缓存直链")
			}
			if scenario == "single-use" || scenario == "115-revision" {
				if h.guangya.infos != before {
					t.Fatal("有效光鸭预检未复用，或 115 修订使其失效")
				}
			} else if h.guangya.infos != before+1 {
				t.Fatal("失效预检绕过文件即时核验", scenario)
			}
			if scenario == "single-use" {
				h.call(t, http.MethodGet, stream, "", nil, http.StatusFound)
				if h.guangya.infos != before+1 || h.guangya.urls != 2 {
					t.Fatal("重复使用预检证明绕过即时文件核验")
				}
			}
		})
	}
}

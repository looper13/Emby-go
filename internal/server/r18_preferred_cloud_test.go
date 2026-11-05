package server

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/clouddrive"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
)

// 两盘使用同一原始账号和相同磁力，验证网盘选择同时约束队列、账号命名空间和直链。
type r18PreferredCloudProvider struct {
	*r18NativeCloudProvider
	name           string
	submitError    error
	submitAttempts atomic.Int32
}

func (p *r18PreferredCloudProvider) AddTask(ctx context.Context, magnet, folder string) (string, error) {
	p.submitAttempts.Add(1)
	if p.submitError != nil {
		return "", p.submitError
	}
	return p.r18NativeCloudProvider.AddTask(ctx, magnet, folder)
}

func (p *r18PreferredCloudProvider) URL(ctx context.Context, code, ua string) (string, error) {
	raw, err := p.r18NativeCloudProvider.URL(ctx, code, ua)
	return strings.Replace(raw, "https://guangya.example/", "https://"+p.name+".example/", 1), err
}

type r18PreferredCloudHarness struct {
	*guangyaServerHarness
	catalog *r18NativeHTTPSource
	clouds  map[string]*r18PreferredCloudProvider
}

func newR18PreferredCloudHarness(t *testing.T, preference string, configured ...string) *r18PreferredCloudHarness {
	t.Helper()
	h := newGuangyaServerHarness(t)
	h.client.Timeout = 8 * time.Second
	f := &r18PreferredCloudHarness{guangyaServerHarness: h, catalog: newR18NativeHTTPSource(h), clouds: map[string]*r18PreferredCloudProvider{}}
	for name, fixture := range map[string]*namedCloudFixture{"guangya": h.guangya, "115": h.legacy} {
		f.clouds[name] = &r18PreferredCloudProvider{
			r18NativeCloudProvider: &r18NativeCloudProvider{namedCloudFixture: fixture, tasks: map[string]r18NativeRemoteTask{}},
			name:                   name,
		}
	}
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider {
		return f.clouds["guangya"]
	}
	h.a.cloud115Open = func(cloud115.Tokens, func(cloud115.Tokens) error) cloud115.Provider { return f.clouds["115"] }
	for _, provider := range configured {
		if provider == "115" {
			h.call(t, http.MethodPut, "/api/admin/115/connection", h.admin,
				map[string]any{"cookie": "UID=115-token-secret; CID=fixture-cid; SEID=fixture-seid", "revision": 0}, http.StatusNoContent)
		} else {
			h.call(t, http.MethodPut, "/api/admin/guangya/connection", h.admin,
				map[string]any{"access_token": "guangya-secret-access", "refresh_token": "guangya-secret-refresh", "revision": 0}, http.StatusNoContent)
		}
	}
	f.setPreference(preference)
	return f
}

func (f *r18PreferredCloudHarness) setPreference(provider string) {
	f.a.cfgMu.Lock()
	f.a.cfg.R18PreferredCloud = provider
	f.a.cfgMu.Unlock()
}

func (f *r18PreferredCloudHarness) startQueues(t *testing.T) {
	t.Helper()
	for _, provider := range []string{"115", "guangya"} {
		queue := offline.NewForProvider(f.a.rootCtx, f.a.db, func() (clouddrive.Provider, string, error) {
			return f.a.providerFor(provider)
		}, cloudName(provider), func() (string, error) {
			connection, err := f.a.offlineConnection(provider)
			return connection.AccountID, err
		})
		if provider == "115" {
			f.a.offlineService = queue
		} else {
			f.a.guangyaOffline = queue
		}
		t.Cleanup(queue.Close)
	}
}

func (f *r18PreferredCloudHarness) assertNoDownload(t *testing.T) {
	t.Helper()
	for name, p := range f.clouds {
		if p.submitAttempts.Load() != 0 || p.creates.Load() != 0 || p.urlAttempts.Load() != 0 || p.legacy.Load() != 0 {
			t.Fatalf("浏览或 HEAD 提前下载、查询历史或取得 %s 直链", name)
		}
	}
}

func (f *r18PreferredCloudHarness) assertOnlyDownload(t *testing.T, chosen, hash, status string) {
	t.Helper()
	for name, p := range f.clouds {
		connection, err := f.a.offlineConnection(name)
		if err != nil {
			if name == chosen {
				t.Fatal("实际选中的网盘没有连接", name, err)
			}
			continue
		}
		bindings, err := f.a.db.OfflineBindings(f.library, f.video.ID, connection.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		if name == chosen {
			if len(bindings) != 1 || bindings[0].Task.InfoHash != hash || bindings[0].Task.AccountID != connection.AccountID || bindings[0].Task.Status != status {
				t.Fatalf("%s 下载没有绑定所选账号、磁力和状态：%+v", name, bindings)
			}
		} else if len(bindings) != 0 || p.submitAttempts.Load() != 0 || p.creates.Load() != 0 || p.urlAttempts.Load() != 0 {
			t.Fatalf("点播 %s 时创建或播放了 %s 任务：%+v", chosen, name, bindings)
		}
	}
}

func TestR18PreferredCloudSourcesFollowSettingAndConfiguredFallback(t *testing.T) {
	for _, tc := range []struct {
		name, preference, want string
		configured             []string
	}{
		{"默认光鸭", "", "guangya", []string{"guangya", "115"}},
		{"优先115", "115", "115", []string{"guangya", "115"}},
		{"显式优先光鸭", "guangya", "guangya", []string{"guangya", "115"}},
		{"光鸭未配置时使用115", "guangya", "115", []string{"115"}},
		{"115未配置时使用光鸭", "115", "guangya", []string{"guangya"}},
		{"仅115配置", "115", "115", []string{"115"}},
		{"双盘均未配置", "115", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreferredCloudHarness(t, tc.preference, tc.configured...)
			item := r18ItemID(f.library.ID, f.video.ID)
			_, playback := f.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", f.admin, nil, http.StatusOK)
			sources := cloudResponseList(t, playback, "MediaSources")
			if tc.want == "" {
				if len(sources) != 0 || playback["ErrorCode"] != "NoCompatibleStream" {
					t.Fatal("没有配置任何网盘仍发布了可下载磁力", playback)
				}
			} else {
				if len(sources) != 2 {
					t.Fatal("可用网盘没有发布完整磁力版本", playback)
				}
				for index, hash := range []string{r18PlaybackHashA, r18PlaybackHashB} {
					id := r18MagnetSourceID(tc.want, hash)
					source := cloudResponseSource(t, playback, "MediaSources", id)
					if source["Provider"] != tc.want || source["Default"] != (index == 0) || !strings.Contains(source["Name"].(string), cloudName(tc.want)) {
						t.Fatal("版本的网盘身份、名称或默认顺序没有遵循配置", source)
					}
				}
			}
			_, detail := f.call(t, http.MethodGet, "/Items/"+item, f.admin, nil, http.StatusOK)
			if detail["IsPlaceHolder"] != (tc.want == "") {
				t.Fatal("单片占位状态没有遵循可用网盘", detail)
			}
			reads := f.catalog.allReads.Load()
			_, list := f.call(t, http.MethodGet, "/Items?ParentId="+externalLibraryID(f.library.ID), f.admin, nil, http.StatusOK)
			items := cloudResponseList(t, list, "Items")
			if len(items) != 1 || items[0].(map[string]any)["IsPlaceHolder"] != (tc.want == "") || f.catalog.allReads.Load() != reads {
				t.Fatal("列表占位状态错误，或为列表额外查询了磁力", list)
			}
			f.assertNoDownload(t)
		})
	}
}

func TestR18PreferredCloudDefaultStreamCreatesOnlyChosenQueue(t *testing.T) {
	for _, tc := range []struct {
		name, preference, want string
		configured             []string
	}{
		{"双盘优先光鸭", "guangya", "guangya", []string{"guangya", "115"}},
		{"双盘优先115", "115", "115", []string{"guangya", "115"}},
		{"只有115时回退115", "guangya", "115", []string{"115"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreferredCloudHarness(t, tc.preference, tc.configured...)
			f.startQueues(t)
			_, reader := f.reader(t, "preferred-cloud-reader")
			item := r18ItemID(f.library.ID, f.video.ID)
			path := "/emby/Videos/" + item + "/stream?Static=true"
			f.call(t, http.MethodHead, path, reader, nil, http.StatusOK)
			f.assertNoDownload(t)
			for range 2 {
				headers, _ := f.call(t, http.MethodGet, path, reader, nil, http.StatusFound)
				if headers.Get("Location") != "https://"+tc.want+".example/"+r18PlaybackHashA+".mp4?sign=private" {
					t.Fatal("默认播放跳到了错误网盘或其它磁力", headers)
				}
			}
			chosen := f.clouds[tc.want]
			if chosen.submitAttempts.Load() != 1 || chosen.creates.Load() != 1 || chosen.legacy.Load() != 0 {
				t.Fatal("默认磁力下载重复提交，或扫描了历史任务")
			}
			f.assertOnlyDownload(t, tc.want, r18PlaybackHashA, "ready")
		})
	}
}

func TestR18PreferredCloudExplicitSourceKeepsDriveAfterPreferenceChange(t *testing.T) {
	f := newR18PreferredCloudHarness(t, "guangya", "guangya", "115")
	f.startQueues(t)
	_, reader := f.reader(t, "preferred-explicit-reader")
	item := r18ItemID(f.library.ID, f.video.ID)
	oldID := r18MagnetSourceID("guangya", r18PlaybackHashB)
	_, oldPlayback := f.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", reader, nil, http.StatusOK)
	oldStream := cloudResponseSource(t, oldPlayback, "MediaSources", oldID)["DirectStreamUrl"].(string)
	f.setPreference("115")
	_, changed := f.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", reader, nil, http.StatusOK)
	newDefault := cloudResponseList(t, changed, "MediaSources")[0].(map[string]any)
	if newDefault["Provider"] != "115" || newDefault["Id"] != r18MagnetSourceID("115", r18PlaybackHashA) {
		t.Fatal("偏好更新后默认来源没有即时切换", changed)
	}
	for _, downloaded := range []bool{false, true} {
		_, selected := f.call(t, http.MethodPost, "/Items/"+item+"/PlaybackInfo", reader, map[string]any{"MediaSourceId": oldID}, http.StatusOK)
		if len(cloudResponseList(t, selected, "MediaSources")) != 1 || cloudResponseSource(t, selected, "MediaSources", oldID)["Provider"] != "guangya" {
			t.Fatal("显式旧来源被偏好覆盖或被同哈希的新来源挤掉", selected)
		}
		if !downloaded {
			f.call(t, http.MethodHead, oldStream, "", nil, http.StatusOK)
			f.assertNoDownload(t)
		}
		headers, _ := f.call(t, http.MethodGet, oldStream, "", nil, http.StatusFound)
		if headers.Get("Location") != "https://guangya.example/"+r18PlaybackHashB+".mp4?sign=private" {
			t.Fatal("旧签名播放入口在更新偏好后跨盘", headers)
		}
	}
	if f.clouds["guangya"].creates.Load() != 1 {
		t.Fatal("显式旧版本没有复用已就绪任务")
	}
	f.assertOnlyDownload(t, "guangya", r18PlaybackHashB, "ready")
}

func TestR18PreferredCloudSubmissionFailureNeverSwitchesDrive(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		auth         bool
		httpStatus   int
	}{
		{"创建失败", "failed", false, http.StatusConflict},
		{"鉴权失败", "needs_auth", true, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18PreferredCloudHarness(t, "115", "guangya", "115")
			f.clouds["115"].submitError = &clouddrive.Error{Message: "测试创建拒绝", Auth: tc.auth}
			f.startQueues(t)
			_, reader := f.reader(t, "preferred-failure-reader")
			item := r18ItemID(f.library.ID, f.video.ID)
			f.call(t, http.MethodGet, "/Videos/"+item+"/stream", reader, nil, tc.httpStatus)
			if f.clouds["115"].submitAttempts.Load() != 1 || f.clouds["115"].creates.Load() != 0 {
				t.Fatal("明确失败后仍重复提交或错误记录成功下载")
			}
			f.assertOnlyDownload(t, "115", r18PlaybackHashA, tc.status)
		})
	}
}

func TestR18PreferredCloudUnavailableExplicitSourceNeverFallsBack(t *testing.T) {
	for _, chosen := range []string{"guangya", "115"} {
		t.Run(chosen, func(t *testing.T) {
			other := "115"
			if chosen == "115" {
				other = "guangya"
			}
			f := newR18PreferredCloudHarness(t, other, other)
			f.startQueues(t)
			item := r18ItemID(f.library.ID, f.video.ID)
			id := r18MagnetSourceID(chosen, r18PlaybackHashA)
			_, playback := f.call(t, http.MethodPost, "/Items/"+item+"/PlaybackInfo", f.admin, map[string]any{"MediaSourceId": id}, http.StatusOK)
			if len(cloudResponseList(t, playback, "MediaSources")) != 0 || playback["ErrorCode"] != "NoCompatibleStream" {
				t.Fatal("显式选择的未配置网盘被替换成其它来源", playback)
			}
			f.call(t, http.MethodGet, "/Videos/"+item+"/stream?MediaSourceId="+id, f.admin, nil, http.StatusServiceUnavailable)
			f.assertNoDownload(t)
		})
	}
}

func TestR18PreferredCloudExplicitSourceIgnoresOtherDriveReadFailure(t *testing.T) {
	for _, tc := range []struct {
		unavailable, chosen, breakQuery string
	}{
		{"guangya", "115", "ALTER TABLE cloud_guangya_connection RENAME TO unavailable_guangya_connection"},
		{"115", "guangya", "ALTER TABLE cloud115_connection RENAME TO unavailable_115_connection"},
	} {
		t.Run(tc.unavailable, func(t *testing.T) {
			f := newR18PreferredCloudHarness(t, tc.unavailable, "guangya", "115")
			// 仅破坏当前测试临时数据库中的默认盘表，模拟读取失败而非未配置。
			db, err := sql.Open("sqlite", f.a.configSnapshot().DBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(tc.breakQuery); err != nil {
				t.Fatal("无法建立网盘配置读取失败场景", err)
			}
			if _, err := f.a.offlineConnection(tc.unavailable); err == nil {
				t.Fatal("默认网盘配置仍可读取，未复现隔离条件")
			}
			item := r18ItemID(f.library.ID, f.video.ID)
			id := r18MagnetSourceID(tc.chosen, r18PlaybackHashA)
			path := "/Items/" + item + "/PlaybackInfo"
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				selectedPath := path
				var body any
				if method == http.MethodGet {
					selectedPath += "?MediaSourceId=" + id
				} else {
					body = map[string]any{"MediaSourceId": id}
				}
				_, selected := f.call(t, method, selectedPath, f.admin, body, http.StatusOK)
				if len(cloudResponseList(t, selected, "MediaSources")) != 1 || cloudResponseSource(t, selected, "MediaSources", id)["Provider"] != tc.chosen {
					t.Fatal("未选择的网盘读取失败阻断了显式来源，或选错网盘", selected)
				}
			}
			reads := f.catalog.allReads.Load()
			f.call(t, http.MethodGet, path, f.admin, nil, http.StatusBadGateway)
			if f.catalog.allReads.Load() != reads {
				t.Fatal("默认网盘读取失败后仍跨盘读取了磁力来源")
			}
			f.assertNoDownload(t)
			var tasks int
			if err := db.QueryRow("SELECT COUNT(*) FROM offline_tasks").Scan(&tasks); err != nil || tasks != 0 {
				t.Fatal("播放信息查询创建了本地下载任务", tasks, err)
			}
		})
	}
}

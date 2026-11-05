package server

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"emby-go/internal/store"
)

func TestR18DefaultLocalStreamAliasesAndEmptySelection(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/default.mp4")
	r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	for _, prefix := range []string{"", "/emby"} {
		for _, path := range []string{"/Videos/" + f.item() + "/stream", "/videos/" + f.item() + "/stream.mp4"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, query := range []string{"?Static=true", "?MediaSourceId=&Static=true", "?mediasourceid=&Static=true"} {
					t.Run(method+prefix+path+query, func(t *testing.T) {
						before := f.fake.getCalls.Load()
						resp, body := r18LocalVirtualRequest(t, f, method, prefix+path+query, http.Header{"X-Emby-Token": {f.token}})
						if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://media.fixture.invalid/default.mp4" {
							t.Fatalf("默认本地版本没有起播：状态=%d，地址=%q，响应=%s", resp.StatusCode, resp.Header.Get("Location"), body)
						}
						if method == http.MethodHead && len(body) != 0 {
							t.Fatal("默认本地 HEAD 返回了正文")
						}
						// 配置网盘后可能查询磁力列表，但转派不能重复读取影片详情。
						if f.fake.getCalls.Load()-before > 1 {
							t.Fatal("默认本地转派重复读取了影片上游", f.fake.getCalls.Load()-before)
						}
					})
				}
			}
		}
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18DefaultLocalStreamUsesStableFirstLocalVersion(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	locations := map[string]string{}
	for _, name := range []string{"z.strm", "a.strm"} {
		location := "https://media.fixture.invalid/" + strings.TrimSuffix(name, ".strm") + ".mp4"
		writeFile(t, filepath.Join(f.directory.Path, name), location)
		id := r18LocalAddMovie(t, f, f.directory, name, "TEST-001", nil)
		locations["mediasource_"+strconv.FormatInt(id, 10)] = location
	}
	playback := r18LocalHTTPJSON(t, f, http.MethodGet, "/Items/"+f.item()+"/PlaybackInfo", f.token, "", http.StatusOK)
	sources := cloudResponseList(t, playback, "MediaSources")
	if len(sources) != 2 {
		t.Fatal("多本地版本夹具缺少来源", playback)
	}
	defaultID, _ := sources[0].(map[string]any)["Id"].(string)
	if locations[defaultID] == "" || sources[0].(map[string]any)["Default"] != true {
		t.Fatal("本地默认版本无效", playback)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodGet} {
		resp, body := r18LocalVirtualRequest(t, f, method, streamURL(f.item()), http.Header{"X-Emby-Token": {f.token}})
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != locations[defaultID] {
			t.Fatalf("默认起播未保持第一个本地版本：状态=%d，地址=%q，响应=%s", resp.StatusCode, resp.Header.Get("Location"), body)
		}
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18DefaultLocalStreamRequiresLoginAndBothLibraryPermissions(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/default.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	endpoint := streamURL(f.item())
	for _, path := range []string{endpoint, f.app.cloudStreamURL(f.library, f.fake.video.ID, ""), f.app.cloudStreamURL(f.library, f.fake.video.ID, "mediasource_"+strconv.FormatInt(id, 10))} {
		resp, body := r18LocalVirtualRequest(t, f, http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("匿名或网盘票据错误授权了默认本地来源：状态=%d，响应=%s", resp.StatusCode, body)
		}
	}
	viewer, err := f.app.db.CreateUser("default-local-viewer", "password-1234")
	if err != nil {
		t.Fatal(err)
	}
	token := "fixture-default-local-viewer-token"
	if err := f.app.db.SaveUserToken(token, viewer.ID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		ids    []int64
		status int
	}{
		{"只有目录库", []int64{f.directory.ID}, http.StatusNotFound},
		{"只有资料库", []int64{f.library.ID}, http.StatusNotFound},
		{"两库都授权", []int64{f.library.ID, f.directory.ID}, http.StatusFound},
		{"撤销目录库", []int64{f.library.ID}, http.StatusNotFound},
		{"恢复两库授权", []int64{f.library.ID, f.directory.ID}, http.StatusFound},
		{"撤销资料库", []int64{f.directory.ID}, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := f.app.db.ForUser(viewer.ID).SaveLibraryAccessPolicy(store.LibraryAccessPolicy{LibraryIDs: test.ids}); err != nil {
				t.Fatal(err)
			}
			resp, body := r18LocalVirtualRequest(t, f, http.MethodGet, endpoint, http.Header{"X-Emby-Token": {token}})
			if resp.StatusCode != test.status {
				t.Fatalf("默认本地来源未复核双库授权：状态=%d，预期=%d，响应=%s", resp.StatusCode, test.status, body)
			}
		})
	}
	r18LocalNoTasksOrCloud(t, f)
}

func TestR18DefaultLocalStreamNeverReplacesExplicitSelection(t *testing.T) {
	f := newR18LocalHTTPFixture(t)
	writeFile(t, filepath.Join(f.directory.Path, "movie.strm"), "https://media.fixture.invalid/default.mp4")
	id := r18LocalAddMovie(t, f, f.directory, "movie.strm", "TEST-001", nil)
	for _, test := range []struct {
		source string
		status int
	}{
		{"mediasource_999999", http.StatusNotFound},
		{"mediasource_0" + strconv.FormatInt(id, 10), http.StatusNotFound},
		{"115-missing-task", http.StatusNotFound},
		{"guangya-missing-task", http.StatusServiceUnavailable},
		{"unknown-source", http.StatusNotFound},
	} {
		resp, body := r18LocalVirtualRequest(t, f, http.MethodGet, r18LocalVirtualURL(f, test.source), http.Header{"X-Emby-Token": {f.token}})
		if resp.StatusCode != test.status {
			t.Fatalf("显式失效来源被默认本地源替换：来源=%q，状态=%d，响应=%s", test.source, resp.StatusCode, body)
		}
	}
	endpoint := r18LocalVirtualURL(f, "") + "&MediaSourceId=mediasource_" + strconv.FormatInt(id, 10)
	resp, body := r18LocalVirtualRequest(t, f, http.MethodGet, endpoint, http.Header{"X-Emby-Token": {f.token}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空默认和显式版本冲突未被拒绝：状态=%d，响应=%s", resp.StatusCode, body)
	}
	var tasks int
	if err := r18LocalVirtualDB(t, f).QueryRow("SELECT COUNT(*) FROM offline_tasks").Scan(&tasks); err != nil || tasks != 0 {
		t.Fatal("显式失效来源创建了下载任务", tasks, err)
	}
}

func TestR18DefaultLocalStreamPreservesReadyCloudPriority(t *testing.T) {
	for _, provider := range []string{"115", "guangya"} {
		t.Run(provider, func(t *testing.T) {
			h := newGuangyaServerHarness(t)
			h.configure(t)
			h.ready(t, provider)
			directory, err := h.a.db.AddLibrary("默认本地目录", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(directory.Path, "movie.strm")
			writeFile(t, file, "https://media.fixture.invalid/local.mp4")
			if _, err := h.a.db.UpsertMovie(store.Movie{LibraryID: directory.ID, SourcePath: file, Number: h.video.Number, Title: "本地版本", Status: "success"}, 0, h.a.probeClock()); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				header, _ := h.call(t, method, streamURL(r18ItemID(h.library.ID, h.video.ID)), h.admin, nil, http.StatusFound)
				if header.Get("Location") != "https://"+provider+".example/movie?sign=private" {
					t.Fatal("已有网盘默认被本地版本替换", header)
				}
			}
		})
	}
}

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type r18NativeHTTPSource struct {
	*fakeR18
	resources                  []r18.Resource
	resourcesError             error
	details, allReads, lookups atomic.Int32
}

func (f *r18NativeHTTPSource) Get(ctx context.Context, id string) (r18.Video, error) {
	f.details.Add(1)
	if err := ctx.Err(); err != nil {
		return r18.Video{}, err
	}
	if id != f.video.ID {
		return r18.Video{}, sql.ErrNoRows
	}
	return f.video, nil
}

func (f *r18NativeHTTPSource) CatalogPage(ctx context.Context, _ r18.CatalogQuery) (r18.CatalogPage, error) {
	return r18.CatalogPage{Items: []r18.Video{f.video}}, ctx.Err()
}

func (f *r18NativeHTTPSource) AllResources(ctx context.Context, v r18.Video) ([]r18.Resource, error) {
	f.allReads.Add(1)
	if v.ID != f.video.ID {
		return nil, sql.ErrNoRows
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return f.resources, f.resourcesError
}

func (f *r18NativeHTTPSource) Resource(ctx context.Context, v r18.Video, id string) (r18.Resource, error) {
	f.lookups.Add(1)
	if v.ID != f.video.ID {
		return r18.Resource{}, sql.ErrNoRows
	}
	if ctx.Err() != nil {
		return r18.Resource{}, ctx.Err()
	}
	if f.resourcesError != nil {
		return r18.Resource{}, f.resourcesError
	}
	for _, resource := range f.resources {
		if resource.ID == id {
			return resource, nil
		}
	}
	return r18.Resource{}, sql.ErrNoRows
}

func newR18NativeHTTPSource(h *guangyaServerHarness) *r18NativeHTTPSource {
	v := h.video
	v.MagnetsCount = 2
	f := &r18NativeHTTPSource{fakeR18: &fakeR18{video: v}}
	for _, hash := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		f.resources = append(f.resources, r18.Resource{ID: "btih-" + hash, Number: v.Number,
			Title: "正片版本 " + hash[:8], Magnet: "magnet:?xt=urn:btih:" + hash, SizeBytes: 100})
	}
	h.a.r18Open = func(r18.Config) (r18Source, error) { return f, nil }
	return f
}

type r18NativeRemoteTask struct {
	hash, folder string
}

type r18NativeCloudProvider struct {
	*namedCloudFixture
	mu             sync.Mutex
	tasks          map[string]r18NativeRemoteTask
	creates        atomic.Int32
	legacy         atomic.Int32
	urlAttempts    atomic.Int32
	missingURLOnce atomic.Bool
	urlHashes      []string
	urlUA          string
}

func (f *r18NativeCloudProvider) Find(context.Context, string) (*cloud115.RemoteTask, error) {
	f.legacy.Add(1)
	return nil, errors.New("原生点播不能查询历史任务")
}

func (f *r18NativeCloudProvider) Add(context.Context, string, string) error {
	f.legacy.Add(1)
	return errors.New("原生点播必须直接提交并保存远端任务 ID")
}

func (f *r18NativeCloudProvider) Mkdir(_ context.Context, parent, name string) (string, error) {
	if parent != "12" || !strings.HasPrefix(name, "emby-go-") {
		return "", errors.New("缓存目录不属于当前任务")
	}
	return name, nil
}

func (f *r18NativeCloudProvider) AddTask(ctx context.Context, magnet, folder string) (string, error) {
	_, hash, err := cloud115.NormalizeMagnet(magnet)
	if err != nil || ctx.Err() != nil || !strings.HasPrefix(folder, "emby-go-") {
		return "", errors.New("磁力提交参数无效")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[hash] = r18NativeRemoteTask{hash: hash, folder: folder}
	f.creates.Add(1)
	return "native-remote-" + hash, nil
}

func (f *r18NativeCloudProvider) FindTask(ctx context.Context, hash, id string) (*cloud115.RemoteTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[hash]
	if ctx.Err() != nil || !ok || id != "native-remote-"+hash {
		return nil, errors.New("未使用所选磁力的远端任务 ID")
	}
	return &cloud115.RemoteTask{Hash: hash, RemoteID: id, FolderID: task.folder,
		ResultID: "native-file-" + hash, Status: 2, Progress: 100}, nil
}

func (f *r18NativeCloudProvider) Info(ctx context.Context, id string) (cloud115.File, error) {
	if strings.HasPrefix(id, "emby-go-") {
		return cloud115.File{ID: id, Name: id, ParentID: "12", Directory: true}, ctx.Err()
	}
	if strings.HasPrefix(id, "native-file-") {
		hash := strings.TrimPrefix(id, "native-file-")
		f.mu.Lock()
		defer f.mu.Unlock()
		task, ok := f.tasks[hash]
		if !ok {
			return cloud115.File{}, &cloud115.Error{Missing: true, Message: "不存在该结果文件"}
		}
		return cloud115.File{ID: id, Name: hash + ".mkv", ParentID: task.folder,
			PickCode: "native-code-" + hash, Size: 100, SHA1: "native-sha-" + hash}, ctx.Err()
	}
	return f.namedCloudFixture.Info(ctx, id)
}

func (f *r18NativeCloudProvider) URL(ctx context.Context, code, ua string) (string, error) {
	f.urlAttempts.Add(1)
	if f.missingURLOnce.Swap(false) {
		return "", &cloud115.Error{Missing: true, Message: "直链阶段未找到视频"}
	}
	hash := strings.TrimPrefix(code, "native-code-")
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[hash]; !ok || !strings.HasPrefix(code, "native-code-") {
		return "", errors.New("播放文件不属于所选任务")
	}
	f.urlHashes = append(f.urlHashes, hash)
	f.urlUA = ua
	return "https://guangya.example/" + hash + ".mp4?sign=private", ctx.Err()
}

func TestR18NativePlaybackInfoOffersDistinctGuangyaMagnetsWithoutDownload(t *testing.T) {
	h := newGuangyaServerHarness(t)
	f := newR18NativeHTTPSource(h)
	h.configure(t)
	user, reader := h.reader(t, "native-version-reader")
	item := r18ItemID(h.library.ID, h.video.ID)
	first, second := r18MagnetSourceID(strings.Repeat("a", 40)), r18MagnetSourceID(strings.Repeat("b", 40))
	for _, prefix := range []string{"", "/emby"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			path := prefix + "/Users/" + strconv.FormatInt(user.ID, 10) + "/Items/" + item + "/PlaybackInfo"
			_, out := h.call(t, method, path, reader, map[string]any{}, http.StatusOK)
			sources := cloudResponseList(t, out, "MediaSources")
			if len(sources) != 2 || out["PlaySessionId"] == "" || out["PlaySessionId"] == nil || out["ErrorCode"] != nil {
				t.Fatal("未下载磁力仍返回空源或缺失播放会话", out)
			}
			for index, id := range []string{first, second} {
				source := cloudResponseSource(t, out, "MediaSources", id)
				if source["Provider"] != "guangya" || source["Default"] != (index == 0) || source["SupportsDirectPlay"] != true || source["Chapters"] == nil {
					t.Fatal("原生版本缺少光鸭默认策略或媒体合同字段", source)
				}
				streams := source["MediaStreams"].([]any)
				if len(streams) == 0 || streams[0].(map[string]any)["IsExternal"] != false {
					t.Fatal("原生播放器收到空轨道或缺少必需字段", source)
				}
				u, err := url.Parse(source["DirectStreamUrl"].(string))
				if err != nil || !strings.HasPrefix(u.Path, "/Videos/") || strings.HasPrefix(u.Path, "/emby") || u.Query().Get("MediaSourceId") != id || u.Query().Get("playback_token") == "" {
					t.Fatal("磁力没有转换为选定版本的签名 HTTP 播放入口", source)
				}
			}
		}
		_, detail := h.call(t, http.MethodGet, prefix+"/Items/"+item, reader, nil, http.StatusOK)
		if detail["IsPlaceHolder"] != false || detail["PlayAccess"] != "Full" || len(cloudResponseList(t, detail, "MediaSources")) != 2 {
			t.Fatal("原生详情未开放磁力版本播放", detail)
		}
	}
	_, byID := h.call(t, http.MethodGet, "/Items?Ids="+item, reader, nil, http.StatusOK)
	items := cloudResponseList(t, byID, "Items")
	if len(items) != 1 || len(cloudResponseList(t, items[0].(map[string]any), "MediaSources")) != 2 {
		t.Fatal("按 ID 获取单片详情仍缺少播放版本", byID)
	}
	before := f.allReads.Load()
	_, list := h.call(t, http.MethodGet, "/Items?ParentId="+externalLibraryID(h.library.ID)+"&IsPlaceHolder=false", reader, nil, http.StatusOK)
	if len(cloudResponseList(t, list, "Items")) != 1 || f.allReads.Load() != before {
		t.Fatal("目录浏览读取了每部影片磁力，或屏蔽了可以按需播放的条目", list)
	}
	connection, _ := h.a.offlineConnection("guangya")
	bindings, err := h.a.db.OfflineBindings(h.library, h.video.ID, connection.AccountID)
	if err != nil || len(bindings) != 0 || f.lookups.Load() != 0 || h.guangya.urls != 0 || h.legacy.urls != 0 {
		t.Fatal("浏览详情或播放信息提前提交了下载或解析了直链", err)
	}
}

func TestR18NativeSelectedMagnetDownloadsAndPlaysOnlyThatVersion(t *testing.T) {
	h := newGuangyaServerHarness(t)
	f := newR18NativeHTTPSource(h)
	p := &r18NativeCloudProvider{namedCloudFixture: h.guangya, tasks: map[string]r18NativeRemoteTask{}}
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider { return p }
	h.configure(t)
	h.a.guangyaOffline = offline.NewForProvider(h.a.rootCtx, h.a.db, h.a.guangyaProvider, "光鸭", func() (string, error) {
		connection, err := h.a.offlineConnection("guangya")
		return connection.AccountID, err
	})
	t.Cleanup(h.a.guangyaOffline.Close)
	_, reader := h.reader(t, "native-play-reader")
	item := r18ItemID(h.library.ID, h.video.ID)
	firstHash, secondHash := strings.Repeat("a", 40), strings.Repeat("b", 40)
	secondID := r18MagnetSourceID(secondHash)
	_, playback := h.call(t, http.MethodPost, "/emby/Items/"+item+"/PlaybackInfo", reader,
		map[string]any{"MediaSourceId": secondID}, http.StatusOK)
	if len(cloudResponseList(t, playback, "MediaSources")) != 1 {
		t.Fatal("选择播放版本后返回了其它磁力", playback)
	}
	stream := cloudResponseSource(t, playback, "MediaSources", secondID)["DirectStreamUrl"].(string)
	h.call(t, http.MethodHead, stream, "", nil, http.StatusOK)
	if p.creates.Load() != 0 || f.lookups.Load() != 1 {
		t.Fatal("播放器 HEAD 探测触发了磁力下载")
	}
	h.ua = "Hill-Yamby-Native-Actual-UA"
	for _, path := range []string{stream, "/emby" + stream} {
		header, _ := h.call(t, http.MethodGet, path, "", nil, http.StatusFound)
		if header.Get("Location") != "https://guangya.example/"+secondHash+".mp4?sign=private" {
			t.Fatal("原生点播未跳转所选磁力文件", header)
		}
	}
	if p.creates.Load() != 1 || p.legacy.Load() != 0 || h.legacy.urls != 0 || f.lookups.Load() != 2 {
		t.Fatal("已缓存磁力再次提交、扫描历史任务或调用了另一网盘")
	}
	connection, _ := h.a.offlineConnection("guangya")
	bindings, err := h.a.db.OfflineBindings(h.library, h.video.ID, connection.AccountID)
	if err != nil || len(bindings) != 1 || bindings[0].Task.InfoHash != secondHash || bindings[0].Task.Status != "ready" {
		t.Fatal("播放一个版本提交了其它磁力，或没有发布就绪绑定", bindings, err)
	}
	_, after := h.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", reader, nil, http.StatusOK)
	if len(cloudResponseList(t, after, "MediaSources")) != 2 || cloudResponseSource(t, after, "MediaSources", secondID)["Container"] != "mkv" {
		t.Fatal("下载后版本身份变化或缺少真实文件元数据", after)
	}
	header, _ := h.call(t, http.MethodGet, "/Videos/"+item+"/stream", reader, nil, http.StatusFound)
	if header.Get("Location") != "https://guangya.example/"+firstHash+".mp4?sign=private" || p.creates.Load() != 2 {
		t.Fatal("未指定版本的点播没有默认使用首个光鸭磁力", header)
	}
	header, _ = h.call(t, http.MethodHead, stream, "", nil, http.StatusFound)
	if header.Get("Location") != "https://guangya.example/"+secondHash+".mp4?sign=private" {
		t.Fatal("另一个版本就绪后改变了原选中版本", header)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.urlUA != h.ua || len(p.urlHashes) != 4 || p.urlHashes[0] != secondHash || p.urlHashes[2] != firstHash || p.urlHashes[3] != secondHash {
		t.Fatal("实际播放器 UA 或多版本文件身份关联错误", p.urlHashes, p.urlUA)
	}
}

func TestR18NativeGuangyaDefaultPrecedesLocalAndKeepsExplicitSources(t *testing.T) {
	h := newGuangyaServerHarness(t)
	f := newR18NativeHTTPSource(h)
	h.configure(t)
	connection, err := h.a.offlineConnection("115")
	if err != nil {
		t.Fatal(err)
	}
	resource := f.resources[0]
	legacy, err := h.a.db.CreateOfflineTask(connection.AccountID, connection.FolderID, resource.Magnet,
		strings.Repeat("a", 40), h.library, h.video.ID, resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Status, legacy.Files = "ready", []cloud115.File{{ID: "115-file", Name: "115-movie.mkv", PickCode: "fixture-code", Size: 100}}
	if err := h.a.db.UpdateOfflineTask(legacy, legacy.Version); err != nil {
		t.Fatal(err)
	}
	directory, err := h.a.db.AddLibrary("原生本地版本", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory.Path, "native.strm")
	writeFile(t, file, "https://media.fixture.invalid/local.mp4")
	localID, err := h.a.db.UpsertMovie(store.Movie{LibraryID: directory.ID, SourcePath: file, Number: h.video.Number, Title: "本地版本", Status: "success"}, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item, localSource := r18ItemID(h.library.ID, h.video.ID), "mediasource_"+strconv.FormatInt(localID, 10)
	_, out := h.call(t, http.MethodGet, "/Items/"+item+"/PlaybackInfo", h.admin, nil, http.StatusOK)
	sources := cloudResponseList(t, out, "MediaSources")
	if len(sources) < 3 || sources[0].(map[string]any)["Provider"] != "guangya" || sources[0].(map[string]any)["Default"] != true {
		t.Fatal("已有本地 STRM 遮蔽了默认光鸭磁力版本", out)
	}
	for _, selected := range []string{localSource, "115-" + legacy.ID} {
		_, out := h.call(t, http.MethodPost, "/Items/"+item+"/PlaybackInfo", h.admin, map[string]any{"MediaSourceId": selected}, http.StatusOK)
		if len(cloudResponseList(t, out, "MediaSources")) != 1 || cloudResponseSource(t, out, "MediaSources", selected)["Default"] != true {
			t.Fatal("显式本地或 115 版本被默认光鸭替代", out)
		}
	}
}

func TestR18NativeMissingLinkRechecksAndRetriesOnlySelectedVersion(t *testing.T) {
	h := newGuangyaServerHarness(t)
	f := newR18NativeHTTPSource(h)
	p := &r18NativeCloudProvider{namedCloudFixture: h.guangya, tasks: map[string]r18NativeRemoteTask{}}
	p.missingURLOnce.Store(true)
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider { return p }
	h.configure(t)
	h.a.guangyaOffline = offline.NewForProvider(h.a.rootCtx, h.a.db, h.a.guangyaProvider, "光鸭", func() (string, error) {
		connection, err := h.a.offlineConnection("guangya")
		return connection.AccountID, err
	})
	t.Cleanup(h.a.guangyaOffline.Close)
	_, reader := h.reader(t, "native-link-retry-reader")
	hash := strings.Repeat("b", 40)
	item := r18ItemID(h.library.ID, h.video.ID)
	path := "/Videos/" + item + "/stream?MediaSourceId=" + r18MagnetSourceID(hash)
	header, _ := h.call(t, http.MethodGet, path, reader, nil, http.StatusFound)
	if header.Get("Location") != "https://guangya.example/"+hash+".mp4?sign=private" || p.urlAttempts.Load() != 2 || p.creates.Load() != 1 || f.lookups.Load() != 1 {
		t.Fatal("直链明确缺失后没有复查重试，或重复提交了下载", header)
	}
	if p.legacy.Load() != 0 || h.legacy.urls != 0 {
		t.Fatal("重试直链扫描了历史任务或切换了网盘")
	}
}

func TestR18NativeChunkedPlaybackSelectionAndConflicts(t *testing.T) {
	h := newGuangyaServerHarness(t)
	f := newR18NativeHTTPSource(h)
	h.configure(t)
	item := r18ItemID(h.library.ID, h.video.ID)
	first, second := r18MagnetSourceID(strings.Repeat("a", 40)), r18MagnetSourceID(strings.Repeat("b", 40))
	for _, test := range []struct {
		name, query, body string
		status            int
	}{
		{"分块 POST", "", `{"MediaSourceId":"` + second + `"}`, http.StatusOK},
		{"查询与正文冲突", "?MediaSourceId=" + first, `{"MediaSourceId":"` + second + `"}`, http.StatusBadRequest},
		{"重复查询冲突", "?MediaSourceId=" + first + "&MediaSourceId=" + second, `{}`, http.StatusBadRequest},
		{"多个正文对象", "", `{} {"MediaSourceId":"` + second + `"}`, http.StatusBadRequest},
		{"非法正文", "", `{"MediaSourceId":`, http.StatusBadRequest},
		{"重复正文来源冲突", "", `{"MediaSourceId":"` + second + `","MediaSourceId":""}`, http.StatusBadRequest},
		{"正文来源大小写冲突", "", `{"MediaSourceId":"` + second + `","mediasourceid":"` + first + `"}`, http.StatusBadRequest},
		{"相同正文来源可用", "", `{"MediaSourceId":"` + second + `","mediasourceid":"` + second + `"}`, http.StatusOK},
		{"非法查询编码", "?MediaSourceId=" + second + "%ZZ", `{}`, http.StatusBadRequest},
		{"非对象正文", "", `[]`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := f.details.Load()
			// 包装 Reader 让 net/http 使用分块传输，ContentLength 为 -1。
			body := struct{ io.Reader }{Reader: strings.NewReader(test.body)}
			req, err := http.NewRequest(http.MethodPost, h.server.URL+"/emby/Items/"+item+"/PlaybackInfo"+test.query, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Emby-Token", h.admin)
			req.Header.Set("Content-Type", "application/json")
			resp, err := h.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != test.status {
				t.Fatalf("分块或冲突选择响应错误：状态=%d，正文=%s", resp.StatusCode, raw)
			}
			if test.status == http.StatusOK {
				var out map[string]any
				if json.Unmarshal(raw, &out) != nil || len(cloudResponseList(t, out, "MediaSources")) != 1 || cloudResponseSource(t, out, "MediaSources", second)["Default"] != true {
					t.Fatalf("分块正文丢失选中版本：%s", raw)
				}
			} else if f.details.Load() != before {
				t.Fatal("冲突请求仍访问了影片上游")
			}
		})
	}
	before := f.details.Load()
	for _, query := range []string{"?MediaSourceId=" + second + "%ZZ", "?mediasourceid=" + second + "%ZZ", "?MediaSourceId=" + second + ";other=1"} {
		h.call(t, http.MethodGet, "/Videos/"+item+"/stream"+query, h.admin, nil, http.StatusBadRequest)
	}
	if f.details.Load() != before || f.lookups.Load() != 0 {
		t.Fatal("非法流查询回退到默认磁力，或访问了影片上游")
	}
}

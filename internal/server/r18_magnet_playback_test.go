package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/clouddrive"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

const r18PlaybackHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const r18PlaybackHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type r18PlaybackCatalogFixture struct {
	*fakeR18
	mu        sync.RWMutex
	resources map[string]r18.Resource
	failure   error
	hook      func(context.Context, r18.Video, string)
	calls     atomic.Int32
}

func (f *r18PlaybackCatalogFixture) Resource(ctx context.Context, video r18.Video, id string) (r18.Resource, error) {
	f.calls.Add(1)
	f.mu.RLock()
	resource, found := f.resources[id]
	failure, hook := f.failure, f.hook
	f.mu.RUnlock()
	if hook != nil {
		hook(ctx, video, id)
	}
	if failure != nil {
		return r18.Resource{}, failure
	}
	if !found || video.ID != f.video.ID {
		return r18.Resource{}, sql.ErrNoRows
	}
	return resource, nil
}

type r18PlaybackCloudFixture struct {
	mu       sync.RWMutex
	files    map[string]cloud115.File
	remotes  map[string]cloud115.RemoteTask
	infoHook func(context.Context, string) (cloud115.File, error, bool)
	addHook  func()
	hold     atomic.Bool
	creates  atomic.Int32
	polls    atomic.Int32
	legacy   atomic.Int32
	infos    atomic.Int32
	urlCalls atomic.Int32
}

var _ clouddrive.CacheProvider = (*r18PlaybackCloudFixture)(nil)
var _ clouddrive.TaskIDProvider = (*r18PlaybackCloudFixture)(nil)

func newR18PlaybackCloudFixture() *r18PlaybackCloudFixture {
	return &r18PlaybackCloudFixture{
		files:   map[string]cloud115.File{"12": {ID: "12", ParentID: "0", Name: cloud115.CacheFolderName, Directory: true}},
		remotes: map[string]cloud115.RemoteTask{},
	}
}

func (*r18PlaybackCloudFixture) Check(context.Context, string) (string, error) {
	return "shared-account", nil
}

func (f *r18PlaybackCloudFixture) Find(context.Context, string) (*cloud115.RemoteTask, error) {
	f.legacy.Add(1)
	return nil, errors.New("测试禁止查询磁力历史")
}

func (f *r18PlaybackCloudFixture) Add(context.Context, string, string) error {
	f.legacy.Add(1)
	return errors.New("测试必须直接添加带任务身份的磁力")
}

func (f *r18PlaybackCloudFixture) Mkdir(_ context.Context, parent, name string) (string, error) {
	if parent != "12" || !strings.HasPrefix(name, "emby-go-") {
		return "", errors.New("测试缓存目录归属错误")
	}
	f.mu.Lock()
	f.files[name] = cloud115.File{ID: name, ParentID: parent, Name: name, Directory: true}
	f.mu.Unlock()
	return name, nil
}

func (f *r18PlaybackCloudFixture) AddTask(_ context.Context, magnet, folder string) (string, error) {
	_, hash, err := cloud115.NormalizeMagnet(magnet)
	if err != nil || !strings.HasPrefix(folder, "emby-go-") {
		return "", errors.New("测试磁力提交身份错误")
	}
	id, result := "task-"+hash, "result-"+hash
	f.mu.Lock()
	f.files[result] = cloud115.File{ID: result, ParentID: folder, Name: "正片.mkv", Size: 1000, PickCode: "fixture-code", SHA1: "fixture-sha"}
	f.remotes[id] = cloud115.RemoteTask{Hash: hash, RemoteID: id, FolderID: folder, ResultID: result, Status: 2, Progress: 100}
	hook := f.addHook
	f.mu.Unlock()
	f.creates.Add(1)
	if hook != nil {
		hook()
	}
	return id, nil
}

func (f *r18PlaybackCloudFixture) FindTask(_ context.Context, hash, id string) (*cloud115.RemoteTask, error) {
	f.polls.Add(1)
	f.mu.RLock()
	remote, found := f.remotes[id]
	f.mu.RUnlock()
	if !found || remote.Hash != hash {
		return nil, errors.New("测试查询了其它任务")
	}
	if f.hold.Load() {
		remote.Status, remote.Progress, remote.ResultID = 1, 20, ""
	}
	return &remote, nil
}

func (f *r18PlaybackCloudFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	f.infos.Add(1)
	f.mu.RLock()
	file, found := f.files[id]
	hook := f.infoHook
	f.mu.RUnlock()
	if hook != nil {
		if replacement, err, handled := hook(ctx, id); handled {
			return replacement, err
		}
	}
	if !found {
		return cloud115.File{}, &cloud115.Error{Message: "测试文件不存在", Missing: true}
	}
	return file, nil
}

func (f *r18PlaybackCloudFixture) List(_ context.Context, parent string, _ int) ([]cloud115.File, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	files := []cloud115.File{}
	for _, file := range f.files {
		if file.ParentID == parent {
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	return files, false, nil
}

func (f *r18PlaybackCloudFixture) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, id)
	return nil
}

func (f *r18PlaybackCloudFixture) RemoveTask(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.remotes, "task-"+hash)
	return nil
}

func (f *r18PlaybackCloudFixture) URL(context.Context, string, string) (string, error) {
	f.urlCalls.Add(1)
	return "https://guangya.invalid/fixture-video", nil
}

type r18MagnetPlaybackFixture struct {
	h       *guangyaServerHarness
	cloud   *r18PlaybackCloudFixture
	catalog *r18PlaybackCatalogFixture
	user    store.User
	app     *App
}

func newR18MagnetPlaybackFixture(t *testing.T) *r18MagnetPlaybackFixture {
	t.Helper()
	h := newGuangyaServerHarness(t)
	cloud := newR18PlaybackCloudFixture()
	catalog := &r18PlaybackCatalogFixture{fakeR18: &fakeR18{video: h.video}, resources: map[string]r18.Resource{}}
	for _, hash := range []string{r18PlaybackHashA, r18PlaybackHashB} {
		catalog.resources["btih-"+hash] = r18.Resource{ID: "btih-" + hash, Number: h.video.Number, Title: "测试磁力", Magnet: "magnet:?xt=urn:btih:" + hash}
	}
	h.a.r18Open = func(r18.Config) (r18Source, error) { return catalog, nil }
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider { return cloud }
	h.configure(t)
	user, _ := h.reader(t, "magnet-playback-reader")
	return &r18MagnetPlaybackFixture{h: h, cloud: cloud, catalog: catalog, user: user, app: &App{appState: h.a.appState, db: h.a.db.ForUser(user.ID)}}
}

func (f *r18MagnetPlaybackFixture) startQueue(t *testing.T) {
	t.Helper()
	f.h.a.guangyaOffline = offline.NewForProvider(f.h.a.rootCtx, f.h.a.db, f.h.a.guangyaProvider, "光鸭", func() (string, error) {
		connection, err := f.h.a.db.CloudGuangyaConnection()
		return store.CloudGuangyaAccount(connection.AccountID), err
	})
	t.Cleanup(f.h.a.guangyaOffline.Close)
}

type r18MagnetPlaybackResult struct {
	context  *gin.Context
	recorder *httptest.ResponseRecorder
	provider cloud115.Provider
	file     cloud115.File
	ok       bool
}

func (f *r18MagnetPlaybackFixture) invoke(ctx context.Context, method, source string) r18MagnetPlaybackResult {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/Videos/"+r18ItemID(f.h.library.ID, f.h.video.ID)+"/stream", nil).WithContext(ctx)
	c.Params = gin.Params{{Key: "id", Value: r18ItemID(f.h.library.ID, f.h.video.ID)}}
	c.Set("accountUser", f.user)
	p, file, ok := f.app.prepareR18MagnetFile(c, f.h.library, f.h.video, source)
	return r18MagnetPlaybackResult{context: c, recorder: recorder, provider: p, file: file, ok: ok}
}

func (f *r18MagnetPlaybackFixture) seed(t *testing.T, hash, status string, files []cloud115.File, selected string, makeDefault bool) store.OfflineTask {
	t.Helper()
	connection, err := f.h.a.offlineConnection("guangya")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.h.a.db.CreateOfflineTask(connection.AccountID, connection.FolderID, "magnet:?xt=urn:btih:"+hash, hash, f.h.library, f.h.video.ID, "btih-"+hash)
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.ExpiresAt, task.CacheFolderID = status, time.Now().Add(time.Hour).Unix(), "emby-go-"+task.ID
	task.RemoteTaskID, task.ResultID = "task-"+hash, "result-"+hash
	for index := range files {
		files[index].ParentID = task.CacheFolderID
	}
	task.Files = files
	f.cloud.mu.Lock()
	f.cloud.files[task.CacheFolderID] = cloud115.File{ID: task.CacheFolderID, ParentID: task.FolderID, Name: task.CacheFolderID, Directory: true}
	for _, file := range files {
		f.cloud.files[file.ID] = file
	}
	f.cloud.mu.Unlock()
	if err := f.h.a.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if selected != "" {
		if err := f.h.a.db.SelectOfflineFile(f.h.library, f.h.video.ID, task.ID, selected, makeDefault); err != nil {
			t.Fatal(err)
		}
	}
	task, err = f.h.a.db.OfflineTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func r18PlaybackTestFile(id, name string, size int64) cloud115.File {
	return cloud115.File{ID: id, Name: name, Size: size, PickCode: "fixture-" + id, SHA1: "fixture-sha-" + id}
}

func assertR18MagnetPlaybackFailure(t *testing.T, result r18MagnetPlaybackResult, status int) {
	t.Helper()
	if result.ok || result.recorder.Code != status || result.recorder.Body.Len() == 0 || !strings.Contains(result.recorder.Body.String(), "ErrorCode") {
		t.Fatalf("拒绝播放未返回预期错误：ok=%t status=%d body=%s", result.ok, result.recorder.Code, result.recorder.Body.String())
	}
	if status == http.StatusServiceUnavailable && result.recorder.Header().Get("Retry-After") == "" {
		t.Fatal("准备中的版本缺少重试提示")
	}
}

func TestR18MagnetPlaybackReaderCreatesOnlySelectedHashAndReusesReady(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	other := f.seed(t, r18PlaybackHashB, "ready", []cloud115.File{r18PlaybackTestFile("other", "其它磁力.mkv", 10)}, "other", true)
	legacy, err := f.h.a.db.CreateOfflineTask("shared-account", "12", "magnet:?xt=urn:btih:"+r18PlaybackHashA, r18PlaybackHashA, f.h.library, f.h.video.ID, "btih-"+r18PlaybackHashA)
	if err != nil {
		t.Fatal(err)
	}
	f.startQueue(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
	if !result.ok || result.provider != f.cloud || result.file.ID != "result-"+r18PlaybackHashA || f.cloud.creates.Load() != 1 || f.cloud.legacy.Load() != 0 {
		t.Fatal("普通用户未能仅直接准备所选光鸭磁力", result.ok, result.recorder.Body.String(), f.cloud.creates.Load())
	}
	if !f.app.validateR18MagnetPlayback(result.context) {
		t.Fatal("已就绪版本的最终核验失败")
	}
	bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, store.CloudGuangyaAccount("shared-account"))
	if err != nil || len(bindings) != 2 {
		t.Fatal("所选磁力未形成独立绑定", bindings, err)
	}
	for _, binding := range bindings {
		if binding.Task.ID == other.ID && (binding.FileID != "other" || !binding.Default || binding.Task.Version != other.Version) {
			t.Fatal("播放所选磁力改动了其它版本", binding)
		}
		if binding.Task.InfoHash == r18PlaybackHashA && binding.Default {
			t.Fatal("原生准备改动了全局默认版本")
		}
	}
	unchanged, err := f.h.a.db.OfflineTask(legacy.ID)
	if err != nil || unchanged.Status != "queued" || unchanged.Version != legacy.Version {
		t.Fatal("光鸭原生播放触发了 115 队列", unchanged, err)
	}
	before := f.catalog.calls.Load()
	f.catalog.mu.Lock()
	f.catalog.failure = sql.ErrNoRows
	f.catalog.mu.Unlock()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		reused := f.invoke(ctx, method, r18MagnetSourceID(r18PlaybackHashA))
		if !reused.ok || reused.file.ID != result.file.ID || f.cloud.creates.Load() != 1 || f.catalog.calls.Load() != before {
			t.Fatal("精确就绪缓存因上游下架不能复用或再次下载", method, reused.recorder.Body.String())
		}
	}
}

func TestR18MagnetPlaybackConcurrentRequestsSubmitOneTask(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	f.startQueue(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan r18MagnetPlaybackResult, 12)
	var workers sync.WaitGroup
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	for result := range results {
		if !result.ok || result.file.ID != "result-"+r18PlaybackHashA || !f.app.validateR18MagnetPlayback(result.context) {
			t.Fatal("并发播放相互破坏了同一版本", result.recorder.Code, result.recorder.Body.String())
		}
	}
	bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, store.CloudGuangyaAccount("shared-account"))
	if err != nil || len(bindings) != 1 || f.cloud.creates.Load() != 1 || f.cloud.legacy.Load() != 0 {
		t.Fatal("并发点击重复提交或绑定任务", len(bindings), f.cloud.creates.Load(), err)
	}
}

func TestR18MagnetPlaybackConcurrentSelectionKeepsDeterministicFileAndManualDefault(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	files := []cloud115.File{
		r18PlaybackTestFile("small", "小文件.mkv", 10),
		r18PlaybackTestFile("tie-b", "B.mkv", 200),
		r18PlaybackTestFile("tie-a", "A.mkv", 200),
		r18PlaybackTestFile("sample", "sample.mkv", 99999),
	}
	task := f.seed(t, r18PlaybackHashA, "waiting_selection", files, "", false)
	other := f.seed(t, r18PlaybackHashB, "ready", []cloud115.File{r18PlaybackTestFile("other", "另一个版本.mkv", 10)}, "other", true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan r18MagnetPlaybackResult, 12)
	var workers sync.WaitGroup
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
		}()
	}
	workers.Wait()
	close(results)
	for result := range results {
		if !result.ok || result.file.ID != "tie-a" || !f.app.validateR18MagnetPlayback(result.context) {
			t.Fatal("自动选择不稳定或同文件版本递增破坏了播放", result.recorder.Body.String())
		}
	}
	current, err := f.app.db.OfflineTask(task.ID)
	if err != nil || current.Version != task.Version+1 || current.Status != "ready" || f.cloud.creates.Load() != 0 {
		t.Fatal("并发自动选择重复推进任务或创建下载", current, err)
	}
	bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, task.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if binding.Task.ID == other.ID && (!binding.Default || binding.FileID != "other") || binding.Task.ID == task.ID && (binding.Default || binding.FileID != "tie-a") {
			t.Fatal("自动选文件改动了人工默认版本", binding)
		}
	}
}

func TestR18MagnetPlaybackRespectsManualSelectionAndHarmlessVersionChanges(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	task := f.seed(t, r18PlaybackHashA, "waiting_selection", []cloud115.File{r18PlaybackTestFile("small", "小文件.mkv", 10), r18PlaybackTestFile("large", "大文件.mkv", 100)}, "", false)
	f.catalog.hook = func(context.Context, r18.Video, string) {
		if err := f.app.db.SelectOfflineFile(f.h.library, f.h.video.ID, task.ID, "small", true); err != nil {
			t.Fatal(err)
		}
	}
	var bumped atomic.Bool
	f.cloud.infoHook = func(_ context.Context, id string) (cloud115.File, error, bool) {
		if id == "small" && bumped.CompareAndSwap(false, true) {
			if err := f.app.db.SelectOfflineFile(f.h.library, f.h.video.ID, task.ID, "small", false); err != nil {
				return cloud115.File{}, err, true
			}
		}
		return cloud115.File{}, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
	if !result.ok || result.file.ID != "small" {
		t.Fatal("自动播放覆盖了人工选择或拒绝同文件版本递增", result.recorder.Body.String())
	}
	if err := f.app.db.SelectOfflineFile(f.h.library, f.h.video.ID, task.ID, "small", false); err != nil || !f.app.validateR18MagnetPlayback(result.context) {
		t.Fatal("直链解析期间同文件的无害版本递增被拒绝", err, result.recorder.Body.String())
	}
	if err := f.app.db.SelectOfflineFile(f.h.library, f.h.video.ID, task.ID, "large", false); err != nil {
		t.Fatal(err)
	}
	if f.app.validateR18MagnetPlayback(result.context) {
		t.Fatal("最终核验允许已被替换的选中文件")
	}
}

func TestR18MagnetPlaybackHEADDoesNotPrepareMissingOrUnreadyVersions(t *testing.T) {
	for _, status := range []string{"missing", "queued", "downloading", "waiting_selection"} {
		t.Run(status, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			if status != "missing" {
				f.seed(t, r18PlaybackHashA, status, []cloud115.File{r18PlaybackTestFile("one", "one.mkv", 100), r18PlaybackTestFile("two", "two.mkv", 200)}, "", false)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			infosBefore := f.cloud.infos.Load()
			assertR18MagnetPlaybackFailure(t, f.invoke(ctx, http.MethodHead, r18MagnetSourceID(r18PlaybackHashA)), http.StatusServiceUnavailable)
			bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, store.CloudGuangyaAccount("shared-account"))
			if err != nil || status == "missing" && len(bindings) != 0 || f.cloud.creates.Load() != 0 || f.catalog.calls.Load() != 0 || f.cloud.infos.Load() != infosBefore {
				t.Fatal("HEAD 预探创建任务、选文件或查询磁力", len(bindings), f.cloud.creates.Load(), f.catalog.calls.Load(), err)
			}
		})
	}
}

func TestR18MagnetPlaybackRejectsResourceIdentityAndOwnershipChanges(t *testing.T) {
	for _, change := range []string{"hash", "resource-id", "number", "removed", "upstream-error", "source", "forbidden", "disabled"} {
		t.Run(change, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			resource := f.catalog.resources["btih-"+r18PlaybackHashA]
			source, status := r18MagnetSourceID(r18PlaybackHashA), http.StatusNotFound
			switch change {
			case "hash":
				resource.Magnet = "magnet:?xt=urn:btih:" + r18PlaybackHashB
			case "resource-id":
				resource.ID = "btih-" + r18PlaybackHashB
			case "number":
				resource.Number = "OTHER-001"
			case "removed":
				f.catalog.failure = sql.ErrNoRows
			case "upstream-error":
				f.catalog.failure = errors.New("private-secret-access-token")
				status = http.StatusBadGateway
			case "source":
				source += "-forged"
			case "forbidden":
				if err := f.app.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if err := f.h.a.db.EditUser(f.user.ID, f.user.Name, "", true); err != nil {
					t.Fatal(err)
				}
				status = http.StatusUnauthorized
			}
			f.catalog.resources["btih-"+r18PlaybackHashA] = resource
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := f.invoke(ctx, http.MethodGet, source)
			assertR18MagnetPlaybackFailure(t, result, status)
			bindings, err := f.h.a.db.OfflineBindings(f.h.library, f.h.video.ID, store.CloudGuangyaAccount("shared-account"))
			if err != nil || len(bindings) != 0 || f.cloud.creates.Load() != 0 || strings.Contains(result.recorder.Body.String(), "private-secret") {
				t.Fatal("无效资源创建下载或错误泄露上游数据", bindings, err)
			}
		})
	}
}

func TestR18MagnetPlaybackFreshInfoFailuresDoNotResubmit(t *testing.T) {
	for _, change := range []string{"network", "auth-missing", "id", "parent", "sha", "name", "size", "directory", "pick-code"} {
		t.Run(change, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("file", "正片.mkv", 100)}, "", false)
			f.cloud.infoHook = func(_ context.Context, id string) (cloud115.File, error, bool) {
				if id != "file" {
					return cloud115.File{}, nil, false
				}
				file := task.Files[0]
				switch change {
				case "network":
					return cloud115.File{}, &cloud115.Error{Message: "private-secret", Retryable: true}, true
				case "auth-missing":
					return cloud115.File{}, &cloud115.Error{Message: "private-secret", Missing: true, Auth: true}, true
				case "id":
					file.ID = "foreign-file"
				case "parent":
					file.ParentID = "foreign-folder"
				case "sha":
					file.SHA1 = "foreign-sha"
				case "name":
					file.Name = "另一个文件.mkv"
				case "size":
					file.Size++
				case "directory":
					file.Directory = true
				case "pick-code":
					file.PickCode = ""
				}
				return file, nil, true
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
			assertR18MagnetPlaybackFailure(t, result, http.StatusBadGateway)
			current, err := f.app.db.OfflineTask(task.ID)
			if err != nil || current.Status != "ready" || current.Version != task.Version || f.cloud.creates.Load() != 0 || f.catalog.calls.Load() != 0 || strings.Contains(result.recorder.Body.String(), "private-secret") {
				t.Fatal("新鲜核验失败触发了下载或改动了任务", current, err)
			}
		})
	}
}

func TestR18MagnetPlaybackTimeoutAndCancellationKeepBackgroundTask(t *testing.T) {
	for _, stopped := range []string{"timeout", "cancel"} {
		t.Run(stopped, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			f.cloud.hold.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if stopped == "cancel" {
				f.cloud.addHook = cancel
			}
			f.startQueue(t)
			result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
			if stopped == "timeout" {
				assertR18MagnetPlaybackFailure(t, result, http.StatusServiceUnavailable)
			} else if result.ok || result.recorder.Body.Len() != 0 || !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("请求取消未及时停止等待", result.ok, result.recorder.Body.String())
			}
			bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, store.CloudGuangyaAccount("shared-account"))
			if err != nil || len(bindings) != 1 || bindings[0].Task.Status == "cancelled" {
				t.Fatal("停止 HTTP 等待取消了后台任务", bindings, err)
			}
			f.cloud.hold.Store(false)
			f.h.a.guangyaOffline.Wake()
			deadline := time.NewTimer(4 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				task, err := f.app.db.OfflineTask(bindings[0].Task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if task.Status == "ready" {
					break
				}
				select {
				case <-deadline.C:
					t.Fatal("HTTP 请求结束后后台未继续就绪", task.Status, task.Stage, task.Error)
				case <-ticker.C:
				}
			}
			if f.cloud.creates.Load() != 1 {
				t.Fatal("等待停止后后台重复添加磁力", f.cloud.creates.Load())
			}
		})
	}
}

func TestR18MagnetPlaybackStoppedAndExpiringTasksNeverAutomaticallyRetry(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "timed_out", "needs_auth", "cleanup_pending", "expired-cache"} {
		t.Run(status, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("file", "正片.mkv", 100)}, "", false)
			task.Status, task.Retryable = status, true
			want := http.StatusConflict
			if status == "needs_auth" || status == "cleanup_pending" || status == "expired-cache" {
				want = http.StatusServiceUnavailable
			}
			if status == "expired-cache" {
				task.Status, task.ExpiresAt = "ready", time.Now().Add(-time.Second).Unix()
			}
			if err := f.app.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.db.OfflineTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for index := 0; index < 2; index++ {
				assertR18MagnetPlaybackFailure(t, f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA)), want)
			}
			after, err := f.app.db.OfflineTask(task.ID)
			if err != nil || after.Version != current.Version || f.cloud.creates.Load() != 0 || f.catalog.calls.Load() != 0 {
				t.Fatal("终端任务被播放请求自动重试", after, err)
			}
		})
	}
}

func TestR18MagnetPlaybackExpiredTaskRestartsOnlyAfterValidResource(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	task := f.seed(t, r18PlaybackHashA, "expired", nil, "", false)
	f.catalog.failure = sql.ErrNoRows
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assertR18MagnetPlaybackFailure(t, f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA)), http.StatusNotFound)
	unchanged, err := f.app.db.OfflineTask(task.ID)
	if err != nil || unchanged.Status != "expired" || unchanged.Version != task.Version || f.cloud.creates.Load() != 0 {
		t.Fatal("下架磁力重建了过期任务", unchanged, err)
	}
	f.catalog.failure = nil
	f.startQueue(t)
	result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
	if !result.ok || f.cloud.creates.Load() != 1 {
		t.Fatal("已清理完成的有效磁力未重新准备", result.recorder.Body.String())
	}
	current, err := f.app.db.OfflineTask(task.ID)
	if err != nil || current.Status != "ready" || current.Version <= task.Version || current.ID != task.ID || current.ExpiresAt <= task.ExpiresAt {
		t.Fatal("重建任务没有更新缓存生命周期", current, err)
	}
}

func TestR18MagnetPlaybackMissingFileWaitsForCASRecoveryWithoutEarlyResponse(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("old-file", "正片.mkv", 100)}, "", false)
	var missing atomic.Bool
	f.cloud.infoHook = func(_ context.Context, id string) (cloud115.File, error, bool) {
		if id == "old-file" && missing.CompareAndSwap(false, true) {
			return cloud115.File{}, &cloud115.Error{Message: "测试文件已删除", Missing: true}, true
		}
		return cloud115.File{}, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			current, err := f.app.db.OfflineTask(task.ID)
			if err != nil {
				completed <- err
				return
			}
			if current.Status == "discovering" {
				if current.RecoveryFileID != "old-file" || len(current.Files) != 0 || current.CacheFolderID != task.CacheFolderID || current.ExpiresAt != task.ExpiresAt {
					completed <- errors.New("恢复没有保存所选文件和缓存归属")
					return
				}
				file := r18PlaybackTestFile("new-file", "恢复后的正片.mkv", 120)
				file.ParentID = task.CacheFolderID
				f.cloud.mu.Lock()
				f.cloud.files[file.ID] = file
				f.cloud.mu.Unlock()
				current.Status, current.RecoveryFileID, current.ResultID, current.Files = "ready", "", file.ID, []cloud115.File{file}
				completed <- f.app.db.UpdateOfflineTask(current, current.Version)
				return
			}
			select {
			case <-ctx.Done():
				completed <- ctx.Err()
				return
			case <-ticker.C:
			}
		}
	}()
	result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
	if err := <-completed; err != nil || !result.ok || result.file.ID != "new-file" || result.recorder.Body.Len() != 0 || f.cloud.creates.Load() != 0 || f.catalog.calls.Load() != 1 {
		t.Fatal("同次请求未等待所选文件恢复，或提前写出错误", result.ok, result.recorder.Body.String(), err)
	}
}

func TestR18MagnetPlaybackReadyRemovedResourceDoesNotRedownloadMissingFile(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("file", "正片.mkv", 100)}, "", false)
	f.catalog.failure = sql.ErrNoRows
	f.cloud.infoHook = func(context.Context, string) (cloud115.File, error, bool) {
		return cloud115.File{}, &cloud115.Error{Message: "测试文件已删除", Missing: true}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assertR18MagnetPlaybackFailure(t, f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA)), http.StatusNotFound)
	current, err := f.app.db.OfflineTask(task.ID)
	if err != nil || current.Status != "ready" || current.Version != task.Version || f.cloud.creates.Load() != 0 {
		t.Fatal("下架资源进入了新下载恢复", current, err)
	}
}

func TestR18MagnetPlaybackRejectsUnboundTaskFromOldCacheFolder(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	otherVideo := r18.EncodeID("another-video", "javdb")
	task, err := f.app.db.CreateOfflineTask(store.CloudGuangyaAccount("shared-account"), "old-folder", "magnet:?xt=urn:btih:"+r18PlaybackHashA, r18PlaybackHashA, f.h.library, otherVideo, "btih-"+r18PlaybackHashA)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assertR18MagnetPlaybackFailure(t, f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA)), http.StatusConflict)
	bindings, err := f.app.db.OfflineBindings(f.h.library, f.h.video.ID, task.AccountID)
	current, readErr := f.app.db.OfflineTask(task.ID)
	if err != nil || readErr != nil || len(bindings) != 0 || current.Version != task.Version || f.cloud.creates.Load() != 0 {
		t.Fatal("拒绝旧目录任务仍留下本影片错误绑定", bindings, current, err, readErr)
	}
}

func TestR18MagnetPlaybackRevokesLibraryAccessWhileWaiting(t *testing.T) {
	f := newR18MagnetPlaybackFixture(t)
	task := f.seed(t, r18PlaybackHashA, "downloading", nil, "", false)
	resourceRead := make(chan struct{})
	f.catalog.hook = func(context.Context, r18.Video, string) { close(resourceRead) }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan r18MagnetPlaybackResult, 1)
	go func() { result <- f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA)) }()
	<-resourceRead
	if err := f.app.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{}); err != nil {
		t.Fatal(err)
	}
	assertR18MagnetPlaybackFailure(t, <-result, http.StatusNotFound)
	current, err := f.app.db.OfflineTask(task.ID)
	if err != nil || current.Version != task.Version || current.Status != "downloading" || f.cloud.creates.Load() != 0 {
		t.Fatal("撤销播放权限改动或取消了共享后台任务", current, err)
	}
}

func TestR18MagnetPlaybackGuardsConfigurationAndAuthorizationDuringPreparationAndBeforeRedirect(t *testing.T) {
	for _, phase := range []string{"resource", "info", "redirect"} {
		for _, change := range []string{"permission", "disabled", "account", "folder", "revision", "library-revision", "task-folder", "cache-expired"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				f := newR18MagnetPlaybackFixture(t)
				task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("file", "正片.mkv", 100)}, "", false)
				if phase == "resource" {
					task.Status, task.Files = "queued", nil
					if err := f.app.db.UpdateOfflineTask(task, task.Version); err != nil {
						t.Fatal(err)
					}
				}
				mutate := func() error {
					switch change {
					case "permission":
						return f.app.db.SaveLibraryAccessPolicy(store.LibraryAccessPolicy{})
					case "disabled":
						return f.h.a.db.EditUser(f.user.ID, f.user.Name, "", true)
					case "library-revision":
						_, err := f.h.a.db.UpdateR18Connection(f.h.library.ID, f.h.library.Revision, f.h.library.Name, r18.Config{BaseURL: "https://avdb.invalid", APIKey: "fixture-new-key"})
						return err
					case "account", "folder", "revision":
						connection, err := f.h.a.db.CloudGuangyaConnection()
						if err != nil {
							return err
						}
						if change == "account" {
							connection.AccountID = "new-account"
						}
						if change == "folder" {
							connection.FolderID = "new-folder"
						}
						return f.h.a.db.SaveCloudGuangyaConnection(connection, connection.Revision)
					case "task-folder", "cache-expired":
						current, err := f.h.a.db.OfflineTask(task.ID)
						if err != nil {
							return err
						}
						if change == "task-folder" {
							current.FolderID = "foreign-folder"
						} else {
							current.ExpiresAt = time.Now().Add(-time.Second).Unix()
						}
						return f.h.a.db.UpdateOfflineTask(current, current.Version)
					}
					return fmt.Errorf("未知测试变更 %s", change)
				}
				var mutationErr error
				if phase == "resource" {
					f.catalog.hook = func(context.Context, r18.Video, string) { mutationErr = mutate() }
				}
				if phase == "info" {
					f.cloud.infoHook = func(_ context.Context, id string) (cloud115.File, error, bool) {
						if id == "file" {
							mutationErr = mutate()
						}
						return cloud115.File{}, nil, false
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				result := f.invoke(ctx, http.MethodGet, r18MagnetSourceID(r18PlaybackHashA))
				if phase == "redirect" {
					if !result.ok {
						t.Fatal("安全变更前准备失败", result.recorder.Body.String())
					}
					mutationErr = mutate()
					if f.app.validateR18MagnetPlayback(result.context) {
						t.Fatal("最终 302 核验未拒绝变更", change)
					}
				} else if result.ok {
					t.Fatal("准备期间安全变更仍返回播放文件", change)
				}
				if mutationErr != nil || f.cloud.creates.Load() != 0 || result.recorder.Body.Len() == 0 {
					t.Fatal("安全变更未被明确拒绝或触发了下载", mutationErr, result.recorder.Body.String())
				}
			})
		}
	}
}

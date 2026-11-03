package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/clouddrive"
	"emby-go/internal/offline"
	"emby-go/internal/store"
)

const fixture115DirectPickCode = "fixture-115-direct-pick-code"

// 该夹具实现生产 Cookie 连接的稳定任务身份能力，旧查询和提交入口禁止调用。
type direct115ServerFixture struct {
	*namedCloudFixture
	db                          *store.Store
	mu                          sync.RWMutex
	folder, hash                string
	boundary                    store.OfflineTask
	creates, polls, legacyCalls atomic.Int32
}

var _ clouddrive.StableTaskIDProvider = (*direct115ServerFixture)(nil)

func (f *direct115ServerFixture) Find(context.Context, string) (*cloud115.RemoteTask, error) {
	f.legacyCalls.Add(1)
	return nil, &clouddrive.Error{Message: "115 直接提交不能先查询历史任务"}
}

func (f *direct115ServerFixture) Add(context.Context, string, string) error {
	f.legacyCalls.Add(1)
	return &clouddrive.Error{Message: "115 必须使用稳定任务身份的直接提交入口"}
}

func (f *direct115ServerFixture) TaskIDForHash(value string) (string, error) {
	_, hash, err := cloud115.NormalizeMagnet("magnet:?xt=urn:btih:" + value)
	return hash, err
}

func (f *direct115ServerFixture) Mkdir(_ context.Context, parent, name string) (string, error) {
	if parent != "12" || !strings.HasPrefix(name, "emby-go-") {
		return "", &clouddrive.Error{Message: "115 任务缓存目录参数错误"}
	}
	return name, nil
}

func (f *direct115ServerFixture) AddTask(_ context.Context, magnet, folder string) (string, error) {
	_, hash, err := cloud115.NormalizeMagnet(magnet)
	if err != nil || !strings.HasPrefix(folder, "emby-go-") {
		return "", &clouddrive.Error{Message: "115 直接提交参数错误"}
	}
	if f.polls.Load() != 0 {
		return "", &clouddrive.Error{Message: "115 创建请求发出前查询了远端任务"}
	}
	// 模拟远端写入之前读取真实数据库，确认同次状态保存已包含稳定任务 ID。
	boundary, err := f.db.OfflineTask(strings.TrimPrefix(folder, "emby-go-"))
	if err != nil || boundary.Status != "submitting" || boundary.Resume != "submitting" || boundary.Stage != "submit" || boundary.RemoteTaskID != hash || boundary.InfoHash != hash || boundary.AccountID != f.account || boundary.FolderID != "12" || boundary.CacheFolderID != folder {
		return "", &clouddrive.Error{Message: "115 发出创建请求前未保存任务 ID、提交状态和缓存归属"}
	}
	f.mu.Lock()
	f.folder, f.hash, f.boundary = folder, hash, boundary
	f.mu.Unlock()
	f.creates.Add(1)
	return hash, nil
}

func (f *direct115ServerFixture) FindTask(_ context.Context, hash, taskID string) (*cloud115.RemoteTask, error) {
	f.polls.Add(1)
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.creates.Load() == 0 || hash != f.hash || taskID != f.hash {
		return nil, &clouddrive.Error{Message: "115 未按本次提交的稳定任务身份查询"}
	}
	return &cloud115.RemoteTask{Hash: hash, RemoteID: taskID, FolderID: f.folder, ResultID: "115-direct-video", Status: 2, Progress: 100}, nil
}

func (f *direct115ServerFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	if strings.HasPrefix(id, "emby-go-") {
		return cloud115.File{ID: id, ParentID: "12", Name: id, Directory: true}, nil
	}
	if id == "115-direct-video" {
		f.mu.RLock()
		defer f.mu.RUnlock()
		return cloud115.File{ID: id, ParentID: f.folder, Name: "正片.mp4", PickCode: fixture115DirectPickCode, Size: 100}, nil
	}
	return f.namedCloudFixture.Info(ctx, id)
}

func TestCloud115DirectSubmissionPublishesSourceAndReusesLocalReadyTask(t *testing.T) {
	h := newGuangyaServerHarness(t)
	provider := &direct115ServerFixture{namedCloudFixture: h.legacy, db: h.a.db}
	var cookieOnly atomic.Bool
	h.a.cloud115Open = func(tokens cloud115.Tokens, _ func(cloud115.Tokens) error) cloud115.Provider {
		cookieOnly.Store(tokens.Cookie != "" && tokens.Access == "" && tokens.Refresh == "")
		return provider
	}
	h.configure(t)
	// 相同影片和磁力先建立光鸭任务，115 队列不能读取或推进它。
	guangya := h.submit(t, "guangya")
	h.a.offlineService = offline.New(h.a.rootCtx, h.a.db, h.a.cloudProvider)
	t.Cleanup(h.a.offlineService.Close)
	task := h.submit(t, "115")
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for task.Status != "ready" {
		if task.Status == "failed" || task.Status == "needs_auth" || task.Status == "timed_out" {
			t.Fatalf("115 直接提交失败：status=%s stage=%s error=%s", task.Status, task.Stage, task.Error)
		}
		select {
		case <-deadline.C:
			t.Fatalf("115 直接提交未发布来源：status=%s stage=%s error=%s", task.Status, task.Stage, task.Error)
		case <-ticker.C:
			var err error
			task, err = h.a.db.OfflineTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	provider.mu.RLock()
	boundary := provider.boundary
	provider.mu.RUnlock()
	if !cookieOnly.Load() || boundary.RemoteTaskID != task.InfoHash || boundary.Status != "submitting" || boundary.Stage != "submit" || task.RemoteTaskID != task.InfoHash || task.Progress != 100 || provider.creates.Load() != 1 || provider.polls.Load() == 0 || provider.legacyCalls.Load() != 0 {
		t.Fatal("115 Cookie 直接提交、预存任务身份或精确轮询流程错误")
	}
	creates, polls := provider.creates.Load(), provider.polls.Load()
	again := h.submit(t, "115")
	if again.ID != task.ID || again.Status != "ready" || again.RemoteTaskID != task.InfoHash || provider.creates.Load() != creates || provider.polls.Load() != polls {
		t.Fatal("115 本地就绪资源再次提交或查询了远端任务")
	}
	_, response := h.call(t, http.MethodGet, h.base("115"), h.admin, nil, http.StatusOK)
	source := cloudResponseSource(t, response, "sources", "115-"+task.ID)
	if len(cloudResponseList(t, response, "sources")) != 1 || source["Provider"] != "115" || source["InfoHash"] != task.InfoHash {
		t.Fatal("115 下载结果未发布属于本次磁力的播放源")
	}
	// InfoHash 是公开磁力身份；仅内部字段和播放文件凭据不得进入页面 DTO。
	encoded, err := json.Marshal(response)
	if err != nil || bytes.Contains(encoded, []byte("remote_task_id")) || bytes.Contains(encoded, []byte("pick_code")) || bytes.Contains(encoded, []byte(fixture115DirectPickCode)) {
		t.Fatal("115 页面响应包含内部任务字段或文件凭据", err)
	}
	guangyaAfter, err := h.a.db.OfflineTask(guangya.ID)
	if err != nil || task.ID == guangya.ID || task.InfoHash != guangya.InfoHash || !reflect.DeepEqual(guangyaAfter, guangya) {
		t.Fatal("115 队列混用了光鸭任务或改变了其状态", err)
	}
	_, guangyaResponse := h.call(t, http.MethodGet, h.base("guangya"), h.admin, nil, http.StatusOK)
	if len(cloudResponseList(t, guangyaResponse, "sources")) != 0 || provider.legacyCalls.Load() != 0 || provider.polls.Load() != polls {
		t.Fatal("光鸭页面混入了 115 播放源，或本地状态读取触发了远端查询")
	}
}

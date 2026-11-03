package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/clouddrive"
	"emby-go/internal/cloudguangya"
	"emby-go/internal/offline"
	"emby-go/internal/store"
)

const fixtureGuangyaRemoteTaskID = "fixture-remote-task"

type directGuangyaServerFixture struct {
	*namedCloudFixture
	mu                          sync.RWMutex
	folder                      string
	creates, polls, legacyCalls atomic.Int32
}

var _ clouddrive.TaskIDProvider = (*directGuangyaServerFixture)(nil)

func (f *directGuangyaServerFixture) Find(context.Context, string) (*cloud115.RemoteTask, error) {
	f.legacyCalls.Add(1)
	return nil, errors.New("禁止按磁力查询历史任务")
}

func (f *directGuangyaServerFixture) Add(context.Context, string, string) error {
	f.legacyCalls.Add(1)
	return errors.New("必须使用返回任务 ID 的直接提交")
}

func (f *directGuangyaServerFixture) Mkdir(_ context.Context, parent, name string) (string, error) {
	if parent != "12" || !strings.HasPrefix(name, "emby-go-") {
		return "", errors.New("任务缓存目录参数错误")
	}
	return name, nil
}

func (f *directGuangyaServerFixture) AddTask(_ context.Context, magnet, folder string) (string, error) {
	if _, _, err := cloud115.NormalizeMagnet(magnet); err != nil || !strings.HasPrefix(folder, "emby-go-") {
		return "", errors.New("直接提交参数错误")
	}
	f.mu.Lock()
	f.folder = folder
	f.mu.Unlock()
	f.creates.Add(1)
	return fixtureGuangyaRemoteTaskID, nil
}

func (f *directGuangyaServerFixture) FindTask(_ context.Context, hash, taskID string) (*cloud115.RemoteTask, error) {
	f.polls.Add(1)
	if taskID != fixtureGuangyaRemoteTaskID {
		return nil, errors.New("轮询未使用本次提交的任务 ID")
	}
	f.mu.RLock()
	folder := f.folder
	f.mu.RUnlock()
	return &cloud115.RemoteTask{Hash: hash, RemoteID: taskID, FolderID: folder, ResultID: "guangya-video", Status: 2, Progress: 100}, nil
}

func (f *directGuangyaServerFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	if strings.HasPrefix(id, "emby-go-") {
		return cloud115.File{ID: id, ParentID: "12", Name: id, Directory: true}, nil
	}
	if id == "guangya-video" {
		f.mu.RLock()
		defer f.mu.RUnlock()
		return cloud115.File{ID: id, ParentID: f.folder, Name: "正片.mp4", PickCode: "fixture-video-code", Size: 100}, nil
	}
	return f.namedCloudFixture.Info(ctx, id)
}

func TestCloudGuangyaDirectSubmissionPublishesSourceAndReusesLocalReadyTask(t *testing.T) {
	h := newGuangyaServerHarness(t)
	provider := &directGuangyaServerFixture{namedCloudFixture: h.guangya}
	h.a.guangyaOpen = func(cloudguangya.Tokens, func(cloudguangya.Tokens) error) cloud115.Provider { return provider }
	h.configure(t)
	h.a.guangyaOffline = offline.NewForProvider(h.a.rootCtx, h.a.db, h.a.guangyaProvider, "光鸭", func() (string, error) {
		connection, err := h.a.db.CloudGuangyaConnection()
		return store.CloudGuangyaAccount(connection.AccountID), err
	})
	t.Cleanup(h.a.guangyaOffline.Close)
	task := h.submit(t, "guangya")
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for task.Status != "ready" {
		select {
		case <-deadline.C:
			t.Fatalf("直接提交未发布可播放来源：status=%s stage=%s error=%s", task.Status, task.Stage, task.Error)
		case <-ticker.C:
			var err error
			task, err = h.a.db.OfflineTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if task.RemoteTaskID != fixtureGuangyaRemoteTaskID || provider.creates.Load() != 1 || provider.polls.Load() == 0 || provider.legacyCalls.Load() != 0 {
		t.Fatal("直接提交、任务 ID 保存或精确轮询流程错误", task.RemoteTaskID)
	}
	again := h.submit(t, "guangya")
	if again.ID != task.ID || again.Status != "ready" || provider.creates.Load() != 1 {
		t.Fatal("已有本地播放资源再次提交了远端下载")
	}
	_, response := h.call(t, http.MethodGet, h.base("guangya"), h.admin, nil, http.StatusOK)
	if len(cloudResponseList(t, response, "sources")) != 1 || cloudResponseSource(t, response, "sources", "guangya-"+task.ID)["Provider"] != "guangya" {
		t.Fatal("直接下载结果未形成光鸭播放来源")
	}
	encoded, err := json.Marshal(response)
	if err != nil || bytes.Contains(encoded, []byte(fixtureGuangyaRemoteTaskID)) || bytes.Contains(encoded, []byte("remote_task_id")) {
		t.Fatal("内部远程任务身份泄露到页面响应", err)
	}
}

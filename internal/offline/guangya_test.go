package offline

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

type guangyaFixture struct{ *cacheFixture }

func (*guangyaFixture) PendingTaskFolderUnknown() bool { return true }

func newGuangyaTask(t *testing.T, s *store.Store, library store.Library, video string) store.OfflineTask {
	t.Helper()
	hash := strings.Repeat("a", 40)
	task, err := s.CreateOfflineTask(store.CloudGuangyaAccount("account"), "work", "magnet:?xt=urn:btih:"+hash, hash, library, video, "1")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func guangyaService(s *store.Store, p clouddrive.Provider) *Service {
	account := store.CloudGuangyaAccount("account")
	return &Service{db: s, ctx: context.Background(), name: "光鸭", wake: make(chan struct{}, 1), provider: func() (clouddrive.Provider, string, error) { return p, account, nil }, configuredAccount: func() (string, error) { return account, nil }}
}

func TestGuangyaQueueAndConfigurationFailuresStayWithinAccount(t *testing.T) {
	s, library, video := setup(t)
	legacy := create(t, s, library, video)
	task := newGuangyaTask(t, s, library, video)
	if err := s.SaveCloud115Connection(store.Cloud115Connection{AccountID: legacy.AccountID, FolderID: "work", Secrets: []byte("fixture")}, 0); err != nil {
		t.Fatal(err)
	}
	p := &guangyaFixture{newCacheFixture()}
	service := guangyaService(s, p)
	service.tick()
	task = read(t, s, task.ID)
	if task.Status != "downloading" || p.adds != 1 || read(t, s, legacy.ID).Status != "queued" {
		t.Fatal("光鸭队列处理了错误账号或未提交任务", task.Status, p.adds)
	}
	if err := service.Cancel(legacy.ID, legacy.Version); err == nil {
		t.Fatal("光鸭服务允许取消 115 任务")
	}
	if err := service.Retry(legacy.ID, legacy.Version); err == nil {
		t.Fatal("光鸭服务允许重试 115 任务")
	}
	task.NextPoll = 0
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	service.provider = func() (clouddrive.Provider, string, error) { return nil, "", errors.New("fixture unavailable") }
	service.tick()
	task = read(t, s, task.ID)
	if task.Status != "needs_auth" || !strings.Contains(task.Error, "光鸭") || strings.Contains(task.Error, "115") || read(t, s, legacy.ID).Status != "queued" {
		t.Fatal("光鸭凭据失败回退到了 115 配置", task.Status, task.Error)
	}
}

func TestGuangyaUnknownPendingFolderWaitsWithoutResubmitting(t *testing.T) {
	for _, remoteStatus := range []int{0, 1, -1} {
		t.Run(strconv.Itoa(remoteStatus), func(t *testing.T) {
			s, library, video := setup(t)
			task := newGuangyaTask(t, s, library, video)
			p := &guangyaFixture{newCacheFixture()}
			service := guangyaService(s, p)
			service.process(p, task)
			task = read(t, s, task.ID)
			p.remote = &clouddrive.RemoteTask{Status: remoteStatus, Progress: 42}
			service.process(p, task)
			task = read(t, s, task.ID)
			if p.adds != 1 || task.Stage == "cache" || len(task.Files) != 0 {
				t.Fatal("未知保存目录触发缓存失败、重复提交或发布播放源", task.Status, p.adds)
			}
			if remoteStatus == -1 {
				if task.Status != "failed" || task.Stage != "remote" || !strings.Contains(task.Error, "光鸭") {
					t.Fatal("失败任务状态未正确保留", task.Status, task.Error)
				}
			} else {
				if task.Status != "downloading" || task.Progress != 42 {
					t.Fatal("未继续等待光鸭任务", task.Status, task.Progress)
				}
				service.process(p, task)
				if p.adds != 1 {
					t.Fatal("等待状态重复提交磁力")
				}
			}
		})
	}
	// 115 未声明此能力，继续执行原有的严格保存目录核验。
	s, library, video := setup(t)
	task := create(t, s, library, video)
	p := newCacheFixture()
	service := &Service{db: s, ctx: context.Background()}
	service.process(p, task)
	p.remote = &clouddrive.RemoteTask{Status: 1, Progress: 42}
	service.process(p, read(t, s, task.ID))
	if result := read(t, s, task.ID); result.Stage != "cache" || result.Status != "failed" {
		t.Fatal("光鸭能力弱化了 115 归属核验", result.Status, result.Stage)
	}
}

func TestGuangyaCompletedResultStillRequiresCacheOwnership(t *testing.T) {
	for _, change := range []string{"unknown-folder", "external-folder", "external-result", "valid"} {
		t.Run(change, func(t *testing.T) {
			s, library, video := setup(t)
			task := newGuangyaTask(t, s, library, video)
			p := &guangyaFixture{newCacheFixture()}
			service := guangyaService(s, p)
			service.process(p, task)
			p.remote = &clouddrive.RemoteTask{Status: 2, Progress: 100, ResultID: "result", FolderID: "cache"}
			p.entries["result"] = clouddrive.File{ID: "result", ParentID: "cache", Name: "movie.mp4", PickCode: "fixture", Size: 100}
			switch change {
			case "unknown-folder":
				p.remote.FolderID = ""
			case "external-folder":
				p.remote.FolderID = "external"
			case "external-result":
				file := p.entries["result"]
				file.ParentID = "external"
				p.entries["result"] = file
				p.entries["external"] = clouddrive.File{ID: "external", ParentID: "0", Name: "permanent", Directory: true}
			}
			service.process(p, read(t, s, task.ID))
			result := read(t, s, task.ID)
			if change == "valid" {
				if result.Status != "ready" || len(result.Files) != 1 {
					t.Fatal("正确归属的光鸭结果未就绪", result.Status)
				}
			} else if result.Stage != "cache" || len(result.Files) != 0 || result.Status != "failed" {
				t.Fatal("完成任务绕过缓存归属核验", result.Status, result.Stage)
			}
			if p.adds != 1 || p.removed != 0 || len(p.deleted) != 0 {
				t.Fatal("结果核验意外修改了远程任务", p.adds, p.removed, p.deleted)
			}
		})
	}
}

func TestGuangyaCleanupUnknownPendingTaskAndMovedCacheStaySafe(t *testing.T) {
	for _, change := range []string{"unknown-task", "moved", "renamed", "authentication", "valid"} {
		t.Run(change, func(t *testing.T) {
			s, library, video := setup(t)
			task := newGuangyaTask(t, s, library, video)
			p := &guangyaFixture{newCacheFixture()}
			service := guangyaService(s, p)
			service.process(p, task)
			task = read(t, s, task.ID)
			task.ExpiresAt = time.Now().Add(-time.Second).Unix()
			if err := s.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p.remote = &clouddrive.RemoteTask{Status: 1, FolderID: "cache"}
			switch change {
			case "unknown-task":
				p.remote.FolderID = ""
			case "moved":
				child := p.entries["cache"]
				child.ParentID = "external"
				p.entries["cache"] = child
			case "renamed":
				root := p.entries["work"]
				root.Name = "permanent"
				p.entries["work"] = root
			case "authentication":
				p.infoErrors = map[string]error{"cache": &clouddrive.Error{Message: "光鸭鉴权失败", Auth: true, Missing: true}}
			}
			service.tickCleanup()
			result := read(t, s, task.ID)
			if change == "valid" {
				if result.Status != "expired" || len(p.deleted) != 1 || p.deleted[0] != "cache" || p.removed != 1 {
					t.Fatal("经核验的光鸭缓存未清理", result.Status, p.deleted, p.removed)
				}
			} else if result.Status != "cleanup_pending" || p.removed != 0 || len(p.deleted) != 0 || result.NextPoll <= time.Now().Unix() {
				t.Fatal("清理未确认归属的目录或任务", result.Status, p.deleted, p.removed)
			}
			if change == "unknown-task" {
				if !strings.Contains(result.Error, "光鸭") {
					t.Fatal("清理错误未标明光鸭", result.Error)
				}
				p.remote.FolderID = "cache"
				result.NextPoll = 0
				if err := s.UpdateOfflineTask(result, result.Version); err != nil {
					t.Fatal(err)
				}
				service.tickCleanup()
				if read(t, s, task.ID).Status != "expired" || p.removed != 1 || len(p.deleted) != 1 {
					t.Fatal("归属明确后未继续清理", p.removed, p.deleted)
				}
			}
		})
	}
}

func TestNewForProviderKeepsConfiguredAccountAndStopsWorkers(t *testing.T) {
	s, _, _ := setup(t)
	account := store.CloudGuangyaAccount("account")
	service := NewForProvider(context.Background(), s, func() (clouddrive.Provider, string, error) { return nil, "", errors.New("fixture unavailable") }, "光鸭", func() (string, error) { return account, nil })
	defer service.Close()
	configured, err := service.configuredAccountID()
	if err != nil || configured != account || service.brand() != "光鸭" {
		t.Fatal("独立服务丢失名称或账号命名空间", configured, err)
	}
}

func TestGuangyaPreCreateFailureCanRetryButUncertainSubmissionCannot(t *testing.T) {
	for _, scenario := range []string{"resolve-network", "resolve-auth", "uncertain-create"} {
		t.Run(scenario, func(t *testing.T) {
			s, library, video := setup(t)
			task := newGuangyaTask(t, s, library, video)
			p := &guangyaFixture{newCacheFixture()}
			p.addError = &clouddrive.Error{Message: "光鸭解析失败", Retryable: true, NotSubmitted: true}
			if scenario == "resolve-auth" {
				p.addError = &clouddrive.Error{Message: "光鸭解析鉴权失败", Auth: true, NotSubmitted: true}
			} else if scenario == "uncertain-create" {
				p.addError = &clouddrive.Error{Message: "光鸭创建响应超时", Retryable: true}
			}
			service := guangyaService(s, p)
			service.process(p, task)
			task = read(t, s, task.ID)
			if scenario == "uncertain-create" {
				if task.Status != "submitting" || task.Resume != "submitting" {
					t.Fatal("不明确创建失败丢失了提交边界", task.Status, task.Resume)
				}
			} else if task.Resume != "queued" || scenario == "resolve-network" && task.Status != "queued" || scenario == "resolve-auth" && task.Status != "needs_auth" {
				t.Fatal("确定未创建的失败无法重新进入提交队列", task.Status, task.Resume)
			}
			if scenario == "resolve-auth" {
				if err := service.Retry(task.ID, task.Version); err != nil {
					t.Fatal("更新凭据后无法重试解析", err)
				}
				task = read(t, s, task.ID)
			}
			p.addError = nil
			service.process(p, task)
			task = read(t, s, task.ID)
			if scenario == "uncertain-create" {
				if p.adds != 1 || task.Status != "submitting" {
					t.Fatal("不明确创建结果被重复提交", task.Status, p.adds)
				}
			} else if p.adds != 2 || task.Status != "downloading" {
				t.Fatal("确定未创建的磁力未成功重试", task.Status, p.adds)
			}
		})
	}
}

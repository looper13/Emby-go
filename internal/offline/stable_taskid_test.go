package offline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

// 115 以磁力 hash 定位远端任务；所有旧查询和旧提交入口仍视为测试错误。
type stableTaskFixture struct {
	*taskIDFixture
	localIDs   atomic.Int32
	beforeFind func()
}

var _ clouddrive.StableTaskIDProvider = (*stableTaskFixture)(nil)

func newStableTaskFixture() *stableTaskFixture {
	p := &stableTaskFixture{taskIDFixture: newTaskIDFixture()}
	p.nextID = strings.Repeat("a", 40)
	return p
}

func (p *stableTaskFixture) TaskIDForHash(hash string) (string, error) {
	p.localIDs.Add(1)
	_, normalized, err := clouddrive.NormalizeMagnet("magnet:?xt=urn:btih:" + hash)
	if err != nil || len(hash) != 40 {
		return "", errors.New("测试 hash 格式不正确")
	}
	return normalized, nil
}

func (p *stableTaskFixture) FindTask(ctx context.Context, hash, id string) (*clouddrive.RemoteTask, error) {
	if p.beforeFind != nil {
		p.beforeFind()
	}
	return p.taskIDFixture.FindTask(ctx, hash, id)
}

func stableService(db *store.Store, p *stableTaskFixture) *Service {
	return &Service{db: db, ctx: context.Background(), name: "115", wake: make(chan struct{}, 1),
		provider:          func() (clouddrive.Provider, string, error) { return p, "account", nil },
		configuredAccount: func() (string, error) { return "account", nil },
	}
}

func storedStableTask(t *testing.T, status string) (*Service, *stableTaskFixture, store.OfflineTask) {
	t.Helper()
	db, library, video := setup(t)
	task := create(t, db, library, video)
	task.Status, task.CacheFolderID, task.ExpiresAt = status, "cache", time.Now().Add(time.Hour).Unix()
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	p := newStableTaskFixture()
	p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
	return stableService(db, p), p, read(t, db, task.ID)
}

func startStableProcess(t *testing.T, s *Service, p *stableTaskFixture, task store.OfflineTask) <-chan struct{} {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.process(p, task)
	}()
	t.Cleanup(func() {
		cancel()
		p.cacheGate.unblock()
		p.addGate.unblock()
		p.pollGate.unblock()
		waitStageProcess(t, done)
	})
	return done
}

func TestStableTaskIDQueuedAlwaysAddsWithIdentitySavedBeforeRequest(t *testing.T) {
	for _, oldIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "old-version3"}[oldIntent], func(t *testing.T) {
			db, library, video := setup(t)
			task := create(t, db, library, video)
			p := newStableTaskFixture()
			if oldIntent {
				task.ExpiresAt = time.Now().Add(time.Hour).Unix()
				if err := db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, db, task.ID)
				task.CacheFolderID = "cache"
				if err := db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, db, task.ID)
				p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
				if task.Version != 3 {
					t.Fatal("未构造旧排队任务")
				}
			}
			p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 2, FolderID: "foreign", ResultID: "foreign-result"}
			var boundaryVersion int64
			p.beforeAdd = func() {
				boundary := read(t, db, task.ID)
				boundaryVersion = boundary.Version
				polled, _ := p.snapshot()
				if boundary.Status != "submitting" || boundary.Resume != "submitting" || boundary.Stage != "submit" || boundary.RemoteTaskID != task.InfoHash || !validPrivateCacheIntent(boundary) || len(polled) != 0 {
					t.Error("新任务发送前没有同次保存创建意图与稳定标识，或提前查询了远端")
				}
			}
			stableService(db, p).process(p, task)
			current := read(t, db, task.ID)
			polled, _ := p.snapshot()
			if current.Status != "downloading" || current.RemoteTaskID != task.InfoHash || current.Stage != "poll" || current.Version != boundaryVersion+1 || p.directAdds.Load() != 1 || len(polled) != 0 {
				t.Fatal("115 排队任务没有直接添加，或成功状态增加了额外标识写入")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDCancellationKeepsSubmittedIdentityAndQueuedIntent(t *testing.T) {
	for _, stage := range []string{"cache", "submit"} {
		t.Run(stage, func(t *testing.T) {
			s, p, task := storedStableTask(t, "queued")
			gate := newStageGate()
			if stage == "cache" {
				p.cacheGate = gate
			} else {
				p.addGate = gate
			}
			done := startStableProcess(t, s, p, task)
			waitStageStarted(t, gate)
			boundary := read(t, s.db, task.ID)
			wantID := ""
			if stage == "submit" {
				wantID = task.InfoHash
			}
			if boundary.RemoteTaskID != wantID || boundary.Stage != stage {
				t.Fatal("稳定标识提前绑定了排队任务，或在途提交未保存标识")
			}
			if err := s.Cancel(boundary.ID, boundary.Version); err != nil {
				t.Fatal(err)
			}
			cancelled := read(t, s.db, task.ID)
			gate.unblock()
			waitStageProcess(t, done)
			if current := read(t, s.db, task.ID); !reflect.DeepEqual(current, cancelled) {
				t.Fatal("旧请求覆盖了取消状态，或给稳定身份增加了回执补写")
			}
			restarted := stableService(s.db, p)
			if err := restarted.Retry(cancelled.ID, cancelled.Version); err != nil {
				t.Fatal(err)
			}
			restarted.tick()
			current := read(t, s.db, task.ID)
			polled, _ := p.snapshot()
			if current.Status != "downloading" || current.RemoteTaskID != task.InfoHash || p.directAdds.Load() != 1 || current.ExpiresAt != task.ExpiresAt {
				t.Fatal("取消后的重试未保留提交边界和缓存期限")
			}
			if stage == "cache" && len(polled) != 0 || stage == "submit" && !reflect.DeepEqual(polled, []string{task.InfoHash}) {
				t.Fatal("原排队取消跳过了新添加，或已提交任务重新添加了磁力")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDOldSubmittedTasksBindLocallyBeforeLookup(t *testing.T) {
	for _, status := range []string{"submitting", "downloading", "discovering", "failed"} {
		t.Run(status, func(t *testing.T) {
			s, p, task := storedStableTask(t, status)
			if status == "failed" {
				task.Resume, task.Retryable, task.Error = "submitting", true, "旧不确定提交"
			}
			if status == "discovering" {
				task.ResultID = "result"
				p.entries["result"] = clouddrive.File{ID: "result", ParentID: "cache", Name: "movie.mp4", Size: 100, PickCode: "fixture"}
			} else {
				p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 1, Progress: 42, FolderID: "cache"}
			}
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			assertBound := func() {
				current := read(t, s.db, task.ID)
				if current.RemoteTaskID != task.InfoHash {
					t.Error("发出查询前没有本地持久化旧任务的稳定标识")
				}
			}
			p.beforeFind = assertBound
			if status == "discovering" {
				p.onReadInfo = func(string) { assertBound() }
			}
			task = read(t, s.db, task.ID)
			if status == "failed" {
				if err := s.Retry(task.ID, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, s.db, task.ID)
				if task.Status != "submitting" {
					t.Fatal("旧不确定提交的手动重试错误地进入新添加队列")
				}
			}
			s.process(p, task)
			current := read(t, s.db, task.ID)
			polled, _ := p.snapshot()
			wantStatus := "downloading"
			if status == "discovering" {
				wantStatus = "ready"
			} else if !reflect.DeepEqual(polled, []string{task.InfoHash}) || current.Progress != 42 {
				t.Fatal("旧提交没有按 hash 精确查询")
			}
			if current.Status != wantStatus || current.RemoteTaskID != task.InfoHash || p.directAdds.Load() != 0 || current.CreatedAt != task.CreatedAt || current.ExpiresAt != task.ExpiresAt {
				t.Fatal("升级恢复重复添加磁力、丢失播放结果或改变缓存生命周期")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDAmbiguousCreationKeepsKnownHashAndNeverAddsAgain(t *testing.T) {
	for _, response := range []string{"timeout", "missing-id", "wrong-id"} {
		t.Run(response, func(t *testing.T) {
			s, p, task := storedStableTask(t, "queued")
			switch response {
			case "timeout":
				p.nextID, p.addTaskErr = "", &clouddrive.Error{Message: "测试创建响应超时", Retryable: true}
			case "missing-id":
				p.nextID = ""
			case "wrong-id":
				p.nextID = strings.Repeat("b", 40)
			}
			s.process(p, task)
			current := read(t, s.db, task.ID)
			if current.Status != "submitting" || current.Resume != "submitting" || current.RemoteTaskID != task.InfoHash || current.NextPoll <= time.Now().Unix() {
				t.Fatal("不确定创建丢失了已知 hash，或覆盖成其他任务标识")
			}
			p.nextID, p.addTaskErr = task.InfoHash, nil
			p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 1, Progress: 42, FolderID: "cache"}
			restarted := stableService(s.db, p)
			restarted.tick()
			if p.directAdds.Load() != 1 {
				t.Fatal("退避期间重复添加了不确定请求")
			}
			restarted.process(p, current)
			current = read(t, s.db, task.ID)
			polled, _ := p.snapshot()
			if current.Status != "downloading" || current.RemoteTaskID != task.InfoHash || p.directAdds.Load() != 1 || !reflect.DeepEqual(polled, []string{task.InfoHash}) {
				t.Fatal("重启后没有继续查询已保存的稳定身份")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDOldQueuedCancellationWaitsForExplicitRetry(t *testing.T) {
	s, p, task := storedStableTask(t, "cancelled")
	task.Resume, task.Stage, task.Error, task.Retryable = "queued", "cancel", "旧排队任务已取消", true
	if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s.db, task.ID)
	s.tick()
	if current := read(t, s.db, task.ID); !reflect.DeepEqual(current, task) || p.directAdds.Load() != 0 {
		t.Fatal("旧排队取消被自动恢复或提前推导标识")
	}
	if err := s.Retry(task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	s.tick()
	current := read(t, s.db, task.ID)
	polled, _ := p.snapshot()
	if current.Status != "downloading" || current.RemoteTaskID != task.InfoHash || p.directAdds.Load() != 1 || len(polled) != 0 {
		t.Fatal("原排队取消的显式重试跳过了直接添加")
	}
	assertNoTaskHistory(t, p.taskIDFixture)
}

func TestStableTaskIDRejectsWrongIdentityAndStaleCacheLifecycle(t *testing.T) {
	for _, scenario := range []string{"hash-mismatch", "wrong-id", "invalid-id", "missing-cache", "missing-expiry", "account", "lifecycle-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, task := storedStableTask(t, "downloading")
			switch scenario {
			case "hash-mismatch":
				task.Magnet = "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)
			case "wrong-id":
				task.RemoteTaskID = strings.Repeat("b", 40)
			case "invalid-id":
				task.RemoteTaskID = "bad/id"
			case "missing-cache":
				task.CacheFolderID = ""
			case "missing-expiry":
				task.ExpiresAt = 0
			case "account":
				task.AccountID = "other-account"
			}
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			task = read(t, s.db, task.ID)
			want := task
			if scenario == "lifecycle-conflict" {
				want.CacheFolderID, want.ExpiresAt, want.CreatedAt = "new-cache", task.ExpiresAt+1, task.CreatedAt+1
				if err := s.db.UpdateOfflineTask(want, want.Version); err != nil {
					t.Fatal(err)
				}
				want = read(t, s.db, task.ID)
			}
			s.process(p, task)
			current := read(t, s.db, task.ID)
			polled, removed := p.snapshot()
			if current.RemoteTaskID != want.RemoteTaskID || p.directAdds.Load() != 0 || len(polled) != 0 || len(removed) != 0 || len(p.deleted) != 0 {
				t.Fatal("错误身份被自动覆盖，或旧版本访问了远端记录")
			}
			if (scenario == "account" || scenario == "lifecycle-conflict") && !reflect.DeepEqual(current, want) {
				t.Fatal("账号或缓存生命周期变化后仍改写了任务")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDOldMissingPlaybackRecoversOnlyOwnedResult(t *testing.T) {
	s, p, task := storedStableTask(t, "discovering")
	task.ResultID, task.RecoveryFileID = "old-result", "old-result"
	if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s.db, task.ID)
	p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 2, Progress: 100, ResultID: "old-result", FolderID: "cache"}
	s.process(p, task)
	current := read(t, s.db, task.ID)
	if current.RemoteTaskID != task.InfoHash || current.RecoveryResultID != "old-result" || current.RecoveryFolderID != "cache" {
		t.Fatal("旧播放缺失没有先补标识并保存专属缓存恢复意图")
	}
	// 服务重建后移除已失效的同一 hash，再直接添加；缓存期限与目录保持不变。
	s = stableService(s.db, p)
	for range 3 {
		s.process(p, read(t, s.db, task.ID))
	}
	current = read(t, s.db, task.ID)
	_, removed := p.snapshot()
	if current.Status != "downloading" || current.RemoteTaskID != task.InfoHash || current.RecoveryResultID != "" || current.CreatedAt != task.CreatedAt || current.ExpiresAt != task.ExpiresAt || current.CacheFolderID != "cache" || p.directAdds.Load() != 1 || !reflect.DeepEqual(removed, []string{task.InfoHash}) || len(p.deleted) != 0 {
		t.Fatal("旧结果恢复重复添加、删除文件或改变缓存生命周期")
	}
	p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 2, Progress: 100, ResultID: "replacement", FolderID: "cache"}
	p.entries["replacement"] = clouddrive.File{ID: "replacement", ParentID: "cache", Name: "new.mp4", Size: 200, PickCode: "replacement"}
	s.process(p, current)
	current = read(t, s.db, task.ID)
	if current.Status != "ready" || current.ResultID != "replacement" || len(current.Files) != 1 || current.Files[0].ID != "replacement" {
		t.Fatal("恢复后没有仅发布本次替代结果")
	}
	assertNoTaskHistory(t, p.taskIDFixture)
}

func TestStableTaskIDRecoveryRejectsMissingExternalResults(t *testing.T) {
	for _, result := range []string{"missing", "empty-directory"} {
		t.Run(result, func(t *testing.T) {
			s, p, task := storedStableTask(t, "downloading")
			p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 2, ResultID: "external-result", FolderID: "foreign"}
			if result == "empty-directory" {
				p.entries["external-result"] = clouddrive.File{ID: "external-result", ParentID: "foreign", Name: "empty", Directory: true}
			}
			for range 2 {
				s.process(p, read(t, s.db, task.ID))
			}
			current := read(t, s.db, task.ID)
			_, removed := p.snapshot()
			if p.directAdds.Load() != 0 || len(removed) != 0 || len(p.deleted) != 0 || current.RecoveryResultID != "" || len(current.Files) != 0 || current.Error == "" {
				t.Fatal("外部已失效结果被误认为本任务缓存并移除或重新添加")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDPersistedExternalRecoveryCannotRemoveRecord(t *testing.T) {
	for _, entry := range []string{"process", "recoverResult"} {
		t.Run(entry, func(t *testing.T) {
			s, p, task := storedStableTask(t, "queued")
			task.RecoveryResultID, task.RecoveryFileID, task.RecoveryFolderID = "external-result", "external-result", "foreign"
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			task = read(t, s.db, task.ID)
			p.remotes[task.InfoHash] = clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 2, ResultID: "external-result", FolderID: "foreign"}
			if entry == "process" {
				s.process(p, task)
			} else {
				s.recoverResult(context.Background(), p, task)
			}
			current := read(t, s.db, task.ID)
			polled, removed := p.snapshot()
			if current.RemoteTaskID != "" || current.Status != "failed" || p.directAdds.Load() != 0 || len(polled) != 0 || len(removed) != 0 || len(p.deleted) != 0 {
				t.Fatal("持久化的外部恢复意图补标识后触及了外部记录")
			}
			if err := s.Retry(current.ID, current.Version); err != nil {
				t.Fatal(err)
			}
			current = read(t, s.db, task.ID)
			if current.Status != "queued" || current.RecoveryResultID != "" || current.RecoveryFolderID != "" || current.CacheFolderID != task.CacheFolderID || current.ExpiresAt != task.ExpiresAt {
				t.Fatal("外部恢复意图的显式重试未清除旧意图或改变了专属缓存")
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

func TestStableTaskIDCleanupRequiresSubmissionProofAndOwnedFolder(t *testing.T) {
	for _, scenario := range []string{"queued", "queued-with-id", "cancelled-queued", "pending-without-proof", "submitting", "ready", "external-record", "wrong-id"} {
		t.Run(scenario, func(t *testing.T) {
			status := "downloading"
			if scenario == "queued" || scenario == "queued-with-id" {
				status = "queued"
			} else if scenario == "cancelled-queued" {
				status = "cancelled"
			} else if scenario == "pending-without-proof" {
				status = "cleanup_pending"
			} else if scenario == "submitting" || scenario == "ready" {
				status = scenario
			}
			s, p, task := storedStableTask(t, status)
			task.ExpiresAt = time.Now().Add(-time.Second).Unix()
			if scenario == "cancelled-queued" {
				task.Resume = "queued"
			} else if scenario == "queued-with-id" {
				task.RemoteTaskID = task.InfoHash
			} else if scenario == "wrong-id" {
				task.RemoteTaskID = strings.Repeat("b", 40)
			}
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			remote := clouddrive.RemoteTask{RemoteID: task.InfoHash, Hash: task.InfoHash, Status: 1, FolderID: "cache"}
			if scenario == "external-record" {
				remote.FolderID = "foreign"
			}
			p.remotes[task.InfoHash] = remote
			s.cleanup(p, read(t, s.db, task.ID))
			current := read(t, s.db, task.ID)
			polled, removed := p.snapshot()
			withoutProof := scenario == "queued" || scenario == "queued-with-id" || scenario == "cancelled-queued" || scenario == "pending-without-proof"
			if scenario == "wrong-id" {
				if current.Status != "cleanup_pending" || current.RemoteTaskID != task.RemoteTaskID || len(polled) != 0 || len(removed) != 0 || len(p.deleted) != 0 {
					t.Fatal("错误标识导致清理其他任务或直接删除缓存")
				}
			} else {
				if current.Status != "expired" || current.RemoteTaskID != "" || !reflect.DeepEqual(p.deleted, []string{"cache"}) {
					t.Fatal("旧记录到期没有只清理核验过的专属缓存")
				}
				if withoutProof && (len(polled) != 0 || len(removed) != 0) || scenario == "external-record" && len(removed) != 0 {
					t.Fatal("没有提交证据或外部目录的记录仍被查询或删除")
				}
				if !withoutProof && scenario != "external-record" && !reflect.DeepEqual(removed, []string{task.InfoHash}) {
					t.Fatal("已提交旧任务没有按稳定身份精确清理")
				}
			}
			assertNoTaskHistory(t, p.taskIDFixture)
		})
	}
}

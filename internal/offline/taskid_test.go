package offline

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

// 该夹具真正实现任务 ID 能力；旧的磁力历史查询和提交入口均视为测试错误。
type taskIDFixture struct {
	*cacheFixture
	nextID, submitFolder         string
	addTaskErr, lookupErr        error
	beforeAdd                    func()
	cacheGate, addGate, pollGate *stageGate
	legacyFinds, legacyAdds      atomic.Int32
	legacyRemoves, directAdds    atomic.Int32
	mu                           sync.Mutex
	remotes                      map[string]clouddrive.RemoteTask
	polledIDs, removedIDs        []string
}

var _ clouddrive.TaskIDProvider = (*taskIDFixture)(nil)
var _ clouddrive.ConditionalTaskRemover = (*taskIDFixture)(nil)

func newTaskIDFixture() *taskIDFixture {
	return &taskIDFixture{cacheFixture: newCacheFixture(), nextID: "task-1", remotes: make(map[string]clouddrive.RemoteTask)}
}

func (p *taskIDFixture) Find(context.Context, string) (*clouddrive.RemoteTask, error) {
	p.legacyFinds.Add(1)
	return nil, errors.New("禁止扫描远端磁力历史")
}

func (p *taskIDFixture) Add(context.Context, string, string) error {
	p.legacyAdds.Add(1)
	return errors.New("必须使用返回任务标识的提交入口")
}

func (p *taskIDFixture) RemoveTask(context.Context, string) error {
	p.legacyRemoves.Add(1)
	return errors.New("禁止按磁力移除远端记录")
}

func (p *taskIDFixture) Info(ctx context.Context, id string) (clouddrive.File, error) {
	if id == "work" && p.cacheGate != nil {
		if err := p.cacheGate.wait(ctx); err != nil {
			return clouddrive.File{}, err
		}
	}
	return p.cacheFixture.Info(ctx, id)
}

func (p *taskIDFixture) AddTask(ctx context.Context, magnet, folder string) (string, error) {
	p.directAdds.Add(1)
	if p.beforeAdd != nil {
		p.beforeAdd()
	}
	if p.addGate != nil {
		if err := p.addGate.wait(ctx); err != nil {
			return "", err
		}
	}
	if p.addTaskErr != nil {
		return p.nextID, p.addTaskErr
	}
	_, hash, err := clouddrive.NormalizeMagnet(magnet)
	if err != nil {
		return "", &clouddrive.Error{Message: "测试磁力格式错误", NotSubmitted: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitFolder = folder
	if validRemoteTaskID(p.nextID) {
		p.remotes[p.nextID] = clouddrive.RemoteTask{RemoteID: p.nextID, Hash: hash, FolderID: folder, Status: 1}
	}
	return p.nextID, nil
}

func (p *taskIDFixture) FindTask(ctx context.Context, hash, id string) (*clouddrive.RemoteTask, error) {
	p.mu.Lock()
	p.polledIDs = append(p.polledIDs, id)
	p.mu.Unlock()
	if p.pollGate != nil {
		if err := p.pollGate.wait(ctx); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lookupErr != nil {
		return nil, p.lookupErr
	}
	remote, found := p.remotes[id]
	if !found {
		return nil, nil
	}
	if remote.Hash == "" {
		remote.Hash = hash
	}
	return &remote, nil
}

func (p *taskIDFixture) RemoveTaskIf(ctx context.Context, hash string, expected clouddrive.RemoteTask) error {
	current, err := p.FindTask(ctx, hash, expected.RemoteID)
	if err != nil || current == nil {
		return err
	}
	if current.RemoteID != expected.RemoteID || current.Hash != hash || current.ResultID != expected.ResultID || current.Status != expected.Status || current.FolderID != expected.FolderID {
		return &clouddrive.Error{Message: "精确任务身份发生变化，未移除记录", Retryable: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removedIDs = append(p.removedIDs, expected.RemoteID)
	delete(p.remotes, expected.RemoteID)
	return nil
}

func (p *taskIDFixture) snapshot() ([]string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.polledIDs...), append([]string(nil), p.removedIDs...)
}

func assertNoTaskHistory(t *testing.T, p *taskIDFixture) {
	t.Helper()
	if p.legacyFinds.Load() != 0 || p.legacyAdds.Load() != 0 || p.legacyRemoves.Load() != 0 {
		t.Fatal("任务 ID 网盘回退使用了历史查询、旧提交或按磁力删除")
	}
}

func startTaskIDProcess(t *testing.T, db *store.Store, p *taskIDFixture, task store.OfflineTask) (*Service, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := guangyaService(db, p)
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
	return s, done
}

func TestTaskIDQueueSubmitsWithoutHistoryAndRestartsWithExactPolling(t *testing.T) {
	for _, existingIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "existing-version3"}[existingIntent], func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := newTaskIDFixture()
			// 相同磁力的外部历史记录不会影响本次直接提交。
			p.remotes["external-history"] = clouddrive.RemoteTask{RemoteID: "external-history", Hash: task.InfoHash, FolderID: "foreign", Status: 2}
			if existingIntent {
				task.ExpiresAt = time.Now().Add(time.Hour).Unix()
				if err := db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, db, task.ID)
				task.CacheFolderID = "cache"
				p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
				if err := db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, db, task.ID)
				if task.Version != 3 {
					t.Fatal("未构造原有排队任务的持久边界")
				}
			}
			var submissionVersion int64
			p.beforeAdd = func() {
				boundary, err := db.OfflineTask(task.ID)
				if err != nil || boundary.Status != "submitting" || boundary.Resume != "submitting" || boundary.Stage != "submit" || boundary.RemoteTaskID != "" {
					t.Error("发送请求前未保存独立的创建边界")
				}
				submissionVersion = boundary.Version
			}
			s := guangyaService(db, p)
			s.process(p, task)
			current := read(t, db, task.ID)
			polled, _ := p.snapshot()
			if current.Status != "downloading" || current.Stage != "poll" || current.RemoteTaskID != "task-1" || current.Version != submissionVersion+1 || p.directAdds.Load() != 1 || len(polled) != 0 || p.submitFolder != current.CacheFolderID {
				t.Fatal("新任务未直接提交，或任务标识未与下载状态一起落盘")
			}
			remote := p.remotes["task-1"]
			remote.Progress = 42
			p.remotes["task-1"] = remote
			// 重建服务对象后只使用持久任务标识，不依赖提交时的内存映射。
			restarted := guangyaService(db, p)
			restarted.process(p, current)
			updated := read(t, db, task.ID)
			polled, _ = p.snapshot()
			if updated.RemoteTaskID != "task-1" || updated.Status != "downloading" || updated.Stage != "poll" || updated.Progress != 42 || updated.Version != current.Version+1 || !reflect.DeepEqual(polled, []string{"task-1"}) || p.directAdds.Load() != 1 {
				t.Fatal("服务恢复没有精确轮询本次任务，或正常轮询增加了额外写入")
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDCancellationPreservesCASAcrossCacheSubmitAndPoll(t *testing.T) {
	for _, stage := range []string{"cache", "submit", "poll"} {
		t.Run(stage, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := newTaskIDFixture()
			gate := newStageGate()
			switch stage {
			case "cache":
				p.cacheGate = gate
			case "submit":
				p.addGate = gate
			case "poll":
				guangyaService(db, p).process(p, task)
				task = read(t, db, task.ID)
				p.pollGate = gate
			}
			s, done := startTaskIDProcess(t, db, p, task)
			waitStageStarted(t, gate)
			current := read(t, db, task.ID)
			if current.Stage != stage || s.Cancel(current.ID, current.Version) != nil {
				t.Fatal("无法按可见阶段的最新版本取消任务")
			}
			cancelled := read(t, db, task.ID)
			gate.unblock()
			waitStageProcess(t, done)
			current = read(t, db, task.ID)
			want := cancelled
			if stage == "submit" {
				want.RemoteTaskID = "task-1"
				want.Version++
				want.UpdatedAt = current.UpdatedAt
			}
			if !reflect.DeepEqual(current, want) {
				t.Fatal("旧请求覆盖了取消状态，或未单独补存本次成功提交的标识")
			}
			wantAdds := int32(1)
			if stage == "cache" {
				wantAdds = 0
			}
			if p.directAdds.Load() != wantAdds {
				t.Fatal("取消后继续发送了新的创建请求")
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDUncertainSubmissionWithoutIDRequiresManualResubmission(t *testing.T) {
	for _, test := range []struct {
		name, id string
		err      error
	}{
		{"request-timeout", "", &clouddrive.Error{Message: "创建请求超时", Retryable: true}},
		{"missing-id", "", nil},
		{"invalid-id", "bad/id", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := newTaskIDFixture()
			p.nextID, p.addTaskErr = test.id, test.err
			s := guangyaService(db, p)
			s.process(p, task)
			current := read(t, db, task.ID)
			if current.Status != "failed" || current.Resume != "submitting" || current.RemoteTaskID != "" || !current.Retryable || !strings.Contains(current.Error, "提交结果不明确") || !strings.Contains(current.Error, "点击重试将重新添加磁力") {
				t.Fatal("缺失标识的模糊提交没有停止自动重写")
			}
			created, expires, cache := current.CreatedAt, current.ExpiresAt, current.CacheFolderID
			p.nextID, p.addTaskErr = "task-on-retry", nil
			restarted := guangyaService(db, p)
			for range 2 {
				restarted.tick()
			}
			if p.directAdds.Load() != 1 || read(t, db, task.ID).Version != current.Version {
				t.Fatal("没有用户重试时自动重复发送了模糊创建请求")
			}
			if err := restarted.Retry(current.ID, current.Version); err != nil {
				t.Fatal(err)
			}
			queued := read(t, db, task.ID)
			if queued.Status != "queued" || queued.RemoteTaskID != "" || queued.Resume != "queued" || queued.CreatedAt != created || queued.ExpiresAt != expires || queued.CacheFolderID != cache {
				t.Fatal("用户明确重试没有进入新提交队列，或改变了缓存安全边界")
			}
			restarted.tick()
			current = read(t, db, task.ID)
			polled, _ := p.snapshot()
			if p.directAdds.Load() != 2 || len(polled) != 0 || current.Status != "downloading" || current.Stage != "poll" || current.RemoteTaskID != "task-on-retry" {
				t.Fatal("手动重试没有直接创建新的任务，或回退查询了历史")
			}
			restarted.process(p, current)
			polled, _ = p.snapshot()
			if !reflect.DeepEqual(polled, []string{"task-on-retry"}) || p.directAdds.Load() != 2 {
				t.Fatal("重新添加后的轮询没有使用新任务标识")
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDCancelledSubmissionReceiptPersistsAndRetryOnlyPolls(t *testing.T) {
	db, library, video := setup(t)
	task := newGuangyaTask(t, db, library, video)
	p := newTaskIDFixture()
	p.addGate = newStageGate()
	s, done := startTaskIDProcess(t, db, p, task)
	waitStageStarted(t, p.addGate)
	boundary := read(t, db, task.ID)
	if err := s.Cancel(boundary.ID, boundary.Version); err != nil {
		t.Fatal(err)
	}
	cancelled := read(t, db, task.ID)
	p.addGate.unblock()
	waitStageProcess(t, done)
	current := read(t, db, task.ID)
	want := cancelled
	want.RemoteTaskID, want.Version, want.UpdatedAt = "task-1", cancelled.Version+1, current.UpdatedAt
	if !reflect.DeepEqual(current, want) {
		t.Fatal("成功回执没有仅补存任务标识并保留取消状态")
	}
	// 服务重建后重试继续本次下载，取消只停止了本地等待。
	restarted := guangyaService(db, p)
	if err := restarted.Retry(current.ID, current.Version); err != nil {
		t.Fatal(err)
	}
	retried := read(t, db, task.ID)
	if retried.Status != "submitting" || retried.RemoteTaskID != "task-1" {
		t.Fatal("有成功回执的取消任务被误当成新的添加")
	}
	restarted.tick()
	current = read(t, db, task.ID)
	polled, _ := p.snapshot()
	if current.Status != "downloading" || current.RemoteTaskID != "task-1" || current.Stage != "poll" || p.directAdds.Load() != 1 || !reflect.DeepEqual(polled, []string{"task-1"}) || current.CreatedAt != cancelled.CreatedAt || current.ExpiresAt != cancelled.ExpiresAt {
		t.Fatal("取消后的重试没有精确继续本次下载，或重新添加了磁力")
	}
	assertNoTaskHistory(t, p)
}

func TestTaskIDOldReceiptCannotBindChangedLifecycle(t *testing.T) {
	for _, scenario := range []string{"account", "hash", "magnet", "root", "cache", "created", "expires", "deadline", "recoveries", "recovery-intent", "replacement-id", "expired", "manual-retry", "new-submit-cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := newTaskIDFixture()
			p.addGate = newStageGate()
			s, done := startTaskIDProcess(t, db, p, task)
			waitStageStarted(t, p.addGate)
			boundary := read(t, db, task.ID)
			// 一次版本推进同时模拟取消和身份变化，确保逐项身份核验有效。
			changed := boundary
			changed.Status, changed.Resume, changed.Stage, changed.Error = "cancelled", "submitting", "cancel", "保留最新状态"
			changed.Retryable = true
			switch scenario {
			case "account":
				changed.AccountID = store.CloudGuangyaAccount("other-account")
			case "hash":
				changed.InfoHash = strings.Repeat("b", 40)
			case "magnet":
				changed.Magnet += "&dn=新的添加"
			case "root":
				changed.FolderID = "other-root"
			case "cache":
				changed.CacheFolderID = "other-cache"
			case "created":
				changed.CreatedAt++
			case "expires":
				changed.ExpiresAt++
			case "deadline":
				changed.Deadline++
			case "recoveries":
				changed.Recoveries++
			case "recovery-intent":
				changed.RecoveryResultID, changed.RecoveryFileID, changed.RecoveryFolderID = "other-result", "other-file", changed.CacheFolderID
			case "replacement-id":
				changed.RemoteTaskID = "replacement-task"
			case "expired":
				changed.Status, changed.Stage = "expired", "cleanup"
			}
			if err := db.UpdateOfflineTask(changed, changed.Version); err != nil {
				t.Fatal(err)
			}
			if scenario == "manual-retry" || scenario == "new-submit-cancelled" {
				changed = read(t, db, task.ID)
				if err := s.Retry(changed.ID, changed.Version); err != nil {
					t.Fatal(err)
				}
				if scenario == "new-submit-cancelled" {
					// 新提交可沿用账号、磁力、缓存和期限，旧回执仍不能绑定。
					changed = read(t, db, task.ID)
					changed.Status, changed.Resume, changed.Stage = "submitting", "submitting", "submit"
					changed.Deadline = boundary.Deadline
					if err := db.UpdateOfflineTask(changed, changed.Version); err != nil {
						t.Fatal(err)
					}
					changed = read(t, db, task.ID)
					if err := s.Cancel(changed.ID, changed.Version); err != nil {
						t.Fatal(err)
					}
				}
			}
			changed = read(t, db, task.ID)
			p.addGate.unblock()
			waitStageProcess(t, done)
			current := read(t, db, task.ID)
			if !reflect.DeepEqual(current, changed) || p.directAdds.Load() != 1 {
				t.Fatal("旧回执被绑定到变化后的任务，或覆盖了新生命周期")
			}
			if scenario == "manual-retry" {
				p.nextID = "new-user-task"
				s.tick()
				current = read(t, db, task.ID)
				polled, _ := p.snapshot()
				if current.Status != "downloading" || current.RemoteTaskID != "new-user-task" || p.directAdds.Load() != 2 || len(polled) != 0 {
					t.Fatal("在途时明确重试没有重新添加，或误用了旧回执")
				}
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDManualRetryCanPersistBeforeCredentialsLoad(t *testing.T) {
	db, library, video := setup(t)
	task := newGuangyaTask(t, db, library, video)
	p := newTaskIDFixture()
	p.nextID, p.addTaskErr = "", &clouddrive.Error{Message: "测试鉴权失败", Auth: true}
	s := guangyaService(db, p)
	s.process(p, task)
	failed := read(t, db, task.ID)
	if failed.Status != "needs_auth" || !strings.Contains(failed.Error, "点击重试将重新添加磁力") {
		t.Fatal("缺标识的鉴权失败未提供正确的手动重试语义")
	}
	s.provider = func() (clouddrive.Provider, string, error) {
		return nil, "", errors.New("测试凭据暂时无法装载")
	}
	if err := s.Retry(failed.ID, failed.Version); err != nil {
		t.Fatal(err)
	}
	queued := read(t, db, task.ID)
	if queued.Status != "queued" || queued.Resume != "queued" || queued.ExpiresAt != failed.ExpiresAt || queued.CacheFolderID != failed.CacheFolderID {
		t.Fatal("凭据不可用时丢失手动重新添加意图或缓存边界")
	}
	p.nextID, p.addTaskErr = "after-auth", nil
	guangyaService(db, p).tick()
	current := read(t, db, task.ID)
	polled, _ := p.snapshot()
	if current.Status != "downloading" || current.RemoteTaskID != "after-auth" || p.directAdds.Load() != 2 || len(polled) != 0 {
		t.Fatal("凭据恢复后未直接执行用户授权的重新添加")
	}
	assertNoTaskHistory(t, p)
}

func TestTaskIDNotSubmittedFailureCanRetryWithNewIdentity(t *testing.T) {
	db, library, video := setup(t)
	task := newGuangyaTask(t, db, library, video)
	// 即使旧持久意图携带标识，新提交也必须先清理该标识。
	task.RemoteTaskID = "stale-task"
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, db, task.ID)
	p := newTaskIDFixture()
	p.nextID, p.addTaskErr = "", &clouddrive.Error{Message: "解析失败，尚未发送创建请求", Retryable: true, NotSubmitted: true}
	s := guangyaService(db, p)
	s.process(p, task)
	current := read(t, db, task.ID)
	if current.Status != "queued" || current.Resume != "queued" || current.RemoteTaskID != "" || current.Stage != "submit" || current.NextPoll <= time.Now().Unix() {
		t.Fatal("明确未创建的失败没有保留可安全重试的边界")
	}
	current.NextPoll = 0
	if err := db.UpdateOfflineTask(current, current.Version); err != nil {
		t.Fatal(err)
	}
	p.nextID, p.addTaskErr = "fresh-task", nil
	s.tick()
	current = read(t, db, task.ID)
	polled, _ := p.snapshot()
	if current.Status != "downloading" || current.RemoteTaskID != "fresh-task" || p.directAdds.Load() != 2 || len(polled) != 0 {
		t.Fatal("未发出创建请求的重试没有直接提交新任务，或复用了旧标识")
	}
	assertNoTaskHistory(t, p)
}

func TestTaskIDRecoveryRemovesOldIDAndDirectlyCreatesNewID(t *testing.T) {
	db, library, video := setup(t)
	task := newGuangyaTask(t, db, library, video)
	p := newTaskIDFixture()
	p.nextID = "old-task"
	s := guangyaService(db, p)
	s.process(p, task)
	current := read(t, db, task.ID)
	p.remotes["old-task"] = clouddrive.RemoteTask{RemoteID: "old-task", Hash: task.InfoHash, Status: 2, Progress: 100, FolderID: "cache", ResultID: "old-result"}
	p.remotes["unrelated-history"] = clouddrive.RemoteTask{RemoteID: "unrelated-history", Hash: task.InfoHash, Status: 2, FolderID: "foreign", ResultID: "foreign-result"}
	p.entries["old-result"] = clouddrive.File{ID: "old-result", ParentID: "cache", Name: "movie.mp4", PickCode: "fixture", Size: 100}
	s.process(p, current)
	current = read(t, db, task.ID)
	if current.Status != "ready" || current.RemoteTaskID != "old-task" || current.ResultID != "old-result" {
		t.Fatal("精确任务完成结果没有经过归属核验并发布")
	}
	created, expires := current.CreatedAt, current.ExpiresAt
	delete(p.entries, "old-result")
	current.Status, current.RecoveryFileID, current.Files = "discovering", "old-result", []clouddrive.File{}
	if err := db.UpdateOfflineTask(current, current.Version); err != nil {
		t.Fatal(err)
	}
	s.process(p, read(t, db, task.ID))
	current = read(t, db, task.ID)
	if current.RecoveryResultID != "old-result" || current.RemoteTaskID != "old-task" || current.Recoveries != 1 {
		t.Fatal("缺失结果的恢复意图没有保留旧任务标识")
	}
	// 重建服务后仅移除已证明结果失效的旧任务记录。
	s = guangyaService(db, p)
	s.process(p, current)
	current = read(t, db, task.ID)
	_, removed := p.snapshot()
	if !current.RecoveryRemoved || current.RemoteTaskID != "old-task" || !reflect.DeepEqual(removed, []string{"old-task"}) || p.directAdds.Load() != 1 {
		t.Fatal("恢复没有按精确标识移除旧记录，或提前重复创建")
	}
	s.process(p, current)
	current = read(t, db, task.ID)
	if current.Status != "queued" || current.RemoteTaskID != "" || current.RecoveryResultID != "" || p.directAdds.Load() != 1 {
		t.Fatal("旧记录消失后没有清除标识并进入新提交边界")
	}
	p.nextID = "new-task"
	p.beforeAdd = func() {
		boundary, err := db.OfflineTask(task.ID)
		if err != nil || boundary.RemoteTaskID != "" || boundary.RecoveryResultID != "" {
			t.Error("新提交携带了旧任务标识或恢复意图")
		}
	}
	s.process(p, current)
	current = read(t, db, task.ID)
	if current.Status != "downloading" || current.Stage != "poll" || current.RemoteTaskID != "new-task" || p.directAdds.Load() != 2 || current.CreatedAt != created || current.ExpiresAt != expires || len(p.deleted) != 0 {
		t.Fatal("恢复后的新提交复用旧标识、延长缓存期限或删除了结果文件")
	}
	s.process(p, current)
	polled, _ := p.snapshot()
	if len(polled) == 0 || polled[len(polled)-1] != "new-task" || p.remotes["unrelated-history"].RemoteID != "unrelated-history" {
		t.Fatal("新任务轮询仍使用旧标识或触及无关历史记录")
	}
	assertNoTaskHistory(t, p)
}

func TestTaskIDRecoveryWithoutIDNeverFallsBackToHistory(t *testing.T) {
	for _, recoveryIntent := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovering", true: "recovery-intent"}[recoveryIntent], func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ExpiresAt, task.ResultID = "discovering", "cache", time.Now().Add(time.Hour).Unix(), "missing-result"
			if recoveryIntent {
				task.Status, task.RecoveryResultID, task.RecoveryFileID, task.RecoveryFolderID = "queued", "missing-result", "missing-result", "cache"
			}
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p := newTaskIDFixture()
			p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			guangyaService(db, p).process(p, read(t, db, task.ID))
			current := read(t, db, task.ID)
			polled, removed := p.snapshot()
			if current.Status != "failed" || current.Error == "" || current.Stage != "recover" || p.directAdds.Load() != 0 || len(polled) != 0 || len(removed) != 0 {
				t.Fatal("无任务标识的旧结果恢复扫描历史、重新创建或删除了远端记录")
			}
			created, expires := current.CreatedAt, current.ExpiresAt
			service := guangyaService(db, p)
			if err := service.Retry(current.ID, current.Version); err != nil {
				t.Fatal(err)
			}
			current = read(t, db, task.ID)
			if current.Status != "queued" || current.Resume != "queued" || current.RecoveryFileID != "" || current.RecoveryResultID != "" || current.RecoveryFolderID != "" || current.RecoveryRemoved || current.ResultID != "" || len(current.Files) != 0 || current.Progress != 0 || current.VisibilityUntil != 0 || current.CacheFolderID != "cache" || current.CreatedAt != created || current.ExpiresAt != expires {
				t.Fatal("手动重试没有清除旧结果与恢复意图，或改变了缓存归属")
			}
			service.tick()
			current = read(t, db, task.ID)
			polled, removed = p.snapshot()
			if current.Status != "downloading" || current.RemoteTaskID != "task-1" || p.directAdds.Load() != 1 || len(polled) != 0 || len(removed) != 0 {
				t.Fatal("缺 ID 的旧结果无法通过手动重试直接重新添加")
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDCleanupWithoutIDDeletesOnlyVerifiedExpiredCache(t *testing.T) {
	for _, status := range []string{"ready", "discovering", "queued", "submitting"} {
		t.Run(status, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ExpiresAt = status, "cache", time.Now().Add(-time.Second).Unix()
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p := newTaskIDFixture()
			p.entries["cache"] = clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			p.remotes["external-history"] = clouddrive.RemoteTask{RemoteID: "external-history", Hash: task.InfoHash, FolderID: "foreign", Status: 1}
			guangyaService(db, p).cleanup(p, read(t, db, task.ID))
			current := read(t, db, task.ID)
			polled, removed := p.snapshot()
			if current.Status != "expired" || current.RemoteTaskID != "" || !reflect.DeepEqual(p.deleted, []string{"cache"}) || len(polled) != 0 || len(removed) != 0 || p.remotes["external-history"].RemoteID != "external-history" {
				t.Fatal("到期的无标识任务没有只清理本任务缓存，或触及无关历史")
			}
			assertNoTaskHistory(t, p)
		})
	}
}

func TestTaskIDCleanupRejectsChangedCacheAndOtherTaskIdentities(t *testing.T) {
	for _, scenario := range []string{"known-id", "moved-cache", "changed-name", "other-id", "other-hash", "foreign-folder"} {
		t.Run(scenario, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ExpiresAt, task.RemoteTaskID = "ready", "cache", time.Now().Add(-time.Second).Unix(), "owned-task"
			if scenario == "moved-cache" || scenario == "changed-name" {
				task.RemoteTaskID = ""
			}
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			p := newTaskIDFixture()
			child := clouddrive.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			remote := clouddrive.RemoteTask{RemoteID: "owned-task", Hash: task.InfoHash, FolderID: "cache", Status: 1}
			switch scenario {
			case "moved-cache":
				child.ParentID = "foreign"
			case "changed-name":
				child.Name = "其他目录"
			case "other-id":
				remote.RemoteID = "other-task"
			case "other-hash":
				remote.Hash = strings.Repeat("b", 40)
			case "foreign-folder":
				remote.FolderID = "foreign"
			}
			p.entries["cache"] = child
			p.remotes["owned-task"] = remote
			guangyaService(db, p).cleanup(p, read(t, db, task.ID))
			current := read(t, db, task.ID)
			polled, removed := p.snapshot()
			if scenario == "known-id" {
				if current.Status != "expired" || current.RemoteTaskID != "" || !reflect.DeepEqual(removed, []string{"owned-task"}) || !reflect.DeepEqual(p.deleted, []string{"cache"}) {
					t.Fatal("已知标识的到期任务没有精确移除并清除本地标识")
				}
			} else if scenario == "foreign-folder" {
				if current.Status != "expired" || len(removed) != 0 || !reflect.DeepEqual(p.deleted, []string{"cache"}) {
					t.Fatal("清理取消了已移至外部目录的记录，或未删除本任务缓存")
				}
			} else if current.Status != "cleanup_pending" || len(removed) != 0 || len(p.deleted) != 0 {
				t.Fatal("缓存或远端身份变化后仍删除了目录或任务记录")
			}
			for _, id := range polled {
				if id != "owned-task" {
					t.Fatal("清理轮询了其他远端任务标识")
				}
			}
			assertNoTaskHistory(t, p)
		})
	}
}

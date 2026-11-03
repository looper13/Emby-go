package offline

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/store"
)

type stageGate struct {
	started, release chan struct{}
	startOnce        sync.Once
	releaseOnce      sync.Once
}

func newStageGate() *stageGate {
	return &stageGate{started: make(chan struct{}), release: make(chan struct{})}
}

func (g *stageGate) wait(ctx context.Context) error {
	g.startOnce.Do(func() { close(g.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.release:
		return nil
	}
}

func (g *stageGate) unblock() {
	if g != nil {
		g.releaseOnce.Do(func() { close(g.release) })
	}
}

type stageFixture struct {
	providerFixture
	cacheGate, findGate, addGate *stageGate
	findCalls, addCalls          atomic.Int32
	findCallsAtAdd               atomic.Int32
}

func (p *stageFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	if id == "work" && p.cacheGate != nil {
		if err := p.cacheGate.wait(ctx); err != nil {
			return cloud115.File{}, err
		}
	}
	return p.providerFixture.Info(ctx, id)
}

func (p *stageFixture) Find(ctx context.Context, hash string) (*cloud115.RemoteTask, error) {
	p.findCalls.Add(1)
	if p.findGate != nil {
		if err := p.findGate.wait(ctx); err != nil {
			return nil, err
		}
	}
	return p.providerFixture.Find(ctx, hash)
}

func (p *stageFixture) Add(ctx context.Context, magnet, folder string) error {
	p.addCalls.Add(1)
	p.findCallsAtAdd.Store(p.findCalls.Load())
	if p.addGate != nil {
		if err := p.addGate.wait(ctx); err != nil {
			return err
		}
	}
	return p.providerFixture.Add(ctx, magnet, folder)
}

func startStageProcess(t *testing.T, db *store.Store, p *stageFixture, task store.OfflineTask) (*Service, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := guangyaService(db, p)
	s.ctx = ctx
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.process(p, task)
	}()
	// 测试失败也释放阻塞请求，避免后台任务访问已关闭的隔离数据库。
	t.Cleanup(func() {
		cancel()
		p.cacheGate.unblock()
		p.findGate.unblock()
		p.addGate.unblock()
		waitStageProcess(t, done)
	})
	return s, done
}

func waitStageStarted(t *testing.T, gate *stageGate) {
	t.Helper()
	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		t.Fatal("后台任务没有进入预期的阻塞阶段")
	}
}

func waitStageProcess(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("后台任务没有及时结束")
	}
}

func TestOfflineBlockingOperationsPublishStages(t *testing.T) {
	for _, test := range []struct {
		name, status, stage string
		recovering          bool
	}{
		{"cache", "queued", "cache", false},
		{"initial-reconcile", "queued", "reconcile", false},
		{"uncertain-reconcile", "submitting", "reconcile", false},
		{"result-recovery-reconcile", "queued", "reconcile", true},
		{"submit", "queued", "submit", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := &stageFixture{}
			if test.status == "submitting" || test.recovering {
				task.Status, task.CacheFolderID, task.ExpiresAt = test.status, "cache", time.Now().Add(time.Hour).Unix()
				p.cacheName = "emby-go-" + task.ID
				if test.recovering {
					task.RecoveryResultID, task.RecoveryFileID, task.RecoveryFolderID = "old-result", "old-result", "cache"
					task.Stage = "recover"
				}
				if err := db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, db, task.ID)
			}
			gate := newStageGate()
			switch test.stage {
			case "cache":
				p.cacheGate = gate
			case "reconcile":
				p.findGate = gate
			case "submit":
				p.addGate = gate
			}
			_, done := startStageProcess(t, db, p, task)
			waitStageStarted(t, gate)
			current := read(t, db, task.ID)
			status := test.status
			if test.stage == "submit" {
				status = "submitting"
			}
			if current.Status != status || current.Stage != test.stage || current.Error != "" || current.Version <= task.Version || current.Progress != 0 {
				t.Fatalf("阻塞操作开始前未发布阶段：status=%s stage=%s version=%d", current.Status, current.Stage, current.Version)
			}
			if test.stage == "submit" && current.Resume != "submitting" {
				t.Fatal("提交阶段没有保存不明确提交的恢复边界")
			}
			gate.unblock()
			waitStageProcess(t, done)
			current = read(t, db, task.ID)
			if test.status == "submitting" || test.recovering {
				if p.addCalls.Load() != 0 {
					t.Fatal("恢复对账重复提交了下载")
				}
			} else if current.Status != "downloading" || current.Stage != "" || p.addCalls.Load() != 1 {
				t.Fatal("阶段保存破坏了后续提交或版本推进")
			}
		})
	}
}

func TestOfflineCancellationDuringBlockingStagesCannotSubmitOldVersion(t *testing.T) {
	for _, stage := range []string{"cache", "reconcile"} {
		t.Run(stage, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			gate := newStageGate()
			p := &stageFixture{}
			if stage == "cache" {
				p.cacheGate = gate
			} else {
				p.findGate = gate
			}
			s, done := startStageProcess(t, db, p, task)
			waitStageStarted(t, gate)
			current := read(t, db, task.ID)
			if current.Stage != stage || s.Cancel(current.ID, current.Version) != nil {
				t.Fatal("无法用可见阶段的最新版本取消任务")
			}
			cancelled := read(t, db, task.ID)
			gate.unblock()
			waitStageProcess(t, done)
			current = read(t, db, task.ID)
			if current.Status != "cancelled" || current.Stage != "cancel" || current.Version != cancelled.Version || p.addCalls.Load() != 0 {
				t.Fatal("取消后的旧版本覆盖任务状态或继续提交")
			}
		})
	}
}

func TestOfflineReconcileFailureBacksOffAndRechecksBeforeSubmission(t *testing.T) {
	for _, status := range []string{"queued", "submitting"} {
		t.Run(status, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			task.Status, task.CacheFolderID, task.ExpiresAt = status, "cache", time.Now().Add(time.Hour).Unix()
			if err := db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			task = read(t, db, task.ID)
			p := &stageFixture{providerFixture: providerFixture{cacheName: "emby-go-" + task.ID, findError: &cloud115.Error{Message: "分页结果不完整", Retryable: true}}}
			s := guangyaService(db, p)
			s.process(p, task)
			current := read(t, db, task.ID)
			if current.Status != status || current.Stage != "reconcile" || current.Error != "分页结果不完整" || !current.Retryable || current.Attempts != 1 || current.NextPoll <= time.Now().Unix() || p.findCalls.Load() != 1 || p.addCalls.Load() != 0 {
				t.Fatal("不完整对账没有保留状态并退避，或发生提前提交")
			}
			s.tick()
			if p.findCalls.Load() != 1 {
				t.Fatal("退避期间提前再次查询了远端任务")
			}
			current.NextPoll = 0
			if err := db.UpdateOfflineTask(current, current.Version); err != nil {
				t.Fatal(err)
			}
			p.findError = nil
			s.tick()
			current = read(t, db, task.ID)
			if p.findCalls.Load() != 2 {
				t.Fatal("重试没有先重新完成对账")
			}
			if status == "queued" {
				if p.addCalls.Load() != 1 || p.findCallsAtAdd.Load() != 2 || current.Status != "downloading" {
					t.Fatal("首次提交没有等待完整对账")
				}
			} else if p.addCalls.Load() != 0 || current.Status != "submitting" || current.Stage != "reconcile" || current.NextPoll <= time.Now().Unix() {
				t.Fatal("不明确提交在重试时重复提交或丢失退避状态")
			}
		})
	}
}

func TestOfflineDownloadingPollKeepsOneStateWrite(t *testing.T) {
	db, library, video := setup(t)
	task := newGuangyaTask(t, db, library, video)
	task.Status, task.CacheFolderID, task.ExpiresAt = "downloading", "cache", time.Now().Add(time.Hour).Unix()
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, db, task.ID)
	p := &stageFixture{providerFixture: providerFixture{remote: &cloud115.RemoteTask{Status: 1, Progress: 42, FolderID: "cache"}}}
	s := guangyaService(db, p)
	for range 2 {
		s.process(p, task)
		current := read(t, db, task.ID)
		if current.Version != task.Version+1 || current.Status != "downloading" || current.Stage != "" || current.Progress != 42 || p.addCalls.Load() != 0 {
			t.Fatal("正常下载轮询增加了无谓的阶段写入或改变提交状态")
		}
		task = current
	}
}

func TestOfflineReconcileLogsOnlySafeErrorCategories(t *testing.T) {
	const privateText = "private-access-fixture https://private.invalid/video?signature=fixture"
	for _, test := range []struct {
		name, category string
		err            error
	}{
		{"success", "none", nil},
		{"authentication", "authentication", &cloud115.Error{Message: privateText, Auth: true}},
		{"retryable", "retryable", &cloud115.Error{Message: privateText, Retryable: true}},
		{"timeout", "timeout", errors.Join(context.DeadlineExceeded, errors.New(privateText))},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, library, video := setup(t)
			task := newGuangyaTask(t, db, library, video)
			p := &stageFixture{providerFixture: providerFixture{findError: test.err}}
			s := guangyaService(db, p)
			var output bytes.Buffer
			prior := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prior)
			s.process(p, task)
			logs := output.String()
			for _, secret := range []string{"private-access-fixture", "https://private.invalid", "signature=fixture", task.Magnet, task.InfoHash} {
				if strings.Contains(logs, secret) {
					t.Fatal("对账日志泄露了原始错误、凭据、直链或磁力")
				}
			}
			if !strings.Contains(logs, "远端任务对账开始") || !strings.Contains(logs, "远端任务对账完成") || !strings.Contains(logs, `"elapsed_ms":`) || !strings.Contains(logs, `"error_category":"`+test.category+`"`) {
				t.Fatal("对账日志没有安全记录开始、完成、耗时和错误类别")
			}
		})
	}
}

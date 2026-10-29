package offline

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/store"
)

type Service struct {
	db          *store.Store
	provider    func() (cloud115.Provider, string, error)
	ctx         context.Context
	cancel      context.CancelFunc
	wake        chan struct{}
	cleanupWake chan struct{}
	running     sync.Map
	wg          sync.WaitGroup
}

const pollInterval = time.Second
const visibilityGrace = 30 * time.Second

func New(ctx context.Context, db *store.Store, provider func() (cloud115.Provider, string, error)) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{db: db, ctx: ctx, cancel: cancel, provider: provider, wake: make(chan struct{}, 2), cleanupWake: make(chan struct{}, 1)}
	// Two bounded download slots and one cleanup slot. The transport still admits
	// only one request per second; slow I/O no longer occupies the entire account.
	for _, cleanup := range []bool{false, false, true} {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			timer := time.NewTicker(pollInterval)
			defer timer.Stop()
			wake := s.wake
			if cleanup {
				wake = s.cleanupWake
			}
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				case <-wake:
				}
				s.tickQueue(cleanup)
			}
		}()
	}
	return s
}
func (s *Service) Wake() {
	for i := 0; i < 2; i++ {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	select {
	case s.cleanupWake <- struct{}{}:
	default:
	}
}
func (s *Service) Wait()  { s.wg.Wait() }
func (s *Service) Close() { s.cancel(); s.Wait() }
func (s *Service) save(t store.OfflineTask, expected int64) error {
	err := s.db.UpdateOfflineTask(t, expected)
	if err != nil && !errors.Is(err, store.ErrConflict) && s.ctx.Err() == nil {
		slog.Error("保存 115 离线任务状态失败", "task_id", t.ID, "stage", t.Stage, "error", err)
	}
	return err
}
func (s *Service) tick()        { s.tickQueue(false) }
func (s *Service) tickCleanup() { s.tickQueue(true) }
func (s *Service) tickQueue(cleanup bool) {
	p, account, err := s.provider()
	if err != nil {
		if !cleanup {
			if conn, e := s.db.Cloud115Connection(); e == nil {
				if tasks, e := s.db.DueOfflineTasks(conn.AccountID, time.Now().Unix()); e == nil {
					for _, t := range tasks {
						s.fail(t, t.Version, "configuration", &cloud115.Error{Message: "115 凭证无法装载，请检查密钥并重新授权", Auth: true})
					}
				}
			}
		}
		return
	}
	var tasks []store.OfflineTask
	if cleanup {
		tasks, err = s.db.DueOfflineCleanup(account, time.Now().Unix())
	} else {
		tasks, err = s.db.DueOfflineTasks(account, time.Now().Unix())
	}
	if err != nil {
		if s.ctx.Err() == nil {
			slog.Error("读取 115 后台队列失败", "cleanup", cleanup, "error", err)
		}
		return
	}
	for _, t := range tasks {
		if s.ctx.Err() != nil {
			return
		}
		if _, busy := s.running.LoadOrStore(t.ID, true); busy {
			continue
		}
		func() {
			defer s.running.Delete(t.ID)
			// Reload after claiming the task: another slot may have just finished it.
			fresh, err := s.db.OfflineTask(t.ID)
			if err != nil || fresh.Version != t.Version {
				return
			}
			if cleanup {
				s.cleanup(p, fresh)
			} else {
				s.process(p, fresh)
			}
		}()
		return
	}
}
func (s *Service) process(p cloud115.Provider, t store.OfflineTask) {
	started := time.Now()
	due := t.NextPoll
	if due == 0 {
		due = t.UpdatedAt
	}
	if due == 0 {
		due = t.CreatedAt
	}
	slog.Debug("115 后台任务开始", "task_id", t.ID, "status", t.Status, "queue_delay_ms", max(0, time.Since(time.Unix(due, 0)).Milliseconds()))
	defer func() {
		slog.Debug("115 后台阶段耗时", "task_id", t.ID, "status", t.Status, "stage", t.Stage, "elapsed_ms", time.Since(started).Milliseconds())
	}()
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancel()
	expected := t.Version
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	if t.Status != "queued" && t.Status != "submitting" && t.Status != "downloading" && t.Status != "discovering" {
		return
	}
	deadline := t.Deadline
	if deadline == 0 {
		deadline = t.CreatedAt + 24*3600
	}
	if time.Now().Unix() > deadline {
		t.Resume = t.Status
		t.Status = "timed_out"
		t.Stage = "poll"
		t.Error = "离线任务等待超过 24 小时，可重试对账（不自动重新下载）"
		t.Retryable = true
		_ = s.save(t, expected)
		return
	}
	if t.RecoveryResultID != "" {
		s.recoverResult(ctx, p, t)
		return
	}
	if t.Status == "queued" {
		cache, ok := p.(cloud115.CacheProvider)
		if !ok {
			s.fail(t, expected, "configuration", &cloud115.Error{Message: "请更新为 Cookie 连接后提交缓存任务", Auth: true})
			return
		}
		root, err := p.Info(ctx, t.FolderID)
		if err != nil {
			s.fail(t, expected, "cache", err)
			return
		}
		if root.ID != t.FolderID || !root.Directory || root.Name != cloud115.CacheFolderName || root.ParentID != "0" {
			s.fail(t, expected, "cache", &cloud115.Error{Message: "115 缓存根目录身份变化，请重新验证 Cookie 连接"})
			return
		}
		if t.CacheFolderID != "" {
			child, err := p.Info(ctx, t.CacheFolderID)
			if err == nil && (child.ID != t.CacheFolderID || !child.Directory || child.ParentID != t.FolderID || child.Name != "emby-go-"+t.ID) {
				err = &cloud115.Error{Message: "115 任务缓存子目录身份不匹配"}
			}
			if err != nil && !isMissing(err) {
				s.fail(t, expected, "cache", err)
				return
			}
			if isMissing(err) {
				t.CacheFolderID = ""
				if s.save(t, expected) != nil {
					return
				}
				expected++
				t.Version = expected
			}
		}
		if t.CacheFolderID == "" {
			reconcileFolder := t.ExpiresAt != 0
			// Persist expiry before mkdir: even a timeout or cancellation that loses
			// the returned folder ID must leave a recoverable cleanup intent.
			if t.ExpiresAt == 0 {
				t.ExpiresAt = time.Now().Add(cloud115.CacheLifetime).Unix()
				if s.save(t, expected) != nil {
					return
				}
				expected++
				t.Version = expected
			}
			var id string
			var err error
			// A fresh UUID has never attempted mkdir. Only a durable prior intent
			// needs a directory lookup to reconcile an uncertain remote write.
			if reconcileFolder {
				id, err = findTaskCacheFolder(ctx, p, t)
			}
			if err == nil && id == "" {
				id, err = cache.Mkdir(ctx, t.FolderID, "emby-go-"+t.ID)
			}
			if err != nil {
				s.fail(t, expected, "cache", err)
				return
			}
			child, err := p.Info(ctx, id)
			if err != nil {
				s.fail(t, expected, "cache", err)
				return
			}
			if child.ID != id || id == "" || id == "0" || id == t.FolderID || !child.Directory || child.ParentID != t.FolderID || child.Name != "emby-go-"+t.ID {
				s.fail(t, expected, "cache", &cloud115.Error{Message: "115 任务缓存子目录身份不匹配"})
				return
			}
			t.CacheFolderID = id
			if s.save(t, expected) != nil {
				return
			}
			expected++
			t.Version = expected
		}
	}
	if t.Status == "discovering" {
		s.discover(ctx, p, t, nil)
		return
	}
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, expected, "reconcile", err)
		return
	}
	if remote != nil {
		var result cloud115.File
		if remote.Status == 2 {
			if remote.ResultID == "" || remote.ResultID == "0" || remote.ResultID == t.FolderID {
				s.fail(t, expected, "file", &cloud115.Error{Message: "115 未返回独立结果文件或目录，未重新提交", Retryable: true})
				return
			}
			result, err = p.Info(ctx, remote.ResultID)
			if isMissing(err) {
				s.prepareRecovery(ctx, p, t, remote, remote.ResultID)
				return
			}
			if err != nil {
				s.fail(t, expected, "file", err)
				return
			}
			if result.Directory && remote.FolderID != t.CacheFolderID {
				gone, err := resultGone(ctx, p, remote.ResultID)
				if err != nil {
					s.fail(t, expected, "file", err)
					return
				}
				if gone {
					s.prepareRecovery(ctx, p, t, remote, remote.ResultID)
					return
				}
			}
		}
		if t.CacheFolderID != "" && remote.FolderID != t.CacheFolderID {
			s.fail(t, expected, "cache", &cloud115.Error{Message: "该磁力已有任务，但不在本服务缓存子目录；不会使用或删除既有网盘文件"})
			return
		}
		t.Attempts = 0
		t.VisibilityUntil = 0
		t.Error = ""
		t.Stage = ""
		t.Progress = max(0, min(100, remote.Progress))
		t.NextPoll = time.Now().Add(pollInterval).Unix()
		switch remote.Status {
		case -1:
			t.Status = "failed"
			t.Stage = "remote"
			t.Error = "115 离线下载失败（状态 -1），请检查网盘任务或选择其它磁力"
			t.Retryable = false
		case 2:
			t.Status = "discovering"
			t.ResultID = remote.ResultID
			t.NextPoll = 0
		case 0, 1:
			t.Status = "downloading"
		default:
			s.fail(t, expected, "poll", &cloud115.Error{Message: "115 返回未知离线状态"})
			return
		}
		if s.save(t, expected) == nil && t.Status == "discovering" {
			t.Version = expected + 1
			// Keep the durable/CAS boundary, but finish discovery in this same
			// bounded turn using only request-local, freshly fetched metadata.
			s.discover(ctx, p, t, &result)
		}
		return
	}
	if t.Status != "queued" {
		if s.waitForVisibility(t, expected, "reconcile", "任务已提交，等待 115 状态同步") {
			return
		}
		s.fail(t, expected, "reconcile", &cloud115.Error{Message: "暂未找到 115 任务；提交结果可能不明确，未再次提交", Retryable: true})
		return
	}
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	// Persist the uncertain boundary before the remote write. A restart or timeout
	// always reconciles 'submitting'; it never blindly submits the magnet again.
	t.Status = "submitting"
	t.Resume = "submitting"
	if s.save(t, expected) != nil {
		return
	}
	expected++
	t.Version = expected
	folder := t.CacheFolderID
	if folder == "" {
		folder = t.FolderID
	}
	if err = p.Add(ctx, t.Magnet, folder); err != nil {
		s.fail(t, expected, "submit", err)
		return
	}
	t.Status = "downloading"
	t.VisibilityUntil = time.Now().Add(visibilityGrace).Unix()
	t.NextPoll = time.Now().Add(pollInterval).Unix()
	t.Error = ""
	t.Stage = ""
	t.Attempts = 0
	_ = s.save(t, expected)
}

func (s *Service) waitForVisibility(t store.OfflineTask, expected int64, stage, message string) bool {
	if time.Now().Unix() >= t.VisibilityUntil {
		return false
	}
	t.Stage, t.Error, t.Attempts = stage, message, 0
	t.NextPoll = time.Now().Add(pollInterval).Unix()
	_ = s.save(t, expected)
	return true
}

func (s *Service) fail(t store.OfflineTask, expected int64, stage string, err error) {
	if s.ctx.Err() != nil {
		return
	}
	var e *cloud115.Error
	if !errors.As(err, &e) {
		e = &cloud115.Error{Message: "115 网络或文件确认失败", Retryable: true}
	}
	t.Stage = stage
	t.Error = e.Message
	t.Retryable = e.Retryable || e.Auth
	t.Resume = t.Status
	t.Attempts++
	if e.Auth {
		t.Status = "needs_auth"
	} else if !e.Retryable || t.Attempts >= 8 {
		t.Status = "failed"
	} else {
		t.NextPoll = time.Now().Add(time.Duration(min(300, 10*(1<<min(t.Attempts, 5)))) * time.Second).Unix()
	}
	_ = s.save(t, expected)
}
func (s *Service) Cancel(id string, version int64) error {
	t, err := s.db.OfflineTask(id)
	if err != nil {
		return err
	}
	if t.Version != version {
		return errors.New("任务已变化，请刷新后重试")
	}
	if t.CacheExpired(time.Now().Unix()) {
		return errors.New("缓存已到期，正在自动清理，不能取消清理")
	}
	if t.Status != "queued" && t.Status != "submitting" && t.Status != "downloading" && t.Status != "discovering" {
		return errors.New("任务已结束，无需再次取消；不会删除网盘文件")
	}
	t.Resume = t.Status
	t.Status = "cancelled"
	t.Error = "已取消本地等待，不删除或取消 115 网盘任务"
	t.Stage = "cancel"
	t.Retryable = true
	return s.save(t, version)
}
func (s *Service) Retry(id string, version int64) error {
	t, err := s.db.OfflineTask(id)
	if err != nil {
		return err
	}
	if t.Version != version {
		return errors.New("任务已变化，请刷新后重试")
	}
	if t.CacheExpired(time.Now().Unix()) {
		return errors.New("缓存已到期；清理完成后请重新提交磁力下载")
	}
	if !t.Retryable || !(t.Status == "failed" || t.Status == "timed_out" || t.Status == "needs_auth" || t.Status == "cancelled") {
		return errors.New("当前任务不支持重试")
	}
	t.Status = t.Resume
	if t.Status != "queued" && t.Status != "submitting" && t.Status != "downloading" && t.Status != "discovering" {
		t.Status = "downloading"
	}
	t.Error = ""
	t.Stage = ""
	t.Attempts = 0
	t.NextPoll = 0
	t.Deadline = time.Now().Add(24 * time.Hour).Unix()
	err = s.save(t, version)
	if err == nil {
		s.Wake()
	}
	return err
}

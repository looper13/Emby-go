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
	db       *store.Store
	provider func() (cloud115.Provider, string, error)
	ctx      context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	wg       sync.WaitGroup
}

func New(ctx context.Context, db *store.Store, provider func() (cloud115.Provider, string, error)) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{db: db, ctx: ctx, cancel: cancel, provider: provider, wake: make(chan struct{}, 1)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			case <-s.wake:
			}
			s.tick()
		}
	}()
	return s
}
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
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
func (s *Service) tick() {
	p, account, err := s.provider()
	if err != nil {
		if conn, e := s.db.Cloud115Connection(); e == nil {
			if tasks, e := s.db.DueOfflineTasks(conn.AccountID, time.Now().Unix()); e == nil {
				for _, t := range tasks {
					s.fail(t, t.Version, "configuration", &cloud115.Error{Message: "115 凭证无法装载，请检查密钥并重新授权", Auth: true})
				}
			}
		}
		return
	}
	cleanup, err := s.db.DueOfflineCleanup(account, time.Now().Unix())
	if err != nil {
		slog.Error("读取 115 缓存清理队列失败", "error", err)
		return
	}
	for _, t := range cleanup {
		if s.ctx.Err() != nil {
			return
		}
		s.cleanup(p, t)
	}
	tasks, err := s.db.DueOfflineTasks(account, time.Now().Unix())
	if err != nil {
		if s.ctx.Err() == nil {
			slog.Error("读取 115 离线队列失败", "error", err)
		}
		return
	}
	for _, t := range tasks {
		if s.ctx.Err() != nil {
			return
		}
		s.process(p, t)
	}
}
func (s *Service) process(p cloud115.Provider, t store.OfflineTask) {
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
			id, err := findTaskCacheFolder(ctx, p, t)
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
		if t.RecoveryFileID != "" {
			gone, err := resultGone(ctx, p, t.RecoveryFileID)
			if err != nil {
				s.fail(t, expected, "file", err)
				return
			}
			if gone {
				s.recoverDiscovery(ctx, p, t, t.RecoveryFileID)
				return
			}
			t.RecoveryFileID = ""
		}
		if t.CacheFolderID != "" {
			if err := verifyCacheResult(ctx, p, t); err != nil {
				if isMissing(err) {
					s.recoverDiscovery(ctx, p, t, t.ResultID)
					return
				}
				s.fail(t, expected, "cache", err)
				return
			}
		}
		files, err := cloud115.Discover(ctx, p, t.ResultID, t.FolderID)
		if err != nil {
			if isMissing(err) {
				s.recoverDiscovery(ctx, p, t, t.ResultID)
				return
			}
			s.fail(t, expected, "discover", err)
			return
		}
		t.Files = files
		t.Progress = 100
		t.Status = "waiting_selection"
		if len(files) == 1 {
			t.Status = "ready"
		}
		t.Error = ""
		t.Stage = ""
		t.Attempts = 0
		_ = s.save(t, expected)
		return
	}
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, expected, "reconcile", err)
		return
	}
	if remote != nil {
		if remote.Status == 2 {
			if remote.ResultID == "" || remote.ResultID == "0" || remote.ResultID == t.FolderID {
				s.fail(t, expected, "file", &cloud115.Error{Message: "115 未返回独立结果文件或目录，未重新提交", Retryable: true})
				return
			}
			result, err := p.Info(ctx, remote.ResultID)
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
		t.Error = ""
		t.Stage = ""
		t.Progress = max(0, min(100, remote.Progress))
		t.NextPoll = time.Now().Add(time.Duration(min(300, max(20, int((time.Now().Unix()-t.CreatedAt)/60)+20))) * time.Second).Unix()
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
		_ = s.save(t, expected)
		return
	}
	if t.Status != "queued" {
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
	t.NextPoll = time.Now().Add(10 * time.Second).Unix()
	t.Error = ""
	t.Stage = ""
	t.Attempts = 0
	_ = s.save(t, expected)
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

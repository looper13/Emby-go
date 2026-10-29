package offline

import (
	"context"
	"errors"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/store"
)

const maxAutomaticRecoveries = 3

func isMissing(err error) bool {
	var e *cloud115.Error
	return errors.As(err, &e) && e.Missing && !e.Auth
}

// A transport/auth failure is never proof of deletion. For result directories,
// accept only an explicitly empty, completely listed directory as missing.
func resultGone(ctx context.Context, p cloud115.Provider, id string) (bool, error) {
	if id == "" || id == "0" {
		return false, &cloud115.Error{Message: "115 未返回有效结果文件 ID，未重新提交"}
	}
	f, err := p.Info(ctx, id)
	if err != nil {
		if isMissing(err) {
			return true, nil
		}
		return false, err
	}
	if f.ID != id {
		return false, &cloud115.Error{Message: "115 结果文件身份不匹配，未重新提交"}
	}
	if f.Directory {
		files, more, err := p.List(ctx, id, 0)
		if err != nil {
			return false, err
		}
		return len(files) == 0 && !more, nil
	}
	return false, nil
}

func (s *Service) prepareRecovery(ctx context.Context, p cloud115.Provider, t store.OfflineTask, remote *cloud115.RemoteTask, missingID string) {
	if _, ok := p.(cloud115.CacheProvider); !ok {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "请更新为 Cookie 连接后恢复下载", Auth: true})
		return
	}
	if remote == nil || remote.Status != 2 || remote.ResultID == "" || remote.ResultID == "0" || remote.FolderID == "" || missingID == t.FolderID || missingID == "0" || remote.FolderID != t.CacheFolderID && missingID != remote.ResultID {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "未确认已完成记录与缺失文件的关系，未重新提交", Retryable: true})
		return
	}
	if t.Recoveries >= maxAutomaticRecoveries {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "文件连续失效，自动重新下载已达 3 次，请检查网盘任务"})
		return
	}
	gone, err := resultGone(ctx, p, missingID)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	if !gone {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "文件状态已变化，尚未确认删除，未重新提交", Retryable: true})
		return
	}
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	// Persist both record identity and deletion evidence before any remote write.
	// Keep the original expiry; repair must not turn the 24h cache into a lease.
	t.RecoveryFileID, t.RecoveryResultID, t.RecoveryFolderID = missingID, remote.ResultID, remote.FolderID
	t.RecoveryRemoved = false
	t.Recoveries++
	t.Status, t.Resume, t.Stage = "queued", "queued", "recover"
	t.Error = "115 结果文件已不存在，正在自动重新下载"
	t.Files, t.ResultID, t.Progress = []cloud115.File{}, "", 0
	t.Attempts, t.NextPoll = 0, 0
	_ = s.save(t, t.Version)
}

func (s *Service) recoverResult(ctx context.Context, p cloud115.Provider, t store.OfflineTask) {
	cache, ok := p.(cloud115.CacheProvider)
	if !ok {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "请更新 Cookie 后恢复下载", Auth: true})
		return
	}
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	clearIntent := func(status string) {
		t.RecoveryFileID, t.RecoveryResultID, t.RecoveryFolderID = "", "", ""
		t.RecoveryRemoved = false
		t.Status, t.Resume, t.Stage, t.Error = status, "", "", ""
		t.NextPoll, t.Attempts = 0, 0
		_ = s.save(t, t.Version)
	}
	if remote == nil {
		// Only a complete absent query opens the boundary for another Add.
		clearIntent("queued")
		return
	}
	if remote.Status != 2 || remote.ResultID != t.RecoveryResultID || remote.FolderID != t.RecoveryFolderID {
		// Another download replaced the old record. Reconcile it, never remove it.
		clearIntent("downloading")
		return
	}
	gone, err := resultGone(ctx, p, t.RecoveryFileID)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	if !gone {
		clearIntent("downloading")
		return
	}
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	if t.RecoveryRemoved {
		s.fail(t, t.Version, "recover", &cloud115.Error{Message: "失效记录仍未从 115 消失，等待对账后重新下载，未重复提交", Retryable: true})
		return
	}
	latest, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	if latest == nil {
		clearIntent("queued")
		return
	}
	if latest.Status != 2 || latest.ResultID != t.RecoveryResultID || latest.FolderID != t.RecoveryFolderID {
		clearIntent("downloading")
		return
	}
	// Remove only the unchanged completed record whose result is proven gone.
	// RemoveTask uses flag=0; recovery never deletes any cloud files/directories.
	t.Stage, t.Error = "recover", "正在移除已失效的 115 离线记录"
	if s.save(t, t.Version) != nil {
		return
	}
	t.Version++
	if err := cache.RemoveTask(ctx, t.InfoHash); err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	t.Error = "失效记录已移除，等待对账后重新下载"
	t.RecoveryRemoved = true
	t.NextPoll = time.Now().Add(5 * time.Second).Unix()
	t.Attempts = 0
	_ = s.save(t, t.Version)
}

func (s *Service) recoverDiscovery(ctx context.Context, p cloud115.Provider, t store.OfflineTask, missingID string) {
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	if remote == nil {
		// Discovering/ready already observed a completed result. A complete absent
		// task query plus proven missing files permits repair, unlike submitting.
		remote = &cloud115.RemoteTask{Status: 2, ResultID: t.ResultID, FolderID: t.CacheFolderID}
	} else if remote.Status == 0 || remote.Status == 1 || t.ResultID != "" && remote.ResultID != t.ResultID {
		// The failed playback/discovery belongs to an older result, not the new
		// download now associated with this hash. Follow the replacement instead.
		t.Status, t.Resume, t.Stage, t.Error = "downloading", "", "", ""
		t.RecoveryFileID, t.ResultID = "", ""
		t.Files, t.NextPoll = []cloud115.File{}, 0
		_ = s.save(t, t.Version)
		return
	}
	s.prepareRecovery(ctx, p, t, remote, missingID)
}

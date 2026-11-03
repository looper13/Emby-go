package offline

import (
	"context"
	"errors"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

const maxAutomaticRecoveries = 3

func isMissing(err error) bool {
	var e *clouddrive.Error
	return errors.As(err, &e) && e.Missing && !e.Auth
}

// 网络或鉴权失败不能证明文件被删除；目录缺失必须经过完整列举且明确为空。
func resultGone(ctx context.Context, p clouddrive.Provider, id string, names ...string) (bool, error) {
	brand := cacheBrand(names)
	if id == "" || id == "0" {
		return false, &clouddrive.Error{Message: brand + " 未返回有效结果文件 ID，未重新提交"}
	}
	f, err := p.Info(ctx, id)
	if err != nil {
		if isMissing(err) {
			return true, nil
		}
		return false, err
	}
	if f.ID != id {
		return false, &clouddrive.Error{Message: brand + " 结果文件身份不匹配，未重新提交"}
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

func (s *Service) prepareRecovery(ctx context.Context, p clouddrive.Provider, t store.OfflineTask, remote *clouddrive.RemoteTask, missingID string) {
	if _, ok := p.(clouddrive.CacheProvider); !ok {
		message := "请更新为 Cookie 连接后恢复下载"
		if s.brand() != "115" {
			message = "请更新" + s.brand() + "凭据后恢复下载"
		}
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: message, Auth: true})
		return
	}
	if remote == nil || remote.Status != 2 || remote.ResultID == "" || remote.ResultID == "0" || remote.FolderID == "" || missingID == t.FolderID || missingID == "0" || remote.FolderID != t.CacheFolderID && missingID != remote.ResultID {
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: "未确认已完成记录与缺失文件的关系，未重新提交", Retryable: true})
		return
	}
	if t.Recoveries >= maxAutomaticRecoveries {
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: "文件连续失效，自动重新下载已达 3 次，请检查网盘任务"})
		return
	}
	gone, err := resultGone(ctx, p, missingID, s.brand())
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	if !gone {
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: "文件状态已变化，尚未确认删除，未重新提交", Retryable: true})
		return
	}
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	// 远程写入前持久化记录身份和删除证据，保留原到期时间，避免恢复延长缓存寿命。
	t.RecoveryFileID, t.RecoveryResultID, t.RecoveryFolderID = missingID, remote.ResultID, remote.FolderID
	t.RecoveryRemoved = false
	t.Recoveries++
	t.Status, t.Resume, t.Stage = "queued", "queued", "recover"
	t.Error = s.brand() + " 结果文件已不存在，正在自动重新下载"
	t.Files, t.ResultID, t.Progress = []clouddrive.File{}, "", 0
	t.Attempts, t.NextPoll = 0, 0
	_ = s.save(t, t.Version)
}

func (s *Service) recoverResult(ctx context.Context, p clouddrive.Provider, t store.OfflineTask) {
	cache, ok := p.(clouddrive.CacheProvider)
	if !ok {
		message := "请更新 Cookie 后恢复下载"
		if s.brand() != "115" {
			message = "请更新" + s.brand() + "凭据后恢复下载"
		}
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: message, Auth: true})
		return
	}
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	remote = provenMissingTaskFolder(p, t, remote)
	clearIntent := func(status string) {
		t.RecoveryFileID, t.RecoveryResultID, t.RecoveryFolderID = "", "", ""
		t.RecoveryRemoved = false
		t.VisibilityUntil = 0
		t.Status, t.Resume, t.Stage, t.Error = status, "", "", ""
		t.NextPoll, t.Attempts = 0, 0
		_ = s.save(t, t.Version)
	}
	if remote == nil {
		// 只有完整查询明确不存在任务后，才允许再次提交磁力。
		clearIntent("queued")
		return
	}
	if remote.Status != 2 || remote.ResultID != t.RecoveryResultID || remote.FolderID != t.RecoveryFolderID {
		// 新下载替换了旧记录，只继续对账，不删除新记录。
		clearIntent("downloading")
		return
	}
	gone, err := resultGone(ctx, p, t.RecoveryFileID, s.brand())
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
		if s.waitForVisibility(t, t.Version, "recover", "失效记录已移除，等待 "+s.brand()+" 状态同步后重新下载") {
			return
		}
		s.fail(t, t.Version, "recover", &clouddrive.Error{Message: "失效记录仍未从 " + s.brand() + " 消失，等待对账后重新下载，未重复提交", Retryable: true})
		return
	}
	latest, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	latest = provenMissingTaskFolder(p, t, latest)
	if latest == nil {
		clearIntent("queued")
		return
	}
	if latest.Status != 2 || latest.ResultID != t.RecoveryResultID || latest.FolderID != t.RecoveryFolderID || latest.RemoteID != remote.RemoteID {
		clearIntent("downloading")
		return
	}
	// 只移除已确认结果缺失且身份未变的完成记录，恢复过程不删除网盘文件或目录。
	t.Stage, t.Error = "recover", "正在移除已失效的 "+s.brand()+" 离线记录"
	if s.save(t, t.Version) != nil {
		return
	}
	t.Version++
	if err := removeTaskRecord(ctx, cache, t.InfoHash, *latest); err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	t.Error = "失效记录已移除，等待对账后重新下载"
	t.RecoveryRemoved = true
	t.VisibilityUntil = time.Now().Add(visibilityGrace).Unix()
	t.NextPoll = time.Now().Add(pollInterval).Unix()
	t.Attempts = 0
	_ = s.save(t, t.Version)
}

func (s *Service) recoverDiscovery(ctx context.Context, p clouddrive.Provider, t store.OfflineTask, missingID string) {
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		s.fail(t, t.Version, "recover", err)
		return
	}
	remote = provenMissingTaskFolder(p, t, remote)
	if remote == nil {
		// 已观察到完成结果；完整任务查询缺失加上文件删除证据才允许修复。
		remote = &clouddrive.RemoteTask{Status: 2, ResultID: t.ResultID, FolderID: t.CacheFolderID}
	} else if remote.Status == 0 || remote.Status == 1 || t.ResultID != "" && remote.ResultID != t.ResultID {
		// 失效来源属于旧结果，继续等待当前磁力的新下载，不发布旧来源。
		t.Status, t.Resume, t.Stage, t.Error = "downloading", "", "", ""
		t.RecoveryFileID, t.ResultID = "", ""
		t.Files, t.NextPoll = []clouddrive.File{}, 0
		_ = s.save(t, t.Version)
		return
	}
	s.prepareRecovery(ctx, p, t, remote, missingID)
}

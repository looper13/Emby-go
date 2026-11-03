package offline

import (
	"context"
	"strings"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

func validRemoteTaskID(id string) bool {
	return id != "" && id != "0" && len(id) <= 512 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\r\n\t\x00/\\")
}

func remoteTaskStage(p clouddrive.Provider) string {
	if _, ok := p.(clouddrive.TaskIDProvider); ok {
		return "poll"
	}
	return "reconcile"
}

func validPrivateCacheIntent(t store.OfflineTask) bool {
	return t.ID != "" && t.AccountID != "" && t.FolderID != "" && t.FolderID != "0" &&
		t.CacheFolderID != "" && t.CacheFolderID != "0" && t.CacheFolderID != t.FolderID &&
		t.CreatedAt > 0 && t.ExpiresAt > 0
}

func hasSubmittedTaskIntent(t store.OfflineTask) bool {
	if t.RecoveryResultID != "" {
		return true
	}
	status := t.Status
	switch status {
	case "failed", "timed_out", "needs_auth", "cancelled", "cleanup_pending":
		status = t.Resume
	}
	switch status {
	case "submitting", "downloading", "discovering", "ready", "waiting_selection":
		return true
	}
	// 老版本清理状态未保存恢复阶段，已绑定的结果仍可证明曾经提交。
	return t.Status == "cleanup_pending" && t.Resume == "" && (t.ResultID != "" || len(t.Files) != 0)
}

// 确定性标识只用于定位；生成前核验磁力与持久 hash，不能覆盖已有的错误标识。
func stableTaskID(p clouddrive.Provider, t store.OfflineTask) (string, bool, error) {
	stable, ok := p.(clouddrive.StableTaskIDProvider)
	if !ok {
		return "", false, nil
	}
	_, hash, err := clouddrive.NormalizeMagnet(t.Magnet)
	if err != nil || !strings.EqualFold(hash, t.InfoHash) {
		return "", true, &clouddrive.Error{Message: "磁力与保存的 InfoHash 不一致，未查询或提交任务", Retryable: true}
	}
	id, err := stable.TaskIDForHash(hash)
	if err != nil || !validRemoteTaskID(id) {
		return "", true, &clouddrive.Error{Message: "无法根据磁力生成可靠的远端任务标识", Retryable: true}
	}
	if t.RemoteTaskID != "" && t.RemoteTaskID != id {
		return id, true, &clouddrive.Error{Message: "保存的远端任务标识与磁力不匹配，未使用或覆盖该记录", Retryable: true}
	}
	return id, true, nil
}

func (s *Service) retryWithoutTaskID(t store.OfflineTask) bool {
	if s.provider != nil {
		p, account, err := s.provider()
		if err == nil {
			if account != t.AccountID {
				return false
			}
			if _, direct := p.(clouddrive.TaskIDProvider); direct && t.RecoveryResultID != "" && t.RecoveryFolderID != t.CacheFolderID {
				return true
			}
			if id, stable, err := stableTaskID(p, t); stable {
				if err != nil {
					// 错误标识只能在用户明确重新添加时清除，不可自动冒用。
					return true
				}
				if t.RemoteTaskID == id {
					return false
				}
				// 老的已提交任务可本地补标识后继续查询，尚未提交的任务才重新添加。
				return !hasSubmittedTaskIntent(t) || !validPrivateCacheIntent(t)
			}
			_, direct := p.(clouddrive.TaskIDProvider)
			return direct && !validRemoteTaskID(t.RemoteTaskID)
		}
	}
	// 凭据尚未装载时仍能保存用户的重试意图，下一轮队列再检查连接。
	return !validRemoteTaskID(t.RemoteTaskID) && strings.HasPrefix(t.AccountID, store.CloudGuangyaAccount(""))
}

func sameCancelledSubmission(submitted, current store.OfflineTask) bool {
	// 只允许提交边界后紧接的取消。重试或新提交即使沿用同一缓存也会推进版本。
	return current.Version == submitted.Version+1 && current.Status == "cancelled" && current.Resume == "submitting" && current.Stage == "cancel" &&
		submitted.Status == "submitting" && submitted.Resume == "submitting" && submitted.RemoteTaskID == "" && current.RemoteTaskID == "" &&
		current.ID == submitted.ID && current.AccountID == submitted.AccountID && current.InfoHash == submitted.InfoHash && current.Magnet == submitted.Magnet &&
		current.FolderID == submitted.FolderID && current.CacheFolderID == submitted.CacheFolderID &&
		current.CreatedAt == submitted.CreatedAt && current.ExpiresAt == submitted.ExpiresAt && current.Deadline == submitted.Deadline &&
		current.ResultID == submitted.ResultID && current.Recoveries == submitted.Recoveries &&
		current.RecoveryFileID == submitted.RecoveryFileID && current.RecoveryResultID == submitted.RecoveryResultID && current.RecoveryFolderID == submitted.RecoveryFolderID && current.RecoveryRemoved == submitted.RecoveryRemoved
}

// 只按明确身份查询；确定性标识可本地推导，其他网盘缺标识时禁止扫描历史。
func lookupRemoteTask(ctx context.Context, p clouddrive.Provider, task store.OfflineTask) (*clouddrive.RemoteTask, error) {
	byID, ok := p.(clouddrive.TaskIDProvider)
	if !ok {
		return p.Find(ctx, task.InfoHash)
	}
	id := task.RemoteTaskID
	if derived, stable, err := stableTaskID(p, task); stable {
		if err != nil {
			return nil, err
		}
		if !hasSubmittedTaskIntent(task) || !validPrivateCacheIntent(task) {
			return nil, &clouddrive.Error{Message: "未确认本任务提交与专属缓存的关系，未查询远端记录", Retryable: true}
		}
		id = derived
	}
	if !validRemoteTaskID(id) {
		return nil, &clouddrive.Error{Message: "本任务缺少远端任务标识，无法精确查询；不会搜索历史或自动重复提交", Retryable: true}
	}
	remote, err := byID.FindTask(ctx, task.InfoHash, id)
	if err == nil && remote != nil && (remote.RemoteID != id || remote.Hash != "" && !strings.EqualFold(remote.Hash, task.InfoHash)) {
		return nil, &clouddrive.Error{Message: "远端返回了其他任务身份，未使用该记录", Retryable: true}
	}
	return remote, err
}

// provenMissingTaskFolder 只复用此前已绑定结果的持久缓存归属证明。
// 新任务、陌生结果和没有明确删除证据的响应不能据此猜测保存目录。
func provenMissingTaskFolder(p clouddrive.Provider, task store.OfflineTask, remote *clouddrive.RemoteTask) *clouddrive.RemoteTask {
	if remote == nil || !allowsUnknownPendingFolder(p) || remote.Status != 2 || !remote.ResultMissing || remote.FolderID != "" {
		return remote
	}
	if task.CacheFolderID == "" || task.CacheFolderID == "0" || task.CacheFolderID == task.FolderID || remote.ResultID == "" || remote.ResultID == "0" || remote.ResultID == task.FolderID {
		return remote
	}
	knownResult := remote.ResultID == task.ResultID
	knownRecovery := remote.ResultID == task.RecoveryResultID && task.RecoveryFolderID == task.CacheFolderID
	if !knownResult && !knownRecovery {
		return remote
	}
	copy := *remote
	copy.FolderID = task.CacheFolderID
	return &copy
}

func removeTaskRecord(ctx context.Context, p clouddrive.CacheProvider, hash string, remote clouddrive.RemoteTask) error {
	if conditional, ok := p.(clouddrive.ConditionalTaskRemover); ok {
		return conditional.RemoveTaskIf(ctx, hash, remote)
	}
	if _, ok := p.(clouddrive.TaskIDProvider); ok {
		return &clouddrive.Error{Message: "当前连接无法按任务标识安全移除记录，未搜索或删除其他任务", Retryable: true}
	}
	return p.RemoveTask(ctx, hash)
}

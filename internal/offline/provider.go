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

func (s *Service) retryWithoutTaskID(t store.OfflineTask) bool {
	if validRemoteTaskID(t.RemoteTaskID) {
		return false
	}
	if s.provider != nil {
		p, account, err := s.provider()
		if err == nil {
			_, direct := p.(clouddrive.TaskIDProvider)
			return direct && account == t.AccountID
		}
	}
	// 凭据尚未装载时仍能保存用户的重试意图，下一轮队列再检查连接。
	return strings.HasPrefix(t.AccountID, store.CloudGuangyaAccount(""))
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

// 支持任务 ID 的网盘只能查询本次提交，缺失标识时禁止回退扫描磁力历史。
func lookupRemoteTask(ctx context.Context, p clouddrive.Provider, task store.OfflineTask) (*clouddrive.RemoteTask, error) {
	byID, ok := p.(clouddrive.TaskIDProvider)
	if !ok {
		return p.Find(ctx, task.InfoHash)
	}
	if !validRemoteTaskID(task.RemoteTaskID) {
		return nil, &clouddrive.Error{Message: "本任务缺少远端任务标识，无法精确查询；不会搜索历史或自动重复提交", Retryable: true}
	}
	remote, err := byID.FindTask(ctx, task.InfoHash, task.RemoteTaskID)
	if err == nil && remote != nil && (remote.RemoteID != task.RemoteTaskID || remote.Hash != "" && remote.Hash != task.InfoHash) {
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

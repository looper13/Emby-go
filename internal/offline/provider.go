package offline

import (
	"context"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

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
	return p.RemoveTask(ctx, hash)
}

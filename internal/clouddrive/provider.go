// Package clouddrive 定义共享的网盘文件和离线任务接口。
package clouddrive

import (
	"context"
	"time"
)

const CacheFolderName = "缓存-24h自动删除"
const CacheLifetime = 24 * time.Hour

type RemoteTask struct {
	Hash             string
	Status, Progress int
	RemoteID         string
	ResultID         string
	FolderID         string
	ResultMissing    bool
}

type File struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA1      string `json:"sha1"`
	PickCode  string `json:"pick_code"`
	Directory bool   `json:"directory"`
	Duration  int64  `json:"duration"`
}

type Provider interface {
	Check(context.Context, string) (string, error)
	Find(context.Context, string) (*RemoteTask, error)
	Add(context.Context, string, string) error
	Info(context.Context, string) (File, error)
	List(context.Context, string, int) ([]File, bool, error)
	URL(context.Context, string, string) (string, error)
}

// CacheProvider 只管理经身份核验的服务专属缓存目录和离线记录。
type CacheProvider interface {
	Provider
	Mkdir(context.Context, string, string) (string, error)
	Delete(context.Context, string) error
	RemoveTask(context.Context, string) error
}

// TaskIDProvider 直接提交新任务，后续仅按保存的远程任务 ID 查询本次下载。
type TaskIDProvider interface {
	AddTask(context.Context, string, string) (string, error)
	FindTask(context.Context, string, string) (*RemoteTask, error)
}

// StableTaskIDProvider 的远端身份由磁力确定，可在提交前保存，重启后精确确认结果。
// 返回身份只用于定位，不能代替提交成功或结果缓存归属的核验。
type StableTaskIDProvider interface {
	TaskIDProvider
	TaskIDForHash(string) (string, error)
}

// PendingTaskFolderUnknown 表示上游未完成任务可能暂不提供保存目录。
// 缺失目录仅允许等待对账，不能用于放宽完成结果或删除操作的归属核验。
type PendingTaskFolderUnknown interface {
	PendingTaskFolderUnknown() bool
}

// ConditionalTaskRemover 只移除身份仍与已核验记录完全一致的离线任务。
type ConditionalTaskRemover interface {
	RemoveTaskIf(context.Context, string, RemoteTask) error
}

type Error struct {
	Message                  string
	Auth, Retryable, Missing bool
	// NotSubmitted 只用于已确认失败发生在远程任务创建请求之前的情况。
	NotSubmitted bool
}

func (e *Error) Error() string { return e.Message }

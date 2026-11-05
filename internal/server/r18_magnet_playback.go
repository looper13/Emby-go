package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
	"github.com/gin-gonic/gin"
)

const r18MagnetPollInterval = 250 * time.Millisecond
const r18MagnetPlaybackProofKey = "r18MagnetPlaybackProof"

type r18MagnetPlaybackScope struct {
	Library    store.Library
	Video      string
	User       int64
	Provider   string
	Connection store.Cloud115Connection
}

type r18MagnetPlaybackProof struct {
	Scope  r18MagnetPlaybackScope
	Task   store.OfflineTask
	File   cloud115.File
	FileID string
	Hash   string
}

func r18MagnetPlaybackError(c *gin.Context, status int, code, message string) {
	// 原生播放器通常只显示状态码，固定原因必须同步落日志；不记录请求 URL 或票据。
	attrs := []any{"method", c.Request.Method, "status", status, "error_code", code, "reason", message}
	if stage := c.GetString("r18MagnetPlaybackErrorStage"); stage != "" {
		attrs = append(attrs, "stage", stage)
	}
	slog.WarnContext(c.Request.Context(), cloudName(requestCloudProvider(c))+"磁力播放失败", attrs...)
	c.Header("Cache-Control", "no-store")
	if status == http.StatusServiceUnavailable {
		c.Header("Retry-After", "3")
	}
	c.JSON(status, gin.H{"error": message, "ErrorCode": code})
}

func r18MagnetCloudErrorCode(provider, suffix string) string {
	if provider == "guangya" {
		return "Guangya" + suffix
	}
	return "Cloud115" + suffix
}

func r18MagnetWaitStopped(c *gin.Context, ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, "MagnetPreparing", "所选网盘版本仍在准备，后台任务会继续，请稍后重试播放")
	}
	return true
}

// 播放等待不缓存授权结果；下载过程中撤销权限或更换连接都必须及时停止。
func (a *App) checkR18MagnetScope(c *gin.Context, scope r18MagnetPlaybackScope) bool {
	user, err := a.db.User(scope.User)
	if err != nil || user.Disabled || a.db.UserID() != scope.User {
		r18MagnetPlaybackError(c, http.StatusUnauthorized, "PlaybackUnauthorized", "播放账号已失效，请重新登录")
		return false
	}
	if !a.db.CanAccessLibrary(scope.Library.ID) {
		r18MagnetPlaybackError(c, http.StatusNotFound, "PlaybackUnavailable", "资料库不存在或无权播放")
		return false
	}
	library, err := a.db.Library(scope.Library.ID)
	if err != nil || library.SourceType != store.SourceR18 || library.Revision != scope.Library.Revision {
		r18MagnetPlaybackError(c, http.StatusConflict, "PlaybackChanged", "资料库配置已变化，请刷新后重新播放")
		return false
	}
	connection, err := a.offlineConnection(scope.Provider)
	if err != nil || connection.AccountID == "" || connection.FolderID == "" || connection.FolderID == "0" {
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, r18MagnetCloudErrorCode(scope.Provider, "Unavailable"), cloudName(scope.Provider)+"连接不可用，请管理员检查配置")
		return false
	}
	if connection.AccountID != scope.Connection.AccountID || connection.FolderID != scope.Connection.FolderID || connection.Revision != scope.Connection.Revision {
		r18MagnetPlaybackError(c, http.StatusConflict, r18MagnetCloudErrorCode(scope.Provider, "ConnectionChanged"), cloudName(scope.Provider)+"账号或缓存配置已变化，请重新播放")
		return false
	}
	return true
}

func (a *App) r18MagnetBinding(scope r18MagnetPlaybackScope, hash string) (store.OfflineBinding, bool, error) {
	bindings, err := a.db.OfflineBindings(scope.Library, scope.Video, scope.Connection.AccountID)
	if err != nil {
		return store.OfflineBinding{}, false, err
	}
	for _, binding := range bindings {
		if strings.EqualFold(binding.Task.InfoHash, hash) {
			return binding, true, nil
		}
	}
	return store.OfflineBinding{}, false, nil
}

func validR18MagnetTask(scope r18MagnetPlaybackScope, task store.OfflineTask, hash string) bool {
	_, normalizedHash, err := cloud115.NormalizeMagnet(task.Magnet)
	return err == nil && normalizedHash == hash && strings.EqualFold(task.InfoHash, hash) &&
		task.ID != "" && task.AccountID == scope.Connection.AccountID && task.FolderID != "" && task.FolderID != "0" &&
		(task.FolderID == scope.Connection.FolderID || task.Status == "expired") &&
		task.CreatedAt > 0 && task.Version > 0 &&
		(task.CacheFolderID == "" || task.CacheFolderID != "0" && task.CacheFolderID != task.FolderID)
}

func (a *App) readR18MagnetBinding(c *gin.Context, scope r18MagnetPlaybackScope, hash string) (store.OfflineBinding, bool, bool) {
	binding, found, err := a.r18MagnetBinding(scope, hash)
	if err != nil {
		r18MagnetPlaybackError(c, http.StatusInternalServerError, "MagnetStateUnavailable", "读取所选网盘版本失败")
		return binding, false, false
	}
	if found && !validR18MagnetTask(scope, binding.Task, hash) {
		r18MagnetPlaybackError(c, http.StatusConflict, "MagnetIdentityChanged", "所选任务的账号、磁力或缓存归属已变化，未使用该任务")
		return binding, found, false
	}
	return binding, found, true
}

func (a *App) resolveR18PlaybackMagnet(c *gin.Context, ctx context.Context, scope r18MagnetPlaybackScope, video r18.Video, hash, resourceID string) (string, string, bool) {
	client, err := a.r18Client(scope.Library)
	if err != nil {
		r18MagnetPlaybackError(c, http.StatusBadGateway, "MagnetResourceUnavailable", "无法核验所选磁力资源，请稍后重试")
		return "", "", false
	}
	if resourceID == "" {
		resourceID = "btih-" + hash
	}
	resource, err := client.Resource(ctx, video, resourceID)
	if r18MagnetWaitStopped(c, ctx) {
		return "", "", false
	}
	if errors.Is(err, sql.ErrNoRows) {
		r18MagnetPlaybackError(c, http.StatusNotFound, "MagnetResourceUnavailable", "所选磁力不存在、已下架或不属于当前影片")
		return "", "", false
	}
	if err != nil {
		r18MagnetPlaybackError(c, http.StatusBadGateway, "MagnetResourceUnavailable", "所选磁力查询失败，请稍后重试")
		return "", "", false
	}
	magnet, normalizedHash, err := cloud115.NormalizeMagnet(resource.Magnet)
	number := func(raw string) string {
		return strings.ToUpper(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(raw)))
	}
	if err != nil || resource.ID != resourceID || normalizedHash != hash || resource.Number != "" && number(resource.Number) != number(video.Number) {
		r18MagnetPlaybackError(c, http.StatusNotFound, "MagnetResourceUnavailable", "所选磁力身份或影片归属不一致，未创建下载")
		return "", "", false
	}
	if !a.checkR18MagnetScope(c, scope) {
		return "", "", false
	}
	return magnet, resource.ID, true
}

func r18MagnetSelectedFile(binding store.OfflineBinding) (cloud115.File, bool) {
	if binding.FileID != "" {
		for _, file := range binding.Task.Files {
			if file.ID == binding.FileID && cloud115.VideoFile(file) {
				return file, true
			}
		}
		return cloud115.File{}, false
	}
	files := make([]cloud115.File, 0, len(binding.Task.Files))
	for _, file := range binding.Task.Files {
		if cloud115.VideoFile(file) {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return cloud115.File{}, false
	}
	// 一条磁力只呈现一个版本；大小相同时按路径和文件身份稳定选择。
	sort.Slice(files, func(i, j int) bool {
		if files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		left, right := files[i].Path, files[j].Path
		if left == "" {
			left = files[i].Name
		}
		if right == "" {
			right = files[j].Name
		}
		if left != right {
			return left < right
		}
		return files[i].ID < files[j].ID
	})
	return files[0], true
}

func sameR18MagnetSelectedFile(left, right cloud115.File) bool {
	return left.ID == right.ID && left.ParentID == right.ParentID && left.Name == right.Name &&
		left.Size == right.Size && left.SHA1 == right.SHA1 && left.PickCode == right.PickCode && left.Directory == right.Directory
}

func sameR18MagnetTaskLifecycle(before, current store.OfflineTask) bool {
	return current.Version >= before.Version && current.ID == before.ID && current.AccountID == before.AccountID &&
		current.FolderID == before.FolderID && current.CacheFolderID == before.CacheFolderID &&
		current.CreatedAt == before.CreatedAt && current.ExpiresAt == before.ExpiresAt &&
		current.ResultID == before.ResultID && current.RemoteTaskID == before.RemoteTaskID && current.Recoveries == before.Recoveries
}

func r18MagnetStoppedTask(c *gin.Context, task store.OfflineTask) bool {
	if task.Status != "expired" && task.CacheExpired(time.Now().Unix()) {
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, "MagnetCacheExpired", "所选网盘缓存已到期，后台正在清理，请稍后重新播放")
		return true
	}
	switch task.Status {
	case "needs_auth":
		c.Set("r18MagnetPlaybackErrorStage", task.Stage)
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, r18MagnetCloudErrorCode(requestCloudProvider(c), "AuthorizationRequired"), "所选网盘任务因授权问题暂停，请管理员检查网盘连接后在网页重试该任务")
		return true
	case "failed", "timed_out", "cancelled":
		r18MagnetPlaybackError(c, http.StatusConflict, "MagnetTaskStopped", "所选网盘任务已失败、超时或取消，请在网页检查该任务或选择其它磁力")
		return true
	}
	return false
}

// validateR18MagnetPlayback 在直链取得后复核准备证明，调用方通过后才能发送 302。
func (a *App) validateR18MagnetPlayback(c *gin.Context) bool {
	value, found := c.Get(r18MagnetPlaybackProofKey)
	if !found {
		return true
	}
	proof, ok := value.(r18MagnetPlaybackProof)
	if !ok || r18MagnetWaitStopped(c, c.Request.Context()) {
		return false
	}
	if !a.checkR18MagnetScope(c, proof.Scope) {
		return false
	}
	binding, found, valid := a.readR18MagnetBinding(c, proof.Scope, proof.Hash)
	if !valid {
		return false
	}
	chosen, playable := r18MagnetSelectedFile(binding)
	if !found || binding.Task.Status != "ready" || binding.Task.CacheExpired(time.Now().Unix()) ||
		!sameR18MagnetTaskLifecycle(proof.Task, binding.Task) || binding.FileID != proof.FileID ||
		!playable || !sameR18MagnetSelectedFile(chosen, proof.File) {
		r18MagnetPlaybackError(c, http.StatusConflict, "MagnetSelectionChanged", "所选播放文件或缓存状态已变化，请重新播放")
		return false
	}
	return true
}

// prepareR18MagnetFile 只准备实际所选的磁力；后台队列独立于播放器请求生命周期。
func (a *App) prepareR18MagnetFile(c *gin.Context, library store.Library, video r18.Video, source string) (cloud115.Provider, cloud115.File, bool) {
	provider, hash, valid := parseR18MagnetSourceID(source)
	itemLibrary, itemVideo, validItem := parseR18ItemID(c.Param("id"))
	if !valid || !validItem || itemLibrary != library.ID || itemVideo != video.ID {
		r18MagnetPlaybackError(c, http.StatusNotFound, "MagnetResourceUnavailable", "所选磁力版本不存在或不属于当前影片")
		return nil, cloud115.File{}, false
	}
	c.Set("offlineProvider", provider)
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Header("Allow", "GET, HEAD")
		r18MagnetPlaybackError(c, http.StatusMethodNotAllowed, "PlaybackMethodUnsupported", "请通过 GET 播放所选磁力版本")
		return nil, cloud115.File{}, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	if r18MagnetWaitStopped(c, ctx) {
		return nil, cloud115.File{}, false
	}
	connection, err := a.offlineConnection(provider)
	if err != nil || connection.AccountID == "" || connection.AccountID == store.CloudGuangyaAccount("") || connection.FolderID == "" || connection.FolderID == "0" {
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, r18MagnetCloudErrorCode(provider, "Unavailable"), "请管理员先配置"+cloudName(provider)+"连接")
		return nil, cloud115.File{}, false
	}
	scope := r18MagnetPlaybackScope{Library: library, Video: video.ID, User: a.db.UserID(), Provider: provider, Connection: connection}
	if !a.checkR18MagnetScope(c, scope) {
		return nil, cloud115.File{}, false
	}
	binding, found, valid := a.readR18MagnetBinding(c, scope, hash)
	if !valid {
		return nil, cloud115.File{}, false
	}
	if found && r18MagnetStoppedTask(c, binding.Task) {
		return nil, cloud115.File{}, false
	}
	if c.Request.Method == http.MethodHead && (!found || binding.Task.Status != "ready" || binding.Task.CacheExpired(time.Now().Unix()) || binding.FileID == "") {
		// 部分播放器先 HEAD，失败便不再 GET。只核验资源，准备仍由真正播放触发。
		if _, _, ok := a.resolveR18PlaybackMagnet(c, ctx, scope, video, hash, binding.ResourceID); !ok {
			return nil, cloud115.File{}, false
		}
		current, currentFound, currentValid := a.readR18MagnetBinding(c, scope, hash)
		if !currentValid || currentFound && r18MagnetStoppedTask(c, current.Task) {
			return nil, cloud115.File{}, false
		}
		if currentFound {
			switch current.Task.Status {
			case "queued", "submitting", "downloading", "discovering", "waiting_selection", "ready", "expired":
			default:
				r18MagnetPlaybackError(c, http.StatusConflict, "MagnetStateChanged", "所选网盘任务状态不可播放，请刷新后重试")
				return nil, cloud115.File{}, false
			}
		}
		// 磁力大小不一定等于最终所选视频大小，未知长度不能伪报为零或磁力总大小。
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Type", "application/octet-stream")
		c.Status(http.StatusOK)
		return nil, cloud115.File{}, false
	}
	resourceVerified := false
	if !found || binding.Task.Status != "ready" || binding.FileID == "" || binding.Task.CacheExpired(time.Now().Unix()) {
		resourceID := binding.ResourceID
		magnet, resourceID, ok := a.resolveR18PlaybackMagnet(c, ctx, scope, video, hash, resourceID)
		if !ok {
			return nil, cloud115.File{}, false
		}
		resourceVerified = true
		// 创建接口按账号和哈希去重；仅已清理完成的 expired 生命周期可以重新排队。
		if !found || binding.Task.Status == "expired" {
			if _, err = a.db.CreateOfflineTask(connection.AccountID, connection.FolderID, magnet, hash, library, video.ID, resourceID); err != nil {
				if errors.Is(err, store.ErrConflict) {
					r18MagnetPlaybackError(c, http.StatusConflict, "MagnetIdentityChanged", "所选磁力已有缓存归属不同的任务，未创建新绑定，请先完成原任务清理")
					return nil, cloud115.File{}, false
				}
				r18MagnetPlaybackError(c, http.StatusInternalServerError, "MagnetCreateFailed", "保存所选磁力任务失败")
				return nil, cloud115.File{}, false
			}
			binding, found, valid = a.readR18MagnetBinding(c, scope, hash)
			if !valid || !found {
				if valid {
					r18MagnetPlaybackError(c, http.StatusConflict, "MagnetIdentityChanged", "所选任务绑定已变化，请重新播放")
				}
				return nil, cloud115.File{}, false
			}
		}
	}
	p, account, err := a.providerFor(provider)
	if err != nil || p == nil || account != connection.AccountID {
		message := cloudName(provider) + "凭证不可用，请管理员检查配置"
		var loadError *guangyaProviderLoadError
		if errors.As(err, &loadError) {
			c.Set("r18MagnetPlaybackErrorStage", loadError.stage)
			message = loadError.Error()
		}
		r18MagnetPlaybackError(c, http.StatusServiceUnavailable, r18MagnetCloudErrorCode(provider, "Unavailable"), message)
		return nil, cloud115.File{}, false
	}
	if !a.checkR18MagnetScope(c, scope) {
		return nil, cloud115.File{}, false
	}
	queue := a.offlineQueue(provider)
	taskID, createdAt, expiresAt := binding.Task.ID, binding.Task.CreatedAt, binding.Task.ExpiresAt
	woken := false
	ticker := time.NewTicker(r18MagnetPollInterval)
	defer ticker.Stop()
	for {
		if r18MagnetWaitStopped(c, ctx) || !a.checkR18MagnetScope(c, scope) {
			return nil, cloud115.File{}, false
		}
		binding, found, valid = a.readR18MagnetBinding(c, scope, hash)
		if !valid {
			return nil, cloud115.File{}, false
		}
		if !found || binding.Task.ID != taskID || binding.Task.CreatedAt != createdAt || expiresAt != 0 && binding.Task.ExpiresAt != expiresAt {
			r18MagnetPlaybackError(c, http.StatusConflict, "MagnetIdentityChanged", "所选任务生命周期已变化，请重新播放")
			return nil, cloud115.File{}, false
		}
		if expiresAt == 0 {
			expiresAt = binding.Task.ExpiresAt
		}
		if binding.Task.CacheExpired(time.Now().Unix()) {
			r18MagnetPlaybackError(c, http.StatusServiceUnavailable, "MagnetCacheExpired", "所选网盘缓存已到期，后台正在清理，请稍后重新播放")
			return nil, cloud115.File{}, false
		}
		if r18MagnetStoppedTask(c, binding.Task) {
			return nil, cloud115.File{}, false
		}
		if !resourceVerified && (binding.Task.Status != "ready" || binding.FileID == "") {
			if _, _, ok := a.resolveR18PlaybackMagnet(c, ctx, scope, video, hash, binding.ResourceID); !ok {
				return nil, cloud115.File{}, false
			}
			resourceVerified = true
			continue
		}
		if !woken && c.Request.Method == http.MethodGet && queue != nil {
			queue.Wake()
			woken = true
		}
		switch binding.Task.Status {
		case "ready", "waiting_selection":
			chosen, playable := r18MagnetSelectedFile(binding)
			if !playable {
				r18MagnetPlaybackError(c, http.StatusConflict, "MagnetFileUnavailable", "所选磁力没有有效视频，或已选文件不再属于该任务")
				return nil, cloud115.File{}, false
			}
			if binding.Task.Status == "waiting_selection" || binding.FileID == "" {
				if c.Request.Method == http.MethodHead {
					r18MagnetPlaybackError(c, http.StatusServiceUnavailable, "MagnetPreparing", "所选网盘版本尚未完成文件选择，请发起播放")
					return nil, cloud115.File{}, false
				}
				if binding.FileID != "" {
					r18MagnetPlaybackError(c, http.StatusConflict, "MagnetSelectionChanged", "所选文件仍在更新，请稍后重新播放")
					return nil, cloud115.File{}, false
				}
				if err = a.db.SelectOfflineFileIfUnselected(library, video.ID, binding.Task.ID, chosen.ID, binding.Task.Version); err != nil && !errors.Is(err, store.ErrConflict) {
					r18MagnetPlaybackError(c, http.StatusConflict, "MagnetSelectionChanged", "所选文件状态已变化，请重新播放")
					return nil, cloud115.File{}, false
				}
				continue
			}
			fresh, infoErr := p.Info(cloud115.WithInteractive(ctx), chosen.ID)
			if r18MagnetWaitStopped(c, ctx) || !a.checkR18MagnetScope(c, scope) {
				return nil, cloud115.File{}, false
			}
			current, currentFound, currentValid := a.readR18MagnetBinding(c, scope, hash)
			if !currentValid {
				return nil, cloud115.File{}, false
			}
			currentFile, currentPlayable := r18MagnetSelectedFile(current)
			if !currentFound || current.Task.Status != "ready" || current.Task.CacheExpired(time.Now().Unix()) || !sameR18MagnetTaskLifecycle(binding.Task, current.Task) ||
				current.FileID != binding.FileID || !currentPlayable || !sameR18MagnetSelectedFile(chosen, currentFile) {
				r18MagnetPlaybackError(c, http.StatusConflict, "MagnetSelectionChanged", "文件核验期间所选版本已变化，请重新播放")
				return nil, cloud115.File{}, false
			}
			if infoErr != nil {
				var cloudErr *cloud115.Error
				if c.Request.Method == http.MethodGet && errors.As(infoErr, &cloudErr) && cloudErr.Missing && !cloudErr.Auth {
					if !resourceVerified {
						if _, _, ok := a.resolveR18PlaybackMagnet(c, ctx, scope, video, hash, binding.ResourceID); !ok {
							return nil, cloud115.File{}, false
						}
						resourceVerified = true
					}
					// 仅明确的文件删除进入恢复；临时网络错误不能触发再次下载。
					task := current.Task
					task.Status, task.Resume, task.Stage = "discovering", "", "file"
					task.Error = "所选网盘文件已删除，正在确认并恢复播放文件"
					task.Retryable, task.RecoveryFileID, task.NextPoll = true, chosen.ID, 0
					task.Files = []cloud115.File{}
					if err = a.db.UpdateOfflineTask(task, task.Version); err != nil && !errors.Is(err, store.ErrConflict) {
						r18MagnetPlaybackError(c, http.StatusInternalServerError, "MagnetRecoveryFailed", "保存所选版本恢复状态失败")
						return nil, cloud115.File{}, false
					}
					if queue != nil {
						queue.Wake()
					}
					continue
				}
				status := http.StatusBadGateway
				if c.Request.Method == http.MethodHead && errors.As(infoErr, &cloudErr) && cloudErr.Missing && !cloudErr.Auth {
					status = http.StatusServiceUnavailable
				}
				r18MagnetPlaybackError(c, status, r18MagnetCloudErrorCode(provider, "FileUnavailable"), cloudName(provider)+"播放文件核验失败，请稍后重试或请管理员检查连接")
				return nil, cloud115.File{}, false
			}
			if fresh.ID != chosen.ID || fresh.Directory || !cloud115.VideoFile(fresh) || fresh.Name != chosen.Name || fresh.Size != chosen.Size || chosen.SHA1 != "" && fresh.SHA1 != chosen.SHA1 || chosen.ParentID != "" && fresh.ParentID != chosen.ParentID {
				r18MagnetPlaybackError(c, http.StatusBadGateway, r18MagnetCloudErrorCode(provider, "FileIdentityChanged"), cloudName(provider)+"视频身份、名称或保存目录已变化，未播放该文件")
				return nil, cloud115.File{}, false
			}
			c.Set(r18MagnetPlaybackProofKey, r18MagnetPlaybackProof{Scope: scope, Task: current.Task, File: chosen, FileID: current.FileID, Hash: hash})
			if !a.validateR18MagnetPlayback(c) {
				return nil, cloud115.File{}, false
			}
			return p, fresh, true
		case "queued", "submitting", "downloading", "discovering":
			if c.Request.Method == http.MethodHead {
				r18MagnetPlaybackError(c, http.StatusServiceUnavailable, "MagnetPreparing", "所选网盘版本正在准备，请稍后播放")
				return nil, cloud115.File{}, false
			}
		case "needs_auth", "failed", "timed_out", "cancelled":
			r18MagnetStoppedTask(c, binding.Task)
			return nil, cloud115.File{}, false
		default:
			r18MagnetPlaybackError(c, http.StatusConflict, "MagnetStateChanged", "所选网盘任务状态不可播放，请刷新后重试")
			return nil, cloud115.File{}, false
		}
		select {
		case <-ctx.Done():
			r18MagnetWaitStopped(c, ctx)
			return nil, cloud115.File{}, false
		case <-ticker.C:
		}
	}
}

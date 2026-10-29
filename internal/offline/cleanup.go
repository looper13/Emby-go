package offline

import (
	"context"
	"errors"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/store"
)

func findTaskCacheFolder(ctx context.Context, p cloud115.Provider, t store.OfflineTask) (string, error) {
	name := "emby-go-" + t.ID
	for offset := 0; offset < 10000; {
		files, more, err := p.List(ctx, t.FolderID, offset)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			if f.Name == name {
				if !f.Directory || f.ParentID != t.FolderID || f.ID == "" || f.ID == "0" || f.ID == t.FolderID {
					return "", &cloud115.Error{Message: "115 任务缓存目录身份不匹配"}
				}
				return f.ID, nil
			}
		}
		if !more {
			return "", nil
		}
		if len(files) == 0 {
			return "", &cloud115.Error{Message: "115 缓存目录分页未前进"}
		}
		offset += len(files)
	}
	return "", &cloud115.Error{Message: "115 缓存目录超过 10000 项，无法确认任务子目录"}
}

func verifyCacheResult(ctx context.Context, p cloud115.Provider, t store.OfflineTask, result cloud115.File) error {
	if result.ID != t.ResultID {
		return &cloud115.Error{Message: "115 结果文件身份不匹配，拒绝扫描或发布播放源"}
	}
	id := t.ResultID
	seen := map[string]bool{}
	for depth := 0; depth <= 16 && id != "" && id != "0" && !seen[id]; depth++ {
		if id == t.CacheFolderID {
			return nil
		}
		seen[id] = true
		f := result
		if id != result.ID {
			var err error
			f, err = p.Info(ctx, id)
			if err != nil {
				return err
			}
		}
		if f.ID != id {
			break
		}
		id = f.ParentID
	}
	return &cloud115.Error{Message: "115 结果不在本任务缓存子目录，拒绝扫描或发布播放源"}
}

func (s *Service) cleanup(p cloud115.Provider, t store.OfflineTask) {
	if t.CacheFolderID == "0" || t.CacheFolderID == t.FolderID {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	ctx = cloud115.WithCleanup(ctx)
	if t.Status != "cleanup_pending" {
		t.Status = "cleanup_pending"
		t.Stage = "cleanup"
		t.Error = "缓存已满 24 小时，正在自动删除任务缓存"
		t.Retryable = false
		t.NextPoll = 0
		if s.save(t, t.Version) != nil {
			return
		}
		t.Version++
	}
	err := removeCache(ctx, p, t)
	if s.ctx.Err() != nil {
		return
	}
	if err != nil {
		t.Stage = "cleanup"
		t.Error = "缓存已到期，自动删除失败：" + cloud115.SafeError(err, cloud115.Tokens{}).Error()
		t.Attempts++
		t.NextPoll = time.Now().Add(time.Duration(min(300, 10*(1<<min(t.Attempts, 5)))) * time.Second).Unix()
	} else {
		t.Status = "expired"
		t.Error = "缓存已满 24 小时并自动删除；如需播放请重新提交下载"
		t.Files = []cloud115.File{}
		t.NextPoll = 0
	}
	_ = s.save(t, t.Version)
}

func removeCache(ctx context.Context, p cloud115.Provider, t store.OfflineTask) error {
	cache, ok := p.(cloud115.CacheProvider)
	if !ok {
		return &cloud115.Error{Message: "请更新 Cookie 后恢复自动清理", Auth: true}
	}
	if t.CacheFolderID == "" {
		var err error
		t.CacheFolderID, err = findTaskCacheFolder(ctx, p, t)
		if err != nil {
			return err
		}
		if t.CacheFolderID == "" {
			return nil
		}
	}
	child, err := p.Info(ctx, t.CacheFolderID)
	var e *cloud115.Error
	missing := errors.As(err, &e) && e.Missing
	if err != nil && !missing {
		return err
	}
	if !missing {
		root, err := p.Info(ctx, t.FolderID)
		if err != nil {
			return err
		}
		if root.ID != t.FolderID || !root.Directory || root.Name != cloud115.CacheFolderName || root.ParentID != "0" || child.ID != t.CacheFolderID || !child.Directory || child.ParentID != t.FolderID || child.Name != "emby-go-"+t.ID {
			return &cloud115.Error{Message: "缓存路径已移动、改名或身份变化，为避免误删已停止删除"}
		}
	}
	remote, err := p.Find(ctx, t.InfoHash)
	if err != nil {
		return err
	}
	// Stop only the task that writes to our owned child, never an external task
	// with the same magnet. flag=0 does not delete the remote task's result files.
	if remote != nil && remote.FolderID == t.CacheFolderID {
		if err = cache.RemoveTask(ctx, t.InfoHash); err != nil {
			return err
		}
	}
	if missing {
		return nil
	}
	return cache.Delete(ctx, t.CacheFolderID)
}

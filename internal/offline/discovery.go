package offline

import (
	"context"
	"log/slog"
	"time"

	"emby-go/internal/clouddrive"
	"emby-go/internal/store"
)

func (s *Service) discover(ctx context.Context, p clouddrive.Provider, t store.OfflineTask, fresh *clouddrive.File) {
	started := time.Now()
	defer func() {
		slog.Debug(s.brand()+" 视频扫描耗时", "task_id", t.ID, "elapsed_ms", time.Since(started).Milliseconds())
	}()
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	if t.RecoveryFileID != "" {
		gone, err := resultGone(ctx, p, t.RecoveryFileID, s.brand())
		if err != nil {
			s.fail(t, t.Version, "file", err)
			return
		}
		if gone {
			s.recoverDiscovery(ctx, p, t, t.RecoveryFileID)
			return
		}
		t.RecoveryFileID = ""
	}
	var result clouddrive.File
	if fresh != nil {
		result = *fresh
	} else {
		var err error
		result, err = p.Info(ctx, t.ResultID)
		if err != nil {
			if isMissing(err) {
				s.recoverDiscovery(ctx, p, t, t.ResultID)
				return
			}
			s.fail(t, t.Version, "discover", err)
			return
		}
	}
	if t.CacheFolderID != "" {
		if err := verifyCacheResult(ctx, p, t, result, s.brand()); err != nil {
			if isMissing(err) {
				s.recoverDiscovery(ctx, p, t, t.ResultID)
				return
			}
			s.fail(t, t.Version, "cache", err)
			return
		}
	}
	files, err := clouddrive.DiscoverFrom(ctx, p, t.ResultID, t.FolderID, result, s.brand())
	if err != nil {
		if isMissing(err) {
			s.recoverDiscovery(ctx, p, t, t.ResultID)
			return
		}
		s.fail(t, t.Version, "discover", err)
		return
	}
	if t.CacheExpired(time.Now().Unix()) {
		s.cleanup(p, t)
		return
	}
	t.Files, t.Progress, t.Status = files, 100, "waiting_selection"
	if len(files) == 1 {
		t.Status = "ready"
	}
	t.Error, t.Stage = "", ""
	t.Attempts, t.NextPoll = 0, 0
	_ = s.save(t, t.Version)
}

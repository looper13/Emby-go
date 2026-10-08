package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"

	"emby-go/internal/librarywatch"
	"emby-go/internal/scanner"
	"emby-go/internal/store"
)

func (a *App) startLibraryMonitoring() {
	if a.cfg.DisableLibraryMonitor || a.rootCtx.Err() != nil {
		return
	}
	a.watchMu.Lock()
	if a.monitors == nil {
		a.monitors = make(map[int64]*librarywatch.Monitor)
	}
	a.watchMu.Unlock()
	libraries, err := a.db.Libraries()
	if err != nil {
		slog.Warn("装载媒体库监听失败", "error", err)
		return
	}
	for _, library := range libraries {
		a.watchLibrary(library)
	}
}

func (a *App) watchLibrary(library store.Library) {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.monitors == nil || a.monitors[library.ID] != nil || a.rootCtx.Err() != nil {
		return
	}
	mode := a.cfg.MonitorMode()
	monitor, err := librarywatch.New(a.rootCtx, library.Path, librarywatch.Options{Mode: mode}, func(ctx context.Context, changes []librarywatch.Change) error {
		return a.refreshLibraryChanges(ctx, library, changes)
	}, func(err error) {
		if !errors.Is(err, errScanBusy) && !errors.Is(err, context.Canceled) {
			slog.Warn("媒体库监听将重试", "library_id", library.ID, "path", library.Path, "error", err)
		}
	})
	if err != nil {
		slog.Warn("启动媒体库监听失败，可使用定时扫描", "library_id", library.ID, "error", err)
		return
	}
	a.monitors[library.ID] = monitor
	slog.Info("已启动媒体库监控", "library_id", library.ID, "path", library.Path, "mode", mode)
}

func (a *App) stopLibraryMonitor(id int64) bool {
	a.watchMu.Lock()
	monitor := a.monitors[id]
	delete(a.monitors, id)
	a.watchMu.Unlock()
	if monitor != nil {
		monitor.Close()
	}
	return monitor != nil
}

func (a *App) closeLibraryMonitors() {
	a.watchMu.Lock()
	monitors := a.monitors
	a.monitors = nil
	a.watchMu.Unlock()
	for _, monitor := range monitors {
		monitor.Close()
	}
}

func (a *App) refreshLibraryChanges(ctx context.Context, library store.Library, changes []librarywatch.Change) (refreshErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.beginScan(1) {
		return errScanBusy
	}
	taskType := "watch"
	if a.cfg.MonitorMode() == librarywatch.ModePolling {
		taskType = "poll"
	}
	taskID := a.startTask(taskType)
	defer func() {
		a.finishTask(taskID, refreshErr)
		a.endScan(refreshErr)
	}()
	a.setScanLibrary(1, library)
	files := make(map[string][]string)
	directories := make(map[string]bool)
	for _, change := range changes {
		if change.Directory {
			directories[change.Path] = true
		} else {
			directory := filepath.Dir(change.Path)
			files[directory] = append(files[directory], change.Path)
			if _, exists := directories[directory]; !exists {
				directories[directory] = false
			}
		}
	}
	ordered := make([]string, 0, len(directories))
	for directory := range directories {
		ordered = append(ordered, directory)
	}
	sort.Strings(ordered)
	total := scanner.Result{}
	processed := 0
	defer func() {
		a.updateScanProgress(scanner.Progress{LibraryID: library.ID, LibraryName: library.Name, Total: processed, Done: processed, Result: total})
		a.cache.Clear()
		a.imgMeta.Clear()
		a.imgThumb.Clear()
		a.tagMu.Lock()
		clear(a.tags)
		a.tagMu.Unlock()
		a.nfoMu.Lock()
		clear(a.nfos)
		a.nfoMu.Unlock()
	}()
	for _, directory := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		var result scanner.Result
		var err error
		if directories[directory] {
			result, err = scanner.RefreshDirectory(a.db, library, directory, true, a.updateScanProgress)
		} else {
			result, err = scanner.RefreshFiles(a.db, library, files[directory], a.updateScanProgress)
		}
		total.Success += result.Success
		total.Pending += result.Pending
		total.Incompatible += result.Incompatible
		total.Failed += result.Failed
		total.Added += result.Added
		total.Updated += result.Updated
		total.Skipped += result.Skipped
		total.Deleted += result.Deleted
		processed += result.Added + result.Updated + result.Skipped + result.Failed
		if err != nil {
			return err
		}
		if result.Failed > 0 {
			return fmt.Errorf("局部刷新有 %d 个文件读取失败: %s", result.Failed, directory)
		}
	}
	slog.Info("媒体库局部刷新完成", "library_id", library.ID, "events", len(changes),
		"added", total.Added, "updated", total.Updated, "skipped", total.Skipped, "deleted", total.Deleted)
	return nil
}

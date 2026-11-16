package server

import (
	"path/filepath"
	"testing"

	"emby-go/internal/librarywatch"
)

func TestLibraryWatchMultipleRoots(t *testing.T) {
	for _, mode := range []string{librarywatch.ModeRealtime, librarywatch.ModePolling} {
		t.Run(mode, func(t *testing.T) {
			first, second := t.TempDir(), t.TempDir()
			app, _, _ := newProbeTestApp(t, first)
			library, err := app.db.Library(1)
			if err != nil {
				t.Fatal(err)
			}
			app.stopLibraryMonitor(library.ID)
			library, err = app.db.UpdateDirectoryLibrary(library.ID, library.Name, []string{first, second}, library.ProxyEnabled, library.ResolveRedirects, library.ProxyHeaders, library.Revision)
			if err != nil {
				t.Fatal(err)
			}
			app.cfg.LibraryMonitorMode = mode
			writeFile(t, filepath.Join(second, "movie.strm"), "https://media.test/movie.mp4")
			writeFile(t, filepath.Join(second, "movie.nfo"), "<movie><title>次目录影片</title></movie>")
			app.startLibraryMonitoring()
			app.watchMu.Lock()
			group := app.monitors[library.ID]
			count := 0
			if group != nil {
				count = len(group.roots)
			}
			app.watchMu.Unlock()
			if count != 2 {
				t.Fatalf("应监控两个根目录，实际 %d", count)
			}
			awaitLibraryWatch(t, func() bool {
				id, err := app.db.MovieIDByPath(filepath.Join(second, "movie.strm"))
				if err != nil {
					return false
				}
				movie, err := app.db.Movie(id)
				return err == nil && movie.Title == "次目录影片"
			})
			if mode == librarywatch.ModeRealtime {
				awaitLibraryWatch(t, func() bool { return !app.scanning() })
				writeFile(t, filepath.Join(second, "movie.nfo"), "<movie><title>次目录更新</title></movie>")
				awaitLibraryWatch(t, func() bool {
					movies, count, err := app.db.SearchAll(library.ID, "次目录更新", "", "title", false, 10, 0)
					return err == nil && count == 1 && len(movies) == 1
				})
			}
			if !app.stopLibraryMonitor(library.ID) {
				t.Fatal("未停止媒体库监控")
			}
			app.watchMu.Lock()
			remaining := app.monitors[library.ID]
			app.watchMu.Unlock()
			if remaining != nil {
				t.Fatal("停止后仍保留监控组")
			}
		})
	}
}

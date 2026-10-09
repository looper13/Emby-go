package server

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"emby-go/internal/librarywatch"
	"emby-go/internal/store"
)

func TestLocalArtworkScanAndIndexedAPI(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "HMN-145-破解.strm")
	writeFile(t, source, "http://media.test/movie.mp4")
	writeFile(t, filepath.Join(root, "HMN-145-破解.nfo"), "<movie><title>Local artwork</title></movie>")
	extra := filepath.Join(root, "extrafanart")
	if err := os.Mkdir(extra, 0755); err != nil {
		t.Fatal(err)
	}
	writeJPEGImage(t, filepath.Join(root, "poster.jpg"), 40, 80)
	writeJPEGImage(t, filepath.Join(root, "fanart.jpg"), 80, 40)
	writeJPEGImage(t, filepath.Join(root, "thumb.jpg"), 120, 60)
	for i := 1; i <= 10; i++ {
		writeJPEGImage(t, filepath.Join(extra, "fanart"+strconv.Itoa(i)+".jpg"), 80+i, 40)
	}
	app, server, token := newProbeTestApp(t, root)
	id, err := app.db.MovieIDByPath(source)
	if err != nil {
		t.Fatal(err)
	}
	base := "/Items/" + strconv.FormatInt(id, 10)
	imageWidth := func(path string, want int) {
		t.Helper()
		resp, err := http.Get(server.URL + base + "/Images/" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		config, _, err := image.DecodeConfig(resp.Body)
		if err != nil || resp.StatusCode != 200 || config.Width != want {
			t.Fatalf("image %s: status=%d width=%d err=%v", path, resp.StatusCode, config.Width, err)
		}
	}
	imageWidth("Thumb", 120)
	imageWidth("Backdrop", 80)
	for i := 0; i <= 10; i++ {
		imageWidth("Backdrop/"+strconv.Itoa(i), 80+i)
	}
	for _, index := range []string{"-1", "11", "invalid"} {
		resp, err := http.Get(server.URL + base + "/Images/Backdrop/" + index)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("invalid index %s returned %d", index, resp.StatusCode)
		}
	}
	resp, raw := embyRaw(t, server, http.MethodGet, base+"/Images", token, "")
	var images []map[string]any
	if err := json.Unmarshal(raw, &images); err != nil || resp.StatusCode != 200 || len(images) != 13 {
		t.Fatalf("image inventory: %s, err=%v", raw, err)
	}
	for i, info := range images[2:] {
		if info["ImageType"] != "Backdrop" || info["ImageIndex"] != float64(i) || info["ImageTag"] == "" {
			t.Fatalf("invalid indexed image: %+v", info)
		}
	}
	movie, err := app.db.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	if tags := app.embyItem(movie, store.UserData{})["BackdropImageTags"].([]string); len(tags) != 11 {
		t.Fatalf("backdrop tags=%v", tags)
	}
	resp, raw = embyRaw(t, server, http.MethodGet, "/api/admin/items/"+strconv.FormatInt(id, 10)+"/detail", token, "")
	var detail struct {
		Images []map[string]any `json:"images"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil || resp.StatusCode != 200 || len(detail.Images) != 13 {
		t.Fatalf("detail artwork missing: %s %v", raw, err)
	}
	// A file event inside extrafanart must refresh movies in the parent directory.
	path := filepath.Join(extra, "fanart2.jpg")
	old := images[4]["ImageTag"]
	stamp, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeJPEGImage(t, path, 160, 40)
	if err := os.Chtimes(path, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	library, err := app.db.Library(movie.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: path}}); err != nil {
		t.Fatal(err)
	}
	imageWidth("Backdrop/2", 160)
	if app.posterTag(path) == old {
		t.Fatal("artwork event retained old image tag")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: path}}); err != nil {
		t.Fatal(err)
	}
	imageWidth("Backdrop/2", 83)
	if err := os.RemoveAll(extra); err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: extra, Directory: true}}); err != nil {
		t.Fatal(err)
	}
	movie, err = app.db.Movie(id)
	movie = app.movieArtwork(movie)
	if err != nil || len(movie.BackdropPaths) != 1 {
		t.Fatalf("directory removal did not refresh parent: %+v %v", movie, err)
	}
}

func TestArtworkMonitoringIncludesExtraImages(t *testing.T) {
	for _, mode := range []string{librarywatch.ModeRealtime, librarywatch.ModePolling} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "movie.strm")
			writeFile(t, source, "https://media.test/movie.mp4")
			writeFile(t, filepath.Join(root, "movie.nfo"), "<movie><title>Movie</title></movie>")
			app, _, _ := newProbeTestApp(t, root)
			app.cfg.LibraryMonitorMode = mode
			id, err := app.db.MovieIDByPath(source)
			if err != nil {
				t.Fatal(err)
			}
			movie, err := app.db.Movie(id)
			if err != nil {
				t.Fatal(err)
			}
			library, err := app.db.Library(movie.LibraryID)
			if err != nil {
				t.Fatal(err)
			}
			monitor, err := librarywatch.New(app.rootCtx, root, librarywatch.Options{Mode: mode, PollInterval: 25 * time.Millisecond, Debounce: 25 * time.Millisecond}, func(ctx context.Context, changes []librarywatch.Change) error {
				return app.refreshLibraryChanges(ctx, library, changes)
			}, func(err error) { t.Log(err) })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(monitor.Close)
			awaitLibraryWatch(t, func() bool {
				app.taskMu.Lock()
				defer app.taskMu.Unlock()
				return len(app.tasks) > 0 && app.tasks[0].Status == "success" && app.tasks[0].Type != "scan"
			})
			extra := filepath.Join(root, "extrafanart")
			if err := os.Mkdir(extra, 0755); err != nil {
				t.Fatal(err)
			}
			writeJPEGImage(t, filepath.Join(extra, "fanart1.jpg"), 80, 40)
			writeJPEGImage(t, filepath.Join(root, "thumb.jpg"), 80, 40)
			awaitLibraryWatch(t, func() bool {
				m, err := app.db.Movie(id)
				m = app.movieArtwork(m)
				return err == nil && len(m.BackdropPaths) == 1 && m.LandscapePath != ""
			})
			if err := os.Remove(filepath.Join(extra, "fanart1.jpg")); err != nil {
				t.Fatal(err)
			}
			awaitLibraryWatch(t, func() bool {
				m, err := app.db.Movie(id)
				m = app.movieArtwork(m)
				return err == nil && len(m.BackdropPaths) == 0
			})
		})
	}
}

func TestAdminItemsImageTagsRefreshAfterArtworkChange(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "movie.strm")
	poster := filepath.Join(root, "poster.jpg")
	thumb := filepath.Join(root, "thumb.jpg")
	writeFile(t, source, "https://media.test/movie.mp4")
	writeFile(t, filepath.Join(root, "movie.nfo"), "<movie><title>Movie</title></movie>")
	writeJPEGImage(t, poster, 40, 80)
	writeJPEGImage(t, thumb, 80, 40)
	app, server, token := newProbeTestApp(t, root)
	id, err := app.db.MovieIDByPath(source)
	if err != nil {
		t.Fatal(err)
	}
	readTags := func() map[string]string {
		t.Helper()
		resp, raw := embyRaw(t, server, http.MethodGet, "/api/admin/items?limit=100", token, "")
		var result struct {
			Items     []store.Movie                `json:"items"`
			ImageTags map[string]map[string]string `json:"image_tags"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || resp.StatusCode != 200 || len(result.Items) != 1 {
			t.Fatalf("admin items: status=%d body=%s err=%v", resp.StatusCode, raw, err)
		}
		tags := result.ImageTags[strconv.FormatInt(id, 10)]
		if tags["Primary"] == "" || tags["Thumb"] == "" {
			t.Fatalf("missing image tags: %v", tags)
		}
		return tags
	}
	before := readTags()
	if before["Primary"] != app.posterTag(poster) || before["Thumb"] != app.posterTag(thumb) {
		t.Fatalf("list tags disagree with image API: %v", before)
	}
	stamp, err := os.Stat(poster)
	if err != nil {
		t.Fatal(err)
	}
	writeJPEGImage(t, poster, 60, 80)
	if err := os.Chtimes(poster, stamp.ModTime(), stamp.ModTime()); err != nil {
		t.Fatal(err)
	}
	movie, err := app.db.Movie(id)
	if err != nil {
		t.Fatal(err)
	}
	library, err := app.db.Library(movie.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.refreshLibraryChanges(context.Background(), library, []librarywatch.Change{{Path: poster}}); err != nil {
		t.Fatal(err)
	}
	after := readTags()
	if after["Primary"] == before["Primary"] || after["Primary"] != app.posterTag(poster) {
		t.Fatalf("poster change retained stale list tag: before=%v after=%v", before, after)
	}
	if after["Thumb"] != before["Thumb"] {
		t.Fatalf("unchanged thumb tag changed: before=%v after=%v", before, after)
	}
}

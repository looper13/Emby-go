package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/store"
)

const nfoWithStreams = `<movie>
  <title>Probed</title>
  <fileinfo>
    <size>12345678</size>
    <streamdetails>
      <video><codec>h264</codec><width>1920</width><height>1080</height></video>
      <audio><codec>aac</codec><channels>2</channels></audio>
    </streamdetails>
  </fileinfo>
</movie>`

// 读取失败的缓存必须按短 TTL 过期：NFO 先不可读、后来可读（文件补上、
// 瞬时 IO 故障恢复）时，不能一直返回通用流信息。修复前 neg 条目只看 tag：
// tag 不变就一直命中，实测一天前的失败记录仍在生效。
func TestNFOReadFailureCacheExpires(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.strm"), "http://media.test/a.mp4")
	writeFile(t, filepath.Join(root, "a.nfo"), nfoWithStreams)
	app, _, _ := newProbeTestApp(t, root)
	movie, err := app.db.Movie(mustMovieID(t, app, filepath.Join(root, "a.strm")))
	if err != nil {
		t.Fatal(err)
	}
	if movie.NFOPath == "" {
		t.Fatal("扫描未记录 NFO 路径")
	}

	inject := func(entry nfoCacheEntry) {
		app.nfoMu.Lock()
		entry.tag = app.posterTag(movie.NFOPath)
		entry.version = app.diskVersion(movie.NFOPath)
		app.nfos.put(movie.NFOPath, entry)
		app.nfoMu.Unlock()
	}
	// 一天前的「读取失败」结果，tag/版本与当前磁盘一致：唯一的重读理由是失败 TTL。
	inject(nfoCacheEntry{streams: genericVideoStream(), readAt: time.Now().Add(-24 * time.Hour), failed: true})
	streams, size := app.nfoFileInfo(movie)
	if len(streams) != 2 || streams[0]["Codec"] != "h264" || size != 12345678 {
		t.Fatalf("失败缓存未过期，仍返回通用流信息: streams=%v size=%d", streams, size)
	}

	// 「NFO 正常但没有流信息」是另一种情况：按 mtime 长期命中，不吃失败 TTL。
	inject(nfoCacheEntry{streams: genericVideoStream(), readAt: time.Now().Add(-24 * time.Hour)})
	streams, _ = app.nfoFileInfo(movie)
	if len(streams) != 1 || streams[0]["Codec"] != nil {
		t.Fatalf("无流信息的正常 NFO 不该被当成失败重读: %v", streams)
	}
}

// 缓存按路径保存最新版本：同路径反复改写只替换条目，容量满时按 LRU 淘汰，
// 不再整表清空（整表清空会把刚访问过的热点一起丢掉）。
func TestNFOCacheReplacesPerPathAndEvictsLRU(t *testing.T) {
	now := time.Now()
	nfoCacheMax := 3
	cache := newNFOCache(nfoCacheMax)
	for _, key := range []string{"a", "b", "c"} {
		cache.put(key, nfoCacheEntry{tag: "t1", version: 1})
	}
	if _, ok := cache.get("a", "t1", 1, now); !ok {
		t.Fatal("a 应命中")
	}
	cache.put("d", nfoCacheEntry{tag: "t1", version: 1})
	if _, ok := cache.get("b", "t1", 1, now); ok {
		t.Fatal("最久未用的 b 应被淘汰")
	}
	if _, ok := cache.get("a", "t1", 1, now); !ok {
		t.Fatal("热点条目 a 被淘汰")
	}
	// 同路径新版本：替换而不是新增条目。
	cache.put("a", nfoCacheEntry{tag: "t2", version: 2})
	if cache.len() != nfoCacheMax {
		t.Fatalf("同路径改写后条目数 = %d，期望 %d", cache.len(), nfoCacheMax)
	}
	if _, ok := cache.get("a", "t1", 1, now); ok {
		t.Fatal("旧版本条目仍然命中")
	}
	if _, ok := cache.get("a", "t2", 2, now); !ok {
		t.Fatal("新版本条目未命中")
	}
	// tag 变化（文件被外部改写）视为未命中。
	if _, ok := cache.get("c", "other", 1, now); ok {
		t.Fatal("tag 不一致仍命中")
	}
}

// 失效记录（diskVersions）超过上限时会清理；清理后旧 tag 缓存条目必须失配，
// 否则会把过期 tag 当成当前值发给客户端（图片/缩略图 304 或旧图）。
func TestDiskVersionPruneNeverServesStaleTag(t *testing.T) {
	root := t.TempDir()
	poster := filepath.Join(root, "poster.jpg")
	writeFile(t, poster, "image")
	app, _, _ := newProbeTestApp(t, root)

	app.invalidateImageTag(poster)
	stale := app.diskVersion(poster)
	if stale == 0 {
		t.Fatal("失效未记录版本")
	}
	app.tagMu.Lock()
	app.tags[poster] = tagEntry{tag: "stale-tag", ts: time.Now(), version: stale}
	// 全是「刚刚失效」的记录 ⇒ 按时间淘汰腾不出空间，走整表重置 + 抬高基线。
	for index := 0; index < diskVersionMaxPaths+1; index++ {
		app.bumpDiskVersionLocked(fmt.Sprintf("%s/bulk-%d", root, index))
	}
	app.tagMu.Unlock()

	app.invalidateDiskPaths([]string{filepath.Join(root, "other.jpg")}, false)
	if got := app.posterTag(poster); got == "stale-tag" {
		t.Fatal("清理后仍命中清理前的 tag：基线没有抬高")
	}
	if len(app.diskVersions) > diskVersionMaxPaths {
		t.Fatalf("失效记录未被清理: %d", len(app.diskVersions))
	}
	// 清理之后版本号仍然可用作失效标记。
	app.invalidateImageTag(poster)
	if app.diskVersion(poster) == app.diskVersion(filepath.Join(root, "never-touched.jpg")) {
		t.Fatal("失效后的版本号与未知路径相同")
	}
}

func mustMovieID(t *testing.T, app *App, sourcePath string) int64 {
	t.Helper()
	id, err := app.db.MovieIDByPath(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDiskVersionPruneNeverReusesImageTag(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset=%v", reset), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "poster.jpg")
			writeFile(t, path, "old")
			app, _, _ := newProbeTestApp(t, root)
			original := app.posterTag(path)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, "new")
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			app.invalidateImageTag(path)
			changed := app.posterTag(path)
			if changed == original {
				t.Fatal("explicit invalidation reused original tag")
			}
			app.tagMu.Lock()
			aged := time.Now().Add(-25 * time.Hour)
			record := app.diskVersions[cachePathKey(path)]
			record.ts = aged
			app.diskVersions[cachePathKey(path)] = record
			for index := 0; index <= diskVersionMaxPaths; index++ {
				key := fmt.Sprintf("bulk-%d", index)
				app.bumpDiskVersionLocked(key)
				if !reset {
					record := app.diskVersions[key]
					record.ts = aged
					app.diskVersions[key] = record
				}
			}
			issued := app.diskVersionSeq
			app.pruneDiskVersionsLocked(time.Now())
			app.tagMu.Unlock()
			if version := app.diskVersion(path); version <= issued {
				t.Fatalf("prune reused issued version: got=%d maxIssued=%d", version, issued)
			}
			if tag := app.posterTag(path); tag == original || tag == changed {
				t.Fatal("prune reused a historical client ImageTag")
			}
		})
	}
}

func TestNFOFlightRespectsDiskChanges(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%v", explicit), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "movie.nfo")
			writeFile(t, path, nfoWithStreams)
			app, _, _ := newProbeTestApp(t, root)
			old := readNFOEntry(path)
			old.tag, old.version = app.posterTag(path), app.diskVersion(path)
			key := nfoFlightKey{path: path, tag: old.tag, version: old.version}
			flight := &nfoFlight{done: make(chan struct{}), entry: old}
			app.nfoMu.Lock()
			app.nfos.flights[key] = flight
			app.nfoMu.Unlock()
			defer close(flight.done)
			// 保持旧读取尚未结束；改写后新请求必须独立读到新轨信息。
			writeFile(t, path, strings.ReplaceAll(nfoWithStreams, "h264", "hevc"))
			if explicit {
				app.invalidateNFOStreams(path)
			} else {
				before, _ := os.Stat(path)
				if err := os.Chtimes(path, before.ModTime(), before.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				app.tagMu.Lock()
				delete(app.tags, path) // 模拟 tag TTL 到期、重新取得文件 mtime。
				app.tagMu.Unlock()
			}
			result := make(chan nfoCacheEntry, 1)
			go func() { result <- app.nfoEntry(store.Movie{NFOPath: path}) }()
			select {
			case entry := <-result:
				if entry.streams[0]["Codec"] != "hevc" {
					t.Fatalf("new request returned stale stream: %+v", entry)
				}
				if got := app.nfoEntry(store.Movie{NFOPath: path}); got.streams[0]["Codec"] != "hevc" {
					t.Fatal("new generation did not remain cached")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("new generation waited for old NFO read")
			}
		})
	}
}

package server

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/store"
)

func TestUpstreamEmptyProbeSkipsMissingBinary(t *testing.T) {
	app, media := detailTestApp(t, t.TempDir(), `<movie><title>Ready</title></movie>`)
	app.cfg.FFProbePath = filepath.Join(t.TempDir(), "missing-ffprobe")
	library, err := app.db.AddLibrary("Empty", media)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.prepareProbe(probeRequest{LibraryID: library.ID})
	if err != nil || len(plan.targets) != 0 {
		t.Fatalf("empty scope should skip: plan=%+v err=%v", plan, err)
	}
	id, err := app.db.UpsertMovie(store.Movie{LibraryID: library.ID, SourcePath: filepath.Join(media, "ABF-018.strm"), NFOPath: filepath.Join(media, "ABF-018.nfo"), Status: "success"}, 0, time.Now())
	if err != nil || id == 0 {
		t.Fatalf("fixture insert failed: id=%d err=%v", id, err)
	}
	if _, err := app.prepareProbe(probeRequest{LibraryID: library.ID}); err == nil {
		t.Fatal("non-empty scope must still report a missing ffprobe")
	}
}

func TestUpstreamCancelledProbeDoesNotCountFailure(t *testing.T) {
	app, media := detailTestApp(t, t.TempDir(), `<movie><title>Ready</title></movie>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := probeTarget{Movie: store.Movie{Title: "Ready"}, Path: filepath.Join(media, "ABF-018.strm")}
	app.probeOne(ctx, filepath.Join(t.TempDir(), "missing-ffprobe"), target, false, make(chan int64, 1))
	if app.probeStatus.Failed != 0 || app.probeStatus.consecutiveFailures != 0 || len(app.probeStatus.Failures) != 0 {
		t.Fatalf("cancellation polluted failure counters: %+v", app.probeStatus)
	}
}

func TestUpstreamTagCacheBoundsHistoryAndPreservesVersions(t *testing.T) {
	app, media := detailTestApp(t, t.TempDir(), `<movie><title>Ready</title></movie>`)
	now := time.Now()
	app.tagMu.Lock()
	for i := 0; i < tagCacheLimit+tagCacheSlack; i++ {
		app.tags[fmt.Sprintf("historic-%d", i)] = tagEntry{tag: "old", ts: now}
	}
	app.tagMu.Unlock()
	path := filepath.Join(media, "ABF-018.nfo")
	before := app.posterTag(path)
	app.tagMu.Lock()
	size := len(app.tags)
	app.tagMu.Unlock()
	if size > tagCacheLimit {
		t.Fatalf("history not bounded after insertion: %d", size)
	}
	app.invalidateImageTag(path)
	if after := app.posterTag(path); after == before || after == "0" {
		t.Fatalf("explicit disk invalidation lost: before=%s after=%s", before, after)
	}
	app.tagMu.Lock()
	for i := 0; i < tagCacheLimit+tagCacheSlack; i++ {
		app.tags[fmt.Sprintf("expired-%d", i)] = tagEntry{tag: "0", neg: true, ts: now.Add(-time.Minute)}
	}
	app.tagMu.Unlock()
	if got := app.posterTag(filepath.Join(media, "missing-poster.jpg")); got != "0" {
		t.Fatalf("negative tag changed: %s", got)
	}
	app.tagMu.Lock()
	defer app.tagMu.Unlock()
	if len(app.tags) > tagCacheLimit {
		t.Fatalf("negative insert did not prune: %d", len(app.tags))
	}
	for path := range app.tags {
		if len(path) >= 8 && path[:8] == "expired-" {
			t.Fatal("expired negative cache survived pruning")
		}
	}
}

package librarywatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func testOptions() Options {
	return Options{Debounce: 120 * time.Millisecond, MaxDelay: 500 * time.Millisecond, RetryDelay: 150 * time.Millisecond}
}

func writeWatchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("http://media.test/movie.mp4\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func nextChanges(t *testing.T, batches <-chan []Change) []Change {
	t.Helper()
	select {
	case changes := <-batches:
		return changes
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for filesystem events")
		return nil
	}
}

func containsChange(changes []Change, path string, directory bool) bool {
	for _, change := range changes {
		if change.Path == path && change.Directory == directory {
			return true
		}
	}
	return false
}

func TestMonitorDebouncesAndFilters(t *testing.T) {
	root := t.TempDir()
	batches := make(chan []Change, 20)
	monitor, err := New(context.Background(), root, testOptions(), func(_ context.Context, changes []Change) error {
		batches <- changes
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	if changes := nextChanges(t, batches); !containsChange(changes, root, true) {
		t.Fatalf("initial reconciliation = %+v", changes)
	}
	for attempt := 0; attempt < 4; attempt++ {
		writeWatchFile(t, filepath.Join(root, "a.strm"))
		time.Sleep(10 * time.Millisecond)
	}
	writeWatchFile(t, filepath.Join(root, "a.nfo"))
	writeWatchFile(t, filepath.Join(root, "test.db-wal"))
	writeWatchFile(t, filepath.Join(root, "metadata.tmp"))
	changes := nextChanges(t, batches)
	if len(changes) != 2 || !containsChange(changes, filepath.Join(root, "a.strm"), false) || !containsChange(changes, filepath.Join(root, "a.nfo"), false) {
		t.Fatalf("burst was not coalesced or filtered: %+v", changes)
	}
	select {
	case extra := <-batches:
		t.Fatalf("duplicate refresh after debounce: %+v", extra)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestMonitorRegistersMovedTreesAndRecoversRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "library")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	batches := make(chan []Change, 30)
	monitor, err := New(context.Background(), root, testOptions(), func(_ context.Context, changes []Change) error {
		batches <- changes
		return nil
	}, func(err error) { t.Logf("watch error: %v", err) })
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	nextChanges(t, batches)
	staging := filepath.Join(parent, "staging")
	if err := os.MkdirAll(filepath.Join(staging, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	writeWatchFile(t, filepath.Join(staging, "child", "movie.strm"))
	destination := filepath.Join(root, "new")
	if err := os.Rename(staging, destination); err != nil {
		t.Fatal(err)
	}
	if changes := nextChanges(t, batches); !containsChange(changes, destination, true) {
		t.Fatalf("moved-in tree was not queued: %+v", changes)
	}
	nested := filepath.Join(destination, "child", "movie.strm")
	writeWatchFile(t, nested)
	if changes := nextChanges(t, batches); !containsChange(changes, nested, false) {
		t.Fatalf("new child directory is not watched: %+v", changes)
	}
	renamed := filepath.Join(root, "renamed")
	if err := os.Rename(destination, renamed); err != nil {
		t.Fatal(err)
	}
	if changes := nextChanges(t, batches); !containsChange(changes, destination, true) || !containsChange(changes, renamed, true) {
		t.Fatalf("rename must refresh both old and new tree: %+v", changes)
	}
	if err := os.Rename(root, filepath.Join(parent, "offline")); err != nil {
		t.Fatal(err)
	}
	nextChanges(t, batches)
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if changes := nextChanges(t, batches); !containsChange(changes, root, true) {
		t.Fatalf("restored root was not reconciled: %+v", changes)
	}
	file := filepath.Join(root, "restored.strm")
	writeWatchFile(t, file)
	if changes := nextChanges(t, batches); !containsChange(changes, file, false) {
		t.Fatalf("restored root is not watched: %+v", changes)
	}
	if err := os.Rename(root, filepath.Join(parent, "replaced")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if changes := nextChanges(t, batches); !containsChange(changes, root, true) {
		t.Fatalf("replacement root identity was missed: %+v", changes)
	}
	writeWatchFile(t, file)
	if changes := nextChanges(t, batches); !containsChange(changes, file, false) {
		t.Fatalf("replacement root is not watched: %+v", changes)
	}
}

func TestMonitorContinuousWritesHaveBoundedDelay(t *testing.T) {
	root := t.TempDir()
	batches := make(chan []Change, 30)
	monitor, err := New(context.Background(), root, testOptions(), func(_ context.Context, changes []Change) error {
		batches <- changes
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	nextChanges(t, batches)
	finished := make(chan struct{})
	file := filepath.Join(root, "continuous.strm")
	go func() {
		defer close(finished)
		deadline := time.Now().Add(1200 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = os.WriteFile(file, []byte("http://media.test/movie.mp4\n"), 0644)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	defer func() { <-finished }()
	if changes := nextChanges(t, batches); !containsChange(changes, file, false) {
		t.Fatalf("continuous events not flushed: %+v", changes)
	}
	select {
	case <-finished:
		t.Fatal("continuous writes starved refresh until writer finished")
	default:
	}
}

func TestMonitorRetriesAndKeepsEventsDuringRefresh(t *testing.T) {
	root := t.TempDir()
	batches := make(chan []Change, 30)
	var reject atomic.Bool
	var block atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	monitor, err := New(context.Background(), root, testOptions(), func(ctx context.Context, changes []Change) error {
		batches <- changes
		if block.CompareAndSwap(true, false) {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if reject.Load() {
			return errors.New("busy")
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	nextChanges(t, batches)
	reject.Store(true)
	first := filepath.Join(root, "first.strm")
	writeWatchFile(t, first)
	nextChanges(t, batches)
	reject.Store(false)
	if changes := nextChanges(t, batches); !containsChange(changes, first, false) {
		t.Fatalf("busy batch was lost: %+v", changes)
	}
	block.Store(true)
	writeWatchFile(t, first)
	nextChanges(t, batches)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	second := filepath.Join(root, "second.strm")
	writeWatchFile(t, second)
	close(release)
	if changes := nextChanges(t, batches); !containsChange(changes, second, false) {
		t.Fatalf("event during refresh was lost: %+v", changes)
	}
}

func TestMonitorCompletedErrorsDoNotReplayAndKeepNewEvents(t *testing.T) {
	root := t.TempDir()
	batches := make(chan []Change, 30)
	reported := make(chan error, 30)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var block atomic.Bool
	monitor, err := New(context.Background(), root, testOptions(), func(ctx context.Context, changes []Change) error {
		batches <- changes
		if block.CompareAndSwap(true, false) {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return fmt.Errorf("wrapped: %w", &RefreshCompletedError{Err: errors.New("invalid NFO")})
	}, func(err error) { reported <- err })
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	nextChanges(t, batches)
	select {
	case err := <-reported:
		if ShouldRetry(err) {
			t.Fatalf("completed item failure was retryable: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("item failure was not reported")
	}
	select {
	case changes := <-batches:
		t.Fatalf("completed root batch was automatically replayed: %+v", changes)
	case <-time.After(3 * testOptions().RetryDelay):
	}
	block.Store(true)
	first := filepath.Join(root, "first.nfo")
	writeWatchFile(t, first)
	if changes := nextChanges(t, batches); !containsChange(changes, first, false) {
		t.Fatalf("new file change did not trigger refresh: %+v", changes)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	second := filepath.Join(root, "second.strm")
	writeWatchFile(t, second)
	close(release)
	changes := nextChanges(t, batches)
	if !containsChange(changes, second, false) || containsChange(changes, first, false) || containsChange(changes, root, true) {
		t.Fatalf("new event lost or completed batch replayed: %+v", changes)
	}
}

func TestMonitorOverflowReconcilesAndReattaches(t *testing.T) {
	root := t.TempDir()
	watcher, err := fsnotify.NewBufferedWatcher(128)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	batches := make(chan []Change, 30)
	go func() {
		defer close(done)
		run(ctx, fsnotifySource{watcher}, root, testOptions(), func(_ context.Context, changes []Change) error {
			batches <- changes
			return nil
		}, func(error) {})
	}()
	defer func() { cancel(); <-done }()
	nextChanges(t, batches)
	watcher.Errors <- fsnotify.ErrEventOverflow
	if changes := nextChanges(t, batches); !containsChange(changes, root, true) {
		t.Fatalf("overflow did not request reconciliation: %+v", changes)
	}
	file := filepath.Join(root, "after-overflow.strm")
	writeWatchFile(t, file)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case changes := <-batches:
			if containsChange(changes, file, false) {
				return
			}
			writeWatchFile(t, file)
		case <-deadline:
			t.Fatal("monitor did not reattach after overflow")
		}
	}
}

func TestMergeDirectoryKeepsSiblingPrefix(t *testing.T) {
	root := t.TempDir()
	pending := make(map[string]Change)
	merge(pending, Change{Path: filepath.Join(root, "movie", "a.strm")})
	merge(pending, Change{Path: filepath.Join(root, "movie-more", "b.strm")})
	merge(pending, Change{Path: filepath.Join(root, "movie"), Directory: true})
	merge(pending, Change{Path: filepath.Join(root, "movie", "a.nfo")})
	if len(pending) != 2 {
		t.Fatalf("recursive merge crossed directory boundary: %+v", pending)
	}
}

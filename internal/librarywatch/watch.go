package librarywatch

import (
	"context"
	"emby-go/internal/imageutil"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Change struct {
	Path      string
	Directory bool
}

type Options struct {
	Mode         string
	PollInterval time.Duration
	Debounce     time.Duration
	MaxDelay     time.Duration
	RetryDelay   time.Duration
}

type Monitor struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type workerResult struct {
	changes []Change
	err     error
}

// RefreshCompletedError reports item failures after the entire batch has been
// processed. Replaying that batch cannot fix invalid input; a new file event
// or explicit scan should attempt those items again.
type RefreshCompletedError struct {
	Err error
}

func (e *RefreshCompletedError) Error() string { return e.Err.Error() }
func (e *RefreshCompletedError) Unwrap() error { return e.Err }

// ShouldRetry distinguishes unfinished refreshes from completed batches with
// item errors. Unfinished work (including a busy scanner) retains its events.
func ShouldRetry(err error) bool {
	var completed *RefreshCompletedError
	return err != nil && !errors.As(err, &completed)
}

func New(parent context.Context, root string, options Options, refresh func(context.Context, []Change) error, report func(error)) (*Monitor, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var watcher eventSource
	switch options.Mode {
	case "", ModeRealtime:
		watcher, err = newEventSource(absolute)
	case ModePolling:
		watcher = newPollingSource(absolute, options.PollInterval)
	default:
		return nil, fmt.Errorf("unsupported library monitor mode: %q", options.Mode)
	}
	if err != nil {
		return nil, err
	}
	if options.Debounce <= 0 {
		options.Debounce = time.Second
	}
	if options.MaxDelay <= 0 {
		options.MaxDelay = 5 * time.Second
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = 5 * time.Second
	}
	if report == nil {
		report = func(error) {}
	}
	ctx, cancel := context.WithCancel(parent)
	monitor := &Monitor{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(monitor.done)
		run(ctx, watcher, absolute, options, refresh, report)
	}()
	return monitor, nil
}

func (m *Monitor) Close() {
	m.cancel()
	<-m.done
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func relevant(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	if extension == ".strm" || extension == ".nfo" {
		return true
	}
	return imageutil.IsLocalArtwork(path)
}

func merge(pending map[string]Change, change Change) {
	change.Path = filepath.Clean(change.Path)
	for path, existing := range pending {
		if existing.Directory && within(path, change.Path) {
			return
		}
		if change.Directory && within(change.Path, path) {
			delete(pending, path)
		}
	}
	pending[change.Path] = change
}

func run(ctx context.Context, watcher eventSource, root string, options Options, refresh func(context.Context, []Change) error, report func(error)) {
	defer watcher.Close()
	watched := make(map[string]struct{})
	repairs := make(map[string]struct{})
	pending := make(map[string]Change)
	results := make(chan workerResult, 1)
	running := false
	defer func() {
		if running {
			<-results
		}
	}()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	retry := time.NewTicker(options.RetryDelay)
	defer retry.Stop()
	firstEvent := time.Time{}
	arm := func(delay time.Duration) {
		timer.Stop()
		timer.Reset(delay)
	}
	queue := func(change Change) {
		merge(pending, change)
		if len(pending) > 4096 {
			report(errors.New("监听事件积压，重新同步媒体库"))
			clear(pending)
			pending[root] = Change{Path: root, Directory: true}
		}
		now := time.Now()
		if firstEvent.IsZero() {
			firstEvent = now
		}
		deadline := minTime(now.Add(options.Debounce), firstEvent.Add(options.MaxDelay))
		arm(time.Until(deadline))
	}
	registerTree := func(directory string) error {
		return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return nil
			}
			if _, exists := watched[path]; !exists {
				if err := watcher.Add(path); err != nil {
					return err
				}
				watched[path] = struct{}{}
			}
			return nil
		})
	}
	removeTree := func(directory string) {
		for path := range watched {
			if within(directory, path) {
				_ = watcher.Remove(path)
				delete(watched, path)
			}
		}
	}
	repair := false
	if err := registerTree(root); err != nil {
		report(err)
		repair = true
	}
	rootIdentity, _ := directoryIdentity(root)
	queue(Change{Path: root, Directory: true})
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-watcher.events():
			if !open {
				return
			}
			path := filepath.Clean(event.Name)
			if !within(root, path) {
				continue
			}
			_, wasDirectory := watched[path]
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if wasDirectory {
					removeTree(path)
					queue(Change{Path: path, Directory: true})
					if path == root {
						repair = true
					}
				} else if relevant(path) {
					queue(Change{Path: path})
				}
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					if err := registerTree(path); err != nil {
						report(err)
						repairs[path] = struct{}{}
					}
					queue(Change{Path: path, Directory: true})
					continue
				}
			}
			if relevant(path) && event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Chmod) != 0 {
				queue(Change{Path: path})
			}
		case err, open := <-watcher.errors():
			if !open {
				return
			}
			report(err)
			removeTree(root)
			repair = true
			queue(Change{Path: root, Directory: true})
		case <-retry.C:
			for path := range repairs {
				err := registerTree(path)
				if err == nil || os.IsNotExist(err) {
					delete(repairs, path)
					queue(Change{Path: path, Directory: true})
				}
			}
			if !repair {
				info, err := os.Stat(root)
				if err != nil || rootIdentity == nil || !os.SameFile(rootIdentity, info) {
					removeTree(root)
					repair = true
					queue(Change{Path: root, Directory: true})
				}
			}
			if !repair {
				continue
			}
			actual := make(map[string]struct{})
			for _, path := range watcher.WatchList() {
				actual[path] = struct{}{}
			}
			for path := range watched {
				if _, exists := actual[path]; !exists {
					delete(watched, path)
				}
			}
			if err := registerTree(root); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					report(err)
				}
				continue
			}
			repair = false
			clear(repairs)
			rootIdentity, _ = directoryIdentity(root)
			queue(Change{Path: root, Directory: true})
		case <-timer.C:
			if running || len(pending) == 0 {
				continue
			}
			changes := make([]Change, 0, len(pending))
			for _, change := range pending {
				changes = append(changes, change)
			}
			sort.Slice(changes, func(left, right int) bool { return changes[left].Path < changes[right].Path })
			clear(pending)
			firstEvent = time.Time{}
			running = true
			go func() { results <- workerResult{changes: changes, err: refresh(ctx, changes)} }()
		case result := <-results:
			running = false
			retry := ShouldRetry(result.err)
			if result.err != nil && ctx.Err() == nil {
				report(result.err)
				if retry {
					for _, change := range result.changes {
						merge(pending, change)
					}
				}
			}
			if len(pending) > 0 {
				firstEvent = time.Now()
				if retry {
					arm(options.RetryDelay)
				} else {
					arm(options.Debounce)
				}
			}
		}
	}
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func directoryIdentity(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, info) {
		return nil, errors.New("无法取得媒体库目录标识: " + path)
	}
	return info, nil
}

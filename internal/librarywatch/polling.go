package librarywatch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	ModeRealtime        = "realtime"
	ModePolling         = "polling"
	DefaultPollInterval = 30 * time.Second
)

type treeEntry struct {
	Size     int64
	Modified int64
	Mode     fs.FileMode
}

type treeSnapshot map[string]treeEntry

type pollingSource struct {
	root     string
	interval time.Duration
	paths    map[string]struct{}
	snapshot treeSnapshot
	eventCh  chan fsnotify.Event
	errorCh  chan error
	cancel   context.CancelFunc
	done     chan struct{}
}

func newPollingSource(root string, interval time.Duration) *pollingSource {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := &pollingSource{
		root: root, interval: interval, paths: make(map[string]struct{}),
		eventCh: make(chan fsnotify.Event, 1024), errorCh: make(chan error, 1),
		cancel: cancel, done: make(chan struct{}),
	}
	go source.run(ctx)
	return source
}

func (source *pollingSource) Add(path string) error {
	source.paths[path] = struct{}{}
	return nil
}

func (source *pollingSource) Remove(path string) error {
	delete(source.paths, path)
	return nil
}

func (source *pollingSource) WatchList() []string {
	paths := make([]string, 0, len(source.paths))
	for path := range source.paths {
		paths = append(paths, path)
	}
	return paths
}

func (source *pollingSource) events() <-chan fsnotify.Event { return source.eventCh }
func (source *pollingSource) errors() <-chan error          { return source.errorCh }

func (source *pollingSource) Close() error {
	source.cancel()
	<-source.done
	return nil
}

func (source *pollingSource) run(ctx context.Context) {
	defer close(source.done)
	defer close(source.eventCh)
	defer close(source.errorCh)
	ticker := time.NewTicker(source.interval)
	defer ticker.Stop()
	for {
		source.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (source *pollingSource) poll(ctx context.Context) {
	snapshot, err := readTreeSnapshot(ctx, source.root)
	if err != nil {
		select {
		case source.errorCh <- err:
		case <-ctx.Done():
		}
		return
	}
	var events []fsnotify.Event
	if source.snapshot == nil {
		events = []fsnotify.Event{{Name: source.root, Op: fsnotify.Create}}
	} else {
		events = diffTreeSnapshots(source.snapshot, snapshot)
	}
	for _, event := range events {
		select {
		case source.eventCh <- event:
		case <-ctx.Done():
			return
		}
	}
	source.snapshot = snapshot
}

func readTreeSnapshot(ctx context.Context, root string) (treeSnapshot, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("媒体库路径不是目录: %s", root)
	}
	snapshot := make(treeSnapshot)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			snapshot[path] = treeEntry{Mode: fs.ModeDir}
		} else if relevant(path) {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			snapshot[path] = treeEntry{Size: info.Size(), Modified: info.ModTime().UnixNano(), Mode: info.Mode()}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func diffTreeSnapshots(previous, current treeSnapshot) []fsnotify.Event {
	changes := make(map[string]fsnotify.Op)
	for path, before := range previous {
		after, exists := current[path]
		if !exists || before.Mode.IsDir() != after.Mode.IsDir() {
			changes[path] = fsnotify.Remove
		}
	}
	for path, after := range current {
		before, exists := previous[path]
		if !exists || before.Mode.IsDir() != after.Mode.IsDir() {
			changes[path] |= fsnotify.Create
		} else if !after.Mode.IsDir() && before != after {
			changes[path] = fsnotify.Write
		}
	}
	events := make([]fsnotify.Event, 0, len(changes))
	for path, operation := range changes {
		events = append(events, fsnotify.Event{Name: path, Op: operation})
	}
	sort.Slice(events, func(left, right int) bool { return events[left].Name < events[right].Name })
	return events
}

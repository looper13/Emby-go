package librarywatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestPollingSnapshotTracksOnlyRelevantChanges(test *testing.T) {
	root := test.TempDir()
	movie := filepath.Join(root, "movie.STRM")
	nfo := filepath.Join(root, "movie.nfo")
	poster := filepath.Join(root, "poster.jpg")
	privatePoster := filepath.Join(root, "movie-poster.webp")
	for _, path := range []string{movie, nfo, poster, privatePoster} {
		writeWatchFile(test, path)
	}
	before, err := readTreeSnapshot(context.Background(), root)
	if err != nil {
		test.Fatal(err)
	}
	writeWatchFile(test, filepath.Join(root, "metadata.tmp"))
	writeWatchFile(test, filepath.Join(root, "test.db-wal"))
	writeWatchFile(test, filepath.Join(root, "unrelated.jpg"))
	unchanged, err := readTreeSnapshot(context.Background(), root)
	if err != nil {
		test.Fatal(err)
	}
	if events := diffTreeSnapshots(before, unchanged); len(events) != 0 {
		test.Fatalf("irrelevant files triggered refresh: %+v", events)
	}
	if err := os.WriteFile(movie, []byte("http://media.test/changed-size.mp4\n"), 0644); err != nil {
		test.Fatal(err)
	}
	modified := time.Now().Add(time.Hour)
	if err := os.Chtimes(nfo, modified, modified); err != nil {
		test.Fatal(err)
	}
	if err := os.Remove(poster); err != nil {
		test.Fatal(err)
	}
	if err := os.Remove(privatePoster); err != nil {
		test.Fatal(err)
	}
	directory := filepath.Join(root, "new")
	if err := os.Mkdir(directory, 0755); err != nil {
		test.Fatal(err)
	}
	nested := filepath.Join(directory, "new.strm")
	writeWatchFile(test, nested)
	after, err := readTreeSnapshot(context.Background(), root)
	if err != nil {
		test.Fatal(err)
	}
	operations := make(map[string]fsnotify.Op)
	for _, event := range diffTreeSnapshots(before, after) {
		operations[event.Name] = event.Op
	}
	want := map[string]fsnotify.Op{movie: fsnotify.Write, nfo: fsnotify.Write, poster: fsnotify.Remove, privatePoster: fsnotify.Remove, directory: fsnotify.Create, nested: fsnotify.Create}
	if !reflect.DeepEqual(operations, want) {
		test.Fatalf("snapshot changes = %+v, want %+v", operations, want)
	}
}

func TestPollingRetainsSnapshotOnReadFailure(test *testing.T) {
	parent := test.TempDir()
	root := filepath.Join(parent, "library")
	if err := os.Mkdir(root, 0755); err != nil {
		test.Fatal(err)
	}
	movie := filepath.Join(root, "movie.strm")
	writeWatchFile(test, movie)
	source := &pollingSource{root: root, eventCh: make(chan fsnotify.Event, 20), errorCh: make(chan error, 2)}
	source.poll(context.Background())
	if event := <-source.eventCh; event.Name != root || !event.Has(fsnotify.Create) {
		test.Fatalf("startup event = %+v", event)
	}
	previous := source.snapshot
	offline := filepath.Join(parent, "offline")
	if err := os.Rename(root, offline); err != nil {
		test.Fatal(err)
	}
	source.poll(context.Background())
	if len(source.errorCh) != 1 || len(source.eventCh) != 0 || !reflect.DeepEqual(previous, source.snapshot) {
		test.Fatal("unavailable root replaced the baseline or emitted removals")
	}
	if err := os.Remove(filepath.Join(offline, "movie.strm")); err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(offline, root); err != nil {
		test.Fatal(err)
	}
	source.poll(context.Background())
	if len(source.eventCh) != 1 {
		test.Fatalf("recovery events = %d", len(source.eventCh))
	}
	if event := <-source.eventCh; event.Name != movie || !event.Has(fsnotify.Remove) {
		test.Fatalf("recovery lost offline changes: %+v", event)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readTreeSnapshot(ctx, root); !errors.Is(err, context.Canceled) {
		test.Fatalf("canceled snapshot error = %v", err)
	}
}

func TestPollingMonitorDebouncesRetriesAndTracksMoves(test *testing.T) {
	previousFactory := newEventSource
	newEventSource = func(string) (eventSource, error) {
		return nil, errors.New("native notifications unavailable")
	}
	defer func() { newEventSource = previousFactory }()
	root := test.TempDir()
	options := testOptions()
	options.Mode, options.PollInterval = ModePolling, 40*time.Millisecond
	batches := make(chan []Change, 30)
	var busy atomic.Bool
	monitor, err := New(context.Background(), root, options, func(_ context.Context, changes []Change) error {
		batches <- changes
		if busy.Load() {
			return errors.New("busy")
		}
		return nil
	}, nil)
	if err != nil {
		test.Fatal(err)
	}
	defer monitor.Close()
	if changes := nextChanges(test, batches); !containsChange(changes, root, true) {
		test.Fatalf("initial reconciliation = %+v", changes)
	}
	file := filepath.Join(root, "movie.strm")
	nfo := filepath.Join(root, "movie.nfo")
	busy.Store(true)
	for attempt := 0; attempt < 4; attempt++ {
		writeWatchFile(test, file)
		writeWatchFile(test, nfo)
	}
	writeWatchFile(test, filepath.Join(root, "ignored.tmp"))
	changes := nextChanges(test, batches)
	if len(changes) != 2 || !containsChange(changes, file, false) || !containsChange(changes, nfo, false) {
		test.Fatalf("polling burst = %+v", changes)
	}
	busy.Store(false)
	if changes := nextChanges(test, batches); len(changes) != 2 || !containsChange(changes, file, false) {
		test.Fatalf("busy polling batch was lost: %+v", changes)
	}
	staging := test.TempDir()
	if err := os.Mkdir(filepath.Join(staging, "child"), 0755); err != nil {
		test.Fatal(err)
	}
	writeWatchFile(test, filepath.Join(staging, "child", "nested.strm"))
	destination := filepath.Join(root, "new")
	if err := os.Rename(staging, destination); err != nil {
		test.Fatal(err)
	}
	if changes := nextChanges(test, batches); len(changes) != 1 || !containsChange(changes, destination, true) {
		test.Fatalf("moved-in tree was not coalesced: %+v", changes)
	}
	renamed := filepath.Join(root, "renamed")
	if err := os.Rename(destination, renamed); err != nil {
		test.Fatal(err)
	}
	if changes := nextChanges(test, batches); len(changes) != 2 || !containsChange(changes, destination, true) || !containsChange(changes, renamed, true) {
		test.Fatalf("directory rename = %+v", changes)
	}
	nested := filepath.Join(renamed, "child", "nested.strm")
	if err := os.Remove(nested); err != nil {
		test.Fatal(err)
	}
	if changes := nextChanges(test, batches); len(changes) != 1 || !containsChange(changes, nested, false) {
		test.Fatalf("nested file removal = %+v", changes)
	}
	select {
	case extra := <-batches:
		test.Fatalf("unchanged snapshot triggered refresh: %+v", extra)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestPollingDefaultsAndShutdown(test *testing.T) {
	if DefaultPollInterval != 30*time.Second {
		test.Fatalf("poll interval = %v", DefaultPollInterval)
	}
	source := newPollingSource(test.TempDir(), 0)
	if source.interval != DefaultPollInterval {
		test.Fatalf("default interval = %v", source.interval)
	}
	done := make(chan struct{})
	go func() { source.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		test.Fatal("shutdown waited for the 30 second tick")
	}
	if _, err := New(context.Background(), test.TempDir(), Options{Mode: "typo"}, nil, nil); err == nil {
		test.Fatal("invalid monitor mode accepted")
	}
}

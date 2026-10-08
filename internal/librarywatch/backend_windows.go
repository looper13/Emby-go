package librarywatch

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf16"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

type windowsSource struct {
	root       string
	eventQueue chan fsnotify.Event
	errorQueue chan error
	paths      map[string]struct{}
	watch      *windowsWatch
	closed     bool
}

type windowsWatch struct {
	handle   windows.Handle
	event    windows.Handle
	mu       sync.Mutex
	closed   bool
	stopped  bool
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func init() {
	newEventSource = func(root string) (eventSource, error) {
		return &windowsSource{root: root, eventQueue: make(chan fsnotify.Event, 1024), errorQueue: make(chan error, 4), paths: make(map[string]struct{})}, nil
	}
}

func (s *windowsSource) events() <-chan fsnotify.Event { return s.eventQueue }
func (s *windowsSource) errors() <-chan error          { return s.errorQueue }

func (s *windowsSource) Add(path string) error {
	if s.closed {
		return fsnotify.ErrClosed
	}
	if path == s.root && s.watch == nil {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		handle, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
		if err != nil {
			return os.NewSyscallError("CreateFile", err)
		}
		event, err := windows.CreateEvent(nil, 1, 0, nil)
		if err != nil {
			windows.CloseHandle(handle)
			return err
		}
		s.watch = &windowsWatch{handle: handle, event: event, stop: make(chan struct{}), done: make(chan struct{})}
		go s.read(s.watch)
	}
	s.paths[path] = struct{}{}
	return nil
}

func (s *windowsSource) Remove(path string) error {
	if path == s.root {
		if s.watch != nil {
			s.watch.close()
			s.watch = nil
		}
		clear(s.paths)
	} else {
		delete(s.paths, path)
	}
	return nil
}

func (s *windowsSource) WatchList() []string {
	paths := make([]string, 0, len(s.paths))
	for path := range s.paths {
		paths = append(paths, path)
	}
	return paths
}

func (s *windowsSource) Close() error {
	if !s.closed {
		s.closed = true
		s.Remove(s.root)
		close(s.eventQueue)
		close(s.errorQueue)
	}
	return nil
}

func (w *windowsWatch) close() {
	w.stopOnce.Do(func() {
		close(w.stop)
		w.mu.Lock()
		w.stopped = true
		if !w.closed {
			_ = windows.CancelIoEx(w.handle, nil)
		}
		w.mu.Unlock()
	})
	<-w.done
}

func (s *windowsSource) read(watch *windowsWatch) {
	defer func() {
		watch.mu.Lock()
		watch.closed = true
		windows.CloseHandle(watch.handle)
		windows.CloseHandle(watch.event)
		watch.mu.Unlock()
		close(watch.done)
	}()
	report := func(err error) {
		select {
		case s.errorQueue <- err:
		case <-watch.stop:
		}
	}
	buffer := make([]byte, 64*1024)
	mask := uint32(windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME | windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_ATTRIBUTES)
	for {
		overlapped := windows.Overlapped{HEvent: watch.event}
		var count uint32
		watch.mu.Lock()
		if watch.stopped {
			watch.mu.Unlock()
			return
		}
		_ = windows.ResetEvent(watch.event)
		err := windows.ReadDirectoryChanges(watch.handle, &buffer[0], uint32(len(buffer)), true, mask, nil, &overlapped, 0)
		watch.mu.Unlock()
		if err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
			report(os.NewSyscallError("ReadDirectoryChangesW", err))
			return
		}
		if err = windows.GetOverlappedResult(watch.handle, &overlapped, &count, true); err != nil {
			if !errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
				report(os.NewSyscallError("GetOverlappedResult", err))
			}
			return
		}
		if count == 0 || count > uint32(len(buffer)) {
			report(fsnotify.ErrEventOverflow)
			return
		}
		events, err := windowsEvents(s.root, buffer[:count])
		if err != nil {
			report(err)
			return
		}
		for _, event := range events {
			select {
			case s.eventQueue <- event:
			case <-watch.stop:
				return
			}
		}
	}
}

func windowsEvents(root string, data []byte) ([]fsnotify.Event, error) {
	var events []fsnotify.Event
	for {
		if len(data) < 12 {
			return nil, fsnotify.ErrEventOverflow
		}
		next := int(binary.LittleEndian.Uint32(data))
		action := binary.LittleEndian.Uint32(data[4:])
		length := int(binary.LittleEndian.Uint32(data[8:]))
		if length <= 0 || length%2 != 0 || length > len(data)-12 {
			return nil, fsnotify.ErrEventOverflow
		}
		name := make([]uint16, length/2)
		for index := range name {
			name[index] = binary.LittleEndian.Uint16(data[12+index*2:])
		}
		var operation fsnotify.Op
		switch action {
		case windows.FILE_ACTION_ADDED, windows.FILE_ACTION_RENAMED_NEW_NAME:
			operation = fsnotify.Create
		case windows.FILE_ACTION_REMOVED:
			operation = fsnotify.Remove
		case windows.FILE_ACTION_MODIFIED:
			operation = fsnotify.Write
		case windows.FILE_ACTION_RENAMED_OLD_NAME:
			operation = fsnotify.Rename
		}
		if operation != 0 {
			events = append(events, fsnotify.Event{Name: filepath.Join(root, string(utf16.Decode(name))), Op: operation})
		}
		if next == 0 {
			return events, nil
		}
		if next < 12+length || next >= len(data) {
			return nil, fsnotify.ErrEventOverflow
		}
		data = data[next:]
	}
}

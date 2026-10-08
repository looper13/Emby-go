package librarywatch

import "github.com/fsnotify/fsnotify"

type eventSource interface {
	Add(string) error
	Remove(string) error
	WatchList() []string
	Close() error
	events() <-chan fsnotify.Event
	errors() <-chan error
}

type fsnotifySource struct {
	*fsnotify.Watcher
}

func (s fsnotifySource) events() <-chan fsnotify.Event { return s.Events }
func (s fsnotifySource) errors() <-chan error          { return s.Errors }

var newEventSource = func(string) (eventSource, error) {
	watcher, err := fsnotify.NewBufferedWatcher(1024)
	if err != nil {
		return nil, err
	}
	return fsnotifySource{watcher}, nil
}

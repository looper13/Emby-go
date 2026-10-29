package cloud115

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type requestPriorityKey struct{}
type transportWaiter struct{ priority int }

func WithInteractive(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestPriorityKey{}, 0)
}
func WithCleanup(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestPriorityKey{}, 2)
}
func priority(ctx context.Context) int {
	if value, ok := ctx.Value(requestPriorityKey{}).(int); ok {
		return value
	}
	return 1
}

// Choose at request boundaries, not while holding the mutex through a timer or
// network call. Four overtakes at most keep background work from starving.
func (t *limitedTransport) admit(ctx context.Context) error {
	w := &transportWaiter{priority: priority(ctx)}
	t.mu.Lock()
	if t.changed == nil {
		t.changed = make(chan struct{})
	}
	t.queue = append(t.queue, w)
	t.notify()
	for {
		if err := ctx.Err(); err != nil {
			t.remove(w)
			t.notify()
			t.mu.Unlock()
			return err
		}
		best := t.queue[0]
		if t.bypasses < 4 {
			for _, candidate := range t.queue {
				if candidate.priority < best.priority {
					best = candidate
				}
			}
		}
		changed := t.changed
		wait := time.Until(t.next)
		if best == w && wait <= 0 {
			if w == t.queue[0] {
				t.bypasses = 0
			} else {
				t.bypasses++
			}
			t.remove(w)
			t.next = time.Now().Add(t.interval)
			t.notify()
			t.mu.Unlock()
			return nil
		}
		t.mu.Unlock()
		if best == w {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
			case <-changed:
			case <-timer.C:
			}
			timer.Stop()
		} else {
			select {
			case <-ctx.Done():
			case <-changed:
			}
		}
		t.mu.Lock()
	}
}
func (t *limitedTransport) notify() {
	close(t.changed)
	t.changed = make(chan struct{})
}
func (t *limitedTransport) remove(w *transportWaiter) {
	for i, entry := range t.queue {
		if entry == w {
			copy(t.queue[i:], t.queue[i+1:])
			t.queue[len(t.queue)-1] = nil
			t.queue = t.queue[:len(t.queue)-1]
			return
		}
	}
}

func logRequestTiming(r *http.Request, wait, network time.Duration, failed bool) {
	stage := "other"
	switch r.URL.Path {
	case "/files/get_info":
		stage = "file_info"
	case "/files":
		stage = "file_list"
	case "/files/add":
		stage = "mkdir"
	case "/rb/delete":
		stage = "delete"
	case "/app/chrome/downurl":
		stage = "direct_url"
	case "/lixian/":
		switch r.URL.Query().Get("ac") {
		case "task_lists":
			stage = "offline_status"
		case "task_del":
			stage = "remove_record"
		}
	case "/lixianssp/":
		stage = "offline_add"
	}
	slog.Debug("115 请求耗时", "stage", stage, "priority", priority(r.Context()), "wait_ms", wait.Milliseconds(), "headers_ms", network.Milliseconds(), "failed", failed)
}

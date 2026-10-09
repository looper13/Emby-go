package cache

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedClearPreservesAuthenticationAndRejectsOldLoad(t *testing.T) {
	m := NewManaged(NewMemory(64))
	m.Set("token:secret", []byte("valid"), time.Hour)
	m.Set("apikey:secret", []byte("valid"), time.Hour)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Load(context.Background(), "items:old", time.Hour, func() ([]byte, error) { close(entered); <-release; return []byte("stale"), nil })
	}()
	<-entered
	m.Clear()
	close(release)
	<-done
	if _, ok := m.Get("items:old"); ok {
		t.Fatal("inflight old load published into new generation")
	}
	for _, key := range []string{"token:secret", "apikey:secret"} {
		if _, ok := m.Get(key); !ok {
			t.Fatal("authentication discarded")
		}
	}
	fresh, err := m.Load(context.Background(), "items:old", time.Hour, func() ([]byte, error) { return []byte("fresh"), nil })
	if err != nil || string(fresh) != "fresh" {
		t.Fatalf("fresh load: %s %v", fresh, err)
	}
}
func TestManagedCollapsesConcurrentLoadsAndReportsMetrics(t *testing.T) {
	m := NewManaged(NewMemory(64))
	var loads atomic.Int64
	const count = 20
	entered, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := m.Load(context.Background(), "items:shared", time.Minute, func() ([]byte, error) { loads.Add(1); close(entered); <-release; return []byte("result"), nil })
			if err != nil || string(body) != "result" {
				t.Errorf("load: %s %v", body, err)
			}
		}()
	}
	<-entered
	deadline := time.Now().Add(3 * time.Second)
	for m.Stats()["items"].Shared < count-1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	_, err := m.Load(context.Background(), "items:shared", time.Minute, func() ([]byte, error) { t.Error("cache hit loaded again"); return nil, nil })
	stats := m.Stats()["items"]
	if err != nil || loads.Load() != 1 || stats.Loads != 1 || stats.Shared != count-1 || stats.Hits != 1 || stats.Misses != count {
		t.Fatalf("loads=%d stats=%+v err=%v", loads.Load(), stats, err)
	}
}
func TestManagedCancellationErrorsAndRetry(t *testing.T) {
	m := NewManaged(NewMemory(64))
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Load(context.Background(), "item:failure", time.Minute, func() ([]byte, error) { close(entered); <-release; return nil, errors.New("failed") })
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Load(ctx, "item:failure", time.Minute, func() ([]byte, error) { t.Error("waiter ran loader"); return nil, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	close(release)
	<-done
	if _, ok := m.Get("item:failure"); ok {
		t.Fatal("failure cached")
	}
	if _, err := m.Load(context.Background(), "item:failure", time.Minute, func() ([]byte, error) { return []byte("retry"), nil }); err != nil {
		t.Fatal(err)
	}
	if m.Stats()["item"].LoadErrors != 1 {
		t.Fatal("error count missing")
	}
}
func TestLocalDeleteMatchingPreservesOtherEntries(t *testing.T) {
	m := NewMemory(4)
	m.Set("a", []byte("a"), time.Hour)
	m.Set("b", []byte("b"), time.Hour)
	m.DeleteMatching(func(key string) bool { return key == "a" })
	if _, ok := m.Get("a"); ok {
		t.Fatal("a retained")
	}
	if _, ok := m.Get("b"); !ok {
		t.Fatal("b discarded")
	}
}

type blockedReadCache struct {
	*Memory
	reads            atomic.Int64
	blocked, release chan struct{}
}

func (b *blockedReadCache) Get(key string) ([]byte, bool) {
	if strings.HasSuffix(key, "items:slow") && b.reads.Add(1) == 2 {
		close(b.blocked)
		<-b.release
	}
	return b.Memory.Get(key)
}
func TestSlowBackendReadDoesNotBlockOtherKeys(t *testing.T) {
	backend := &blockedReadCache{Memory: NewMemory(16), blocked: make(chan struct{}), release: make(chan struct{})}
	m := NewManaged(backend)
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		_, _ = m.Load(context.Background(), "items:slow", time.Minute, func() ([]byte, error) { return []byte("slow"), nil })
	}()
	<-backend.blocked
	quickDone := make(chan struct{})
	go func() {
		defer close(quickDone)
		_, _ = m.Load(context.Background(), "items:quick", time.Minute, func() ([]byte, error) { return []byte("quick"), nil })
	}()
	select {
	case <-quickDone:
	case <-time.After(3 * time.Second):
		t.Error("slow Redis read blocked another key")
	}
	close(backend.release)
	<-slowDone
	<-quickDone
}

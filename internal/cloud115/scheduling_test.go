package cloud115

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterInteractivePreemptsQueuedCleanupWithoutStarvation(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var starts []time.Time
	transport := &limitedTransport{interval: 15 * time.Millisecond, next: time.Now().Add(300 * time.Millisecond), base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		starts = append(starts, time.Now())
		mu.Unlock()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	done := make(chan error, 8)
	enqueue := func(ctx context.Context, name string, count int) {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://fixture.test/"+name, nil)
		go func() {
			resp, err := transport.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}
			done <- err
		}()
		deadline := time.Now().Add(time.Second)
		for {
			transport.mu.Lock()
			n := len(transport.queue)
			transport.mu.Unlock()
			if n == count {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("request did not enqueue", name, n)
			}
			time.Sleep(time.Millisecond)
		}
	}
	enqueue(WithCleanup(context.Background()), "cleanup", 1)
	enqueue(context.Background(), "download", 2)
	for i := 0; i < 6; i++ {
		enqueue(WithInteractive(context.Background()), "play", i+3)
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		if paths[i] != "/play" {
			t.Fatal("interactive request failed to overtake", paths)
		}
	}
	if paths[4] != "/cleanup" {
		t.Fatal("cleanup starved", paths)
	}
	for i := 1; i < len(starts); i++ {
		if starts[i].Sub(starts[i-1]) < 14*time.Millisecond {
			t.Fatal("priority bypassed account rate limit", starts)
		}
	}
}

func TestRateLimiterCancelledPriorityWaiterDoesNotBlockOthers(t *testing.T) {
	transport := &limitedTransport{interval: time.Millisecond, next: time.Now().Add(30 * time.Millisecond), base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Context().Err() != nil {
			t.Error("cancelled request entered transport")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	ctx, cancel := context.WithCancel(WithInteractive(context.Background()))
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://fixture.test/", nil)
	if _, err := transport.RoundTrip(req); err != context.Canceled {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("GET", "https://fixture.test/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(transport.queue) != 0 {
		t.Fatal("cancelled waiter leaked")
	}
}

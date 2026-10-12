package gfriends

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixture = `{"Information":{"Timestamp":1791761793.2094188},"Content":{"z-source":{"Alice.jpg":"AI-Fix-愛麗絲.jpg?t=123","あかり.jpg":"Person A.jpg"},"0-Hand-Storage":{"Alice.jpg":"愛麗絲.jpg","Alice-1.jpg":"愛麗絲-1.jpg","あかり.jpg":"Person B.jpg"}}}`

func TestOrderedMatchAndPaths(t *testing.T) {
	i, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	list, status := i.Match("ＡＬＩＣＥ")
	if status != "matched" || len(list) != 3 || list[0].Group != "0-Hand-Storage" || list[0].Number != 0 {
		t.Fatal(list, status)
	}
	if _, s := i.Match("あかり"); s != "ambiguous" {
		t.Fatal(s)
	}
	if _, s := i.Match("Nobody"); s != "unmatched" {
		t.Fatal(s)
	}
	u, err := ImageURL(DefaultBaseURL, list[2])
	if err != nil || !strings.Contains(u, "/Content/z-source/AI-Fix-%E6%84%9B") || !strings.HasSuffix(u, "?t=123") {
		t.Fatal(u, err)
	}
	for _, data := range []string{`{}`, strings.Replace(fixture, "z-source", "../escape", 1), strings.Replace(fixture, "Person A.jpg", "../A.jpg", 1)} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Fatal("accepted bad index", data)
		}
	}
}

func TestDownloadFailureBounds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/404":
			w.WriteHeader(404)
		case "/429":
			w.WriteHeader(429)
		case "/503":
			w.WriteHeader(503)
		case "/slow":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		case "/loop":
			http.Redirect(w, r, "/loop", 302)
		default:
			w.Write([]byte("<html>not an image</html>"))
		}
	}))
	defer srv.Close()
	c := New(t.TempDir(), Config{}, 50*time.Millisecond)
	for _, p := range []string{"/404", "/429", "/503", "/slow", "/loop"} {
		before := calls.Load()
		if _, err := c.Download(context.Background(), srv.URL+p); err == nil {
			t.Fatal("accepted failed download", p)
		}
		if n := calls.Load() - before; n > 5 {
			t.Fatal("unbounded retries", p, n)
		}
	}
	if _, err := ReadLimit(strings.NewReader("12345"), 4); err == nil {
		t.Fatal("silently truncated oversized response")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Download(ctx, srv.URL); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCorruptRefreshKeepsSnapshot(t *testing.T) {
	var corrupt atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corrupt.Load() {
			w.Write([]byte("<html>gateway error</html>"))
			return
		}
		w.Write([]byte(fixture))
	}))
	defer srv.Close()
	c := New(t.TempDir(), Config{IndexURL: srv.URL, BaseURL: srv.URL}, time.Second)
	first, _, err := c.Snapshot(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	corrupt.Store(true)
	next, status, err := c.Snapshot(context.Background(), true)
	if err != nil || next.Version != first.Version || !status.Stale {
		t.Fatal(status, err)
	}
}
func TestCacheConditionalFailureAndIsolation(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	var sawConditional atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("public request has credentials")
		}
		if fail.Load() {
			http.Error(w, "offline", 503)
			return
		}
		if r.Header.Get("If-None-Match") == "v1" {
			sawConditional.Store(true)
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "v1")
		w.Write([]byte(fixture))
	}))
	defer srv.Close()
	c := New(t.TempDir(), Config{IndexURL: srv.URL, BaseURL: srv.URL}, time.Second)
	i, s, err := c.Snapshot(context.Background(), false)
	if err != nil || i == nil || s.Stale {
		t.Fatal(s, err)
	}
	if _, _, err := c.Snapshot(context.Background(), true); err != nil || !sawConditional.Load() {
		t.Fatal(err)
	}
	fail.Store(true)
	next, status, err := c.Snapshot(context.Background(), true)
	if err != nil || next.Version != i.Version || !status.Stale {
		t.Fatal(status, err)
	}
	c2 := New(c.dir, Config{IndexURL: srv.URL + "/other", BaseURL: srv.URL}, time.Second)
	if _, _, err := c2.Snapshot(context.Background(), false); err == nil {
		t.Fatal("other source used stale cache")
	}
}
func TestRefreshCoalescesAndCancelsWaiter(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		w.Write([]byte(fixture))
	}))
	defer srv.Close()
	c := New(t.TempDir(), Config{IndexURL: srv.URL, BaseURL: srv.URL}, time.Second)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, err := c.Snapshot(context.Background(), false)
		if err != nil {
			t.Error(err)
		}
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Snapshot(ctx, true); err != context.Canceled {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

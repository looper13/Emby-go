package artwork

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func picture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestArtworkTypedFilesRestartAndMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	data := picture(t)
	calls := 0
	fetch := func(context.Context) ([]byte, string, error) {
		calls++
		return data, "https://lain.bgm.tv/pic/crt/l/test.png", nil
	}
	var paths []string
	for _, kind := range []string{"subject", "character", "person", "episode"} {
		a, err := s.Ensure(context.Background(), Key{kind, 7, "medium"}, false, fetch)
		if err != nil || a.State != "ready" || a.Width != 3 {
			t.Fatal(a, err)
		}
		paths = append(paths, a.Path)
	}
	if paths[0] == paths[1] || paths[1] == paths[2] {
		t.Fatal("owner collision")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	k := Key{"character", 7, "medium"}
	a, err := s.Ensure(context.Background(), k, false, fetch)
	if err != nil || calls != 4 || a.Path != paths[1] {
		t.Fatal(a, err, calls)
	}
	if err = os.Remove(a.Path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ensure(context.Background(), k, false, fetch); err != nil || calls != 5 {
		t.Fatal("lost file not repaired", err, calls)
	}
	k = Key{"person", 8, "large"}
	missing := func(context.Context) ([]byte, string, error) { calls++; return nil, "", ErrMissing }
	if a, err = s.Ensure(context.Background(), k, false, missing); !errors.Is(err, ErrMissing) || a.State != "missing" {
		t.Fatal(a, err)
	}
	before := calls
	if _, err = s.Ensure(context.Background(), k, false, missing); !errors.Is(err, ErrMissing) || calls != before {
		t.Fatal("negative cache missed", err, calls)
	}
	if _, err = s.Ensure(context.Background(), k, true, fetch); err != nil {
		t.Fatal(err)
	}
}
func TestArtworkCoalescesAndBoundsConcurrency(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := picture(t)
	var active, peak, calls atomic.Int64
	gate := make(chan struct{})
	entered := make(chan struct{}, 10)
	fetch := func(ctx context.Context) ([]byte, string, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-gate:
			return data, "fixture", nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			if _, err := s.Ensure(context.Background(), Key{"subject", id, "common"}, false, fetch); err != nil {
				t.Error(err)
			}
		}(int64(i%6 + 1))
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	close(gate)
	wg.Wait()
	if peak.Load() > 4 || calls.Load() != 6 {
		t.Fatal(peak.Load(), calls.Load())
	}
	if _, err = s.Ensure(context.Background(), Key{"person", 1, "../../escape"}, false, fetch); err == nil {
		t.Fatal("unsafe key")
	}
}
func TestArtworkFailureRetainsGoodFileAndCloseCancels(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := picture(t)
	k := Key{"subject", 1, "common"}
	a, err := s.Ensure(context.Background(), k, false, func(context.Context) ([]byte, string, error) { return data, "old", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE images SET updated_at=0 WHERE key=?", k.String()); err != nil {
		t.Fatal(err)
	}
	b, err := s.Ensure(context.Background(), k, true, func(context.Context) ([]byte, string, error) { return []byte("not an image"), "bad", nil })
	if err != nil || b.Path != a.Path || b.State != "failed" {
		t.Fatal(b, err)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := s.Ensure(context.Background(), Key{"subject", 2, "common"}, false, func(ctx context.Context) ([]byte, string, error) {
			close(entered)
			<-ctx.Done()
			return nil, "", ctx.Err()
		})
		done <- err
	}()
	<-entered
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

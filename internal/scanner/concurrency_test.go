package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelScanPreservesWriteOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeScanFile(t, root, name+".strm", "https://media.test/movie.mp4")
		writeScanFile(t, root, name+".nfo", fmt.Sprintf("<movie><title>%s</title><actor><name>Shared</name><thumb>https://media.test/%s.jpg</thumb></actor></movie>", name, name))
	}
	database, library := scanLibrary(t, root)
	secondPrepared := make(chan struct{})
	var firstCalls, secondCalls atomic.Int64
	metadataStat = func(path string) (os.FileInfo, error) {
		switch filepath.Base(path) {
		case "a.nfo":
			if firstCalls.Add(1) == 1 {
				select {
				case <-secondPrepared:
					// Let b's result arrive while a's first attribute read waits.
					time.Sleep(20 * time.Millisecond)
				case <-time.After(time.Second):
					t.Error("second candidate was not prepared concurrently")
				}
			}
		case "b.nfo":
			if secondCalls.Add(1) == 2 {
				close(secondPrepared)
			}
		}
		return os.Stat(path)
	}
	t.Cleanup(func() { metadataStat = os.Stat })
	requireScan(t, database, library, Result{Added: 2, Success: 2})
	for i, name := range []string{"a", "b"} {
		id, err := database.MovieIDByPath(filepath.Join(root, name+".strm"))
		if err != nil || id != int64(i+1) {
			t.Errorf("%s id=%d, want %d: %v", name, id, i+1, err)
		}
	}
	actor, err := database.ActorAvatar("Shared")
	if err != nil || actor.AvatarURL != "https://media.test/b.jpg" {
		t.Errorf("last source's actor avatar was not preserved: %+v %v", actor, err)
	}
}

func TestParallelScanStopsWorkersOnAbort(t *testing.T) {
	for _, mode := range []string{"write-failure", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for i := 0; i < 500; i++ {
				writeScanFile(t, root, fmt.Sprintf("%04d.strm", i), "https://media.test/movie.mp4")
			}
			database, library := scanLibrary(t, root)
			countWorkers := func() int {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				return strings.Count(string(buf[:n]), "\nemby-go/internal/scanner.scanDirectory.func")
			}
			before := countWorkers()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result, err := ScanWithProgress(ctx, database, library, func(progress Progress) {
				if progress.Phase != PhaseProcess {
					return
				}
				if mode == "write-failure" && progress.Done == 0 {
					database.Close()
				} else if mode == "cancelled" && progress.Done == 1 {
					cancel()
				}
			})
			if err == nil {
				t.Fatalf("expected %s error: %+v", mode, result)
			}
			if mode == "write-failure" && result.Added != 0 {
				t.Errorf("failed batch reported additions: %+v", result)
			}
			if mode == "cancelled" && (!errors.Is(err, context.Canceled) || result.Added == 0) {
				t.Errorf("cancellation lost completed batch: %+v %v", result, err)
			}
			deadline := time.Now().Add(time.Second)
			after := countWorkers()
			for after > before && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				after = countWorkers()
			}
			if after > before {
				t.Errorf("scan goroutines remain after %s: before=%d after=%d", mode, before, after)
			}
		})
	}
}

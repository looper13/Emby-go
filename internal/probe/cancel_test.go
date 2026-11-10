package probe

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestProbePreservesContextErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-ffprobe")
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = context.DeadlineExceeded
		}
		cancel()
		if _, err := Run(ctx, missing, "https://media.test/video", time.Second); !errors.Is(err, want) {
			t.Errorf("Run lost context cause: want=%v err=%v", want, err)
		}
		if err := Verify(ctx, missing); !errors.Is(err, want) {
			t.Errorf("Verify lost context cause: want=%v err=%v", want, err)
		}
	}
}

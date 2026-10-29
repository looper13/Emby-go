package offline

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emby-go/internal/cloud115"
)

type blockingQueueFixture struct {
	providerFixture
	slowHash, cleanupName       string
	slowStarted, cleanupStarted chan struct{}
	slowRelease, cleanupRelease chan struct{}
	slowOnce, cleanupOnce       sync.Once
	slowCalls, fastCalls        atomic.Int32
}

func (p *blockingQueueFixture) Find(ctx context.Context, hash string) (*cloud115.RemoteTask, error) {
	if hash == p.slowHash {
		p.slowCalls.Add(1)
		p.slowOnce.Do(func() { close(p.slowStarted) })
		select {
		case <-p.slowRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		p.fastCalls.Add(1)
	}
	return &cloud115.RemoteTask{Status: 1, Progress: 77, FolderID: "cache"}, nil
}
func (p *blockingQueueFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	if id == "cleanup" {
		p.cleanupOnce.Do(func() { close(p.cleanupStarted) })
		select {
		case <-p.cleanupRelease:
		case <-ctx.Done():
			return cloud115.File{}, ctx.Err()
		}
		return cloud115.File{ID: id, ParentID: "work", Name: p.cleanupName, Directory: true}, nil
	}
	return p.providerFixture.Info(ctx, id)
}

func TestOfflineBoundedSlotsAvoidSlowCleanupAndDuplicateTaskProcessing(t *testing.T) {
	db, l, video := setup(t)
	p := &blockingQueueFixture{slowHash: strings.Repeat("a", 40), slowStarted: make(chan struct{}), cleanupStarted: make(chan struct{}), slowRelease: make(chan struct{}), cleanupRelease: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(p.slowRelease); close(p.cleanupRelease) }) }
	defer release()
	var fastID string
	for _, hash := range []string{p.slowHash, strings.Repeat("b", 40), strings.Repeat("c", 40)} {
		task, err := db.CreateOfflineTask("account", "work", "magnet:?xt=urn:btih:"+hash, hash, l, video, hash)
		if err != nil {
			t.Fatal(err)
		}
		task.Status, task.CacheFolderID, task.ExpiresAt = "downloading", "cache", time.Now().Add(time.Hour).Unix()
		if hash[0] == 'b' {
			fastID = task.ID
		}
		if hash[0] == 'c' {
			task.ExpiresAt = time.Now().Add(-time.Second).Unix()
			task.CacheFolderID = "cleanup"
			p.cleanupName = "emby-go-" + task.ID
		}
		if err := db.UpdateOfflineTask(task, task.Version); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{db: db, ctx: context.Background(), provider: func() (cloud115.Provider, string, error) { return p, "account", nil }}
	cleanupDone, slowDone := make(chan struct{}), make(chan struct{})
	go func() { s.tickCleanup(); close(cleanupDone) }()
	select {
	case <-p.cleanupStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not start")
	}
	// Claim the slow task explicitly so random UUID ordering does not affect the test.
	due, _ := db.DueOfflineTasks("account", time.Now().Unix())
	for _, task := range due {
		if task.InfoHash != p.slowHash {
			task.NextPoll = time.Now().Add(time.Minute).Unix()
			db.UpdateOfflineTask(task, task.Version)
		}
	}
	go func() { s.tick(); close(slowDone) }()
	select {
	case <-p.slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow download did not start")
	}
	fast := read(t, db, fastID)
	fast.NextPoll = 0
	db.UpdateOfflineTask(fast, fast.Version)
	s.tick()
	if read(t, db, fastID).Progress != 77 || p.slowCalls.Load() != 1 || p.fastCalls.Load() != 1 {
		t.Fatal("slow I/O monopolized slots or duplicated task", p.slowCalls.Load(), p.fastCalls.Load())
	}
	release()
	select {
	case <-slowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("slow slot did not finish")
	}
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup slot did not finish")
	}
}

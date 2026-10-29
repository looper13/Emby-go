package offline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type cacheFixture struct {
	providerFixture
	entries      map[string]cloud115.File
	deleted      []string
	removed      int
	deleteError  error
	mkdirTimeout bool
}

func (p *cacheFixture) Info(_ context.Context, id string) (cloud115.File, error) {
	f, ok := p.entries[id]
	if !ok {
		return cloud115.File{}, &cloud115.Error{Message: "missing", Missing: true}
	}
	return f, nil
}
func (p *cacheFixture) List(_ context.Context, id string, _ int) ([]cloud115.File, bool, error) {
	files := []cloud115.File{}
	for _, f := range p.entries {
		if f.ParentID == id {
			files = append(files, f)
		}
	}
	return files, false, nil
}
func (p *cacheFixture) Mkdir(_ context.Context, parent, name string) (string, error) {
	p.entries["cache"] = cloud115.File{ID: "cache", Name: name, ParentID: parent, Directory: true}
	if p.mkdirTimeout {
		return "", &cloud115.Error{Message: "mkdir timed out after write", Retryable: true}
	}
	return "cache", nil
}
func (p *cacheFixture) Delete(_ context.Context, id string) error {
	p.deleted = append(p.deleted, id)
	if p.deleteError != nil {
		return p.deleteError
	}
	delete(p.entries, id)
	return nil
}
func (p *cacheFixture) RemoveTask(context.Context, string) error {
	p.removed++
	p.remote = nil
	return nil
}
func newCacheFixture() *cacheFixture {
	return &cacheFixture{entries: map[string]cloud115.File{"work": {ID: "work", ParentID: "0", Name: cloud115.CacheFolderName, Directory: true}}}
}
func TestCacheExpiryCleanupRestartAndResubmit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "restart.db")
	s, err := store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.EncodeID("test", "digital")
	task := create(t, s, l, video)
	p := newCacheFixture()
	service := &Service{db: s, ctx: context.Background(), wake: make(chan struct{}, 1), provider: func() (cloud115.Provider, string, error) { return p, "account", nil }}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.CacheFolderID != "cache" || task.ExpiresAt < time.Now().Add(24*time.Hour-time.Minute).Unix() || p.adds != 1 {
		t.Fatal(task)
	}
	p.remote = &cloud115.RemoteTask{Status: 2, Progress: 100, ResultID: "result", FolderID: "cache"}
	p.entries["result"] = cloud115.File{ID: "result", ParentID: "cache", Name: "movie.mp4", PickCode: "pick", Size: 100}
	service.process(p, task)
	service.process(p, read(t, s, task.ID))
	task = read(t, s, task.ID)
	if task.Status != "ready" {
		t.Fatal(task)
	}
	service.tick()
	if len(p.deleted) != 0 {
		t.Fatal("deleted before expiry")
	}
	task.ExpiresAt = time.Now().Add(-time.Second).Unix()
	s.UpdateOfflineTask(task, task.Version)
	if err := s.SelectOfflineFile(l, video, task.ID, "result", true); err == nil {
		t.Fatal("selected expired file")
	}
	if err := service.Retry(task.ID, task.Version+1); err == nil {
		t.Fatal("expiry extended by retry")
	}
	p.deleteError = &cloud115.Error{Message: "network down", Retryable: true}
	service.tick()
	task = read(t, s, task.ID)
	if task.Status != "cleanup_pending" || len(p.deleted) != 1 || p.deleted[0] != "cache" || p.removed != 1 {
		t.Fatal(task, p)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service.db = reopened
	task = read(t, reopened, task.ID)
	task.NextPoll = 0
	reopened.UpdateOfflineTask(task, task.Version)
	p.deleteError = nil
	service.tick()
	task = read(t, reopened, task.ID)
	if task.Status != "expired" || len(task.Files) != 0 || len(p.deleted) != 2 {
		t.Fatal(task, p.deleted)
	}
	again, err := reopened.CreateOfflineTask("account", "work", task.Magnet, task.InfoHash, l, video, "1")
	if err != nil || again.Status != "queued" || again.CacheFolderID != "" || again.ExpiresAt != 0 {
		t.Fatal(again, err)
	}
	bindings, _ := reopened.OfflineBindings(l, video, "account")
	if len(bindings) != 1 || bindings[0].FileID != "" {
		t.Fatal("stale file binding", bindings)
	}
	service.process(p, again)
	if p.adds != 2 {
		t.Fatal("expired magnet did not resubmit", p.adds)
	}
}

func TestCacheCleanupDoesNotDeleteMovedRenamedOrExternalPaths(t *testing.T) {
	for _, change := range []string{"moved", "renamed", "root", "file"} {
		t.Run(change, func(t *testing.T) {
			s, l, video := setup(t)
			task := create(t, s, l, video)
			task.Status, task.CacheFolderID, task.ExpiresAt = "cancelled", "cache", time.Now().Add(-time.Second).Unix()
			s.UpdateOfflineTask(task, task.Version)
			p := newCacheFixture()
			child := cloud115.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
			switch change {
			case "moved":
				child.ParentID = "external"
			case "renamed":
				child.Name = "permanent"
			case "root":
				root := p.entries["work"]
				root.Name = "permanent"
				p.entries["work"] = root
			case "file":
				child.Directory = false
			}
			p.entries["cache"] = child
			p.remote = &cloud115.RemoteTask{FolderID: "cache"}
			service := &Service{db: s, ctx: context.Background()}
			service.cleanup(p, read(t, s, task.ID))
			if len(p.deleted) != 0 || p.removed != 0 || read(t, s, task.ID).Status != "cleanup_pending" {
				t.Fatal("unsafe deletion", p)
			}
		})
	}
}

func TestCacheMkdirTimeoutIsReconciledAndCleanedAfterCancel(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	p := newCacheFixture()
	p.mkdirTimeout = true
	service := &Service{db: s, ctx: context.Background(), wake: make(chan struct{}, 1)}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.CacheFolderID != "" || task.ExpiresAt == 0 || p.adds != 0 {
		t.Fatal(task)
	}
	if err := service.Cancel(task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s, task.ID)
	task.ExpiresAt = time.Now().Add(-time.Second).Unix()
	s.UpdateOfflineTask(task, task.Version)
	service.cleanup(p, read(t, s, task.ID))
	if len(p.deleted) != 1 || read(t, s, task.ID).Status != "expired" {
		t.Fatal("orphan cache not recovered", p.deleted)
	}
}

func TestCacheRefusesExternalMagnetAndResult(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	p := newCacheFixture()
	p.remote = &cloud115.RemoteTask{Status: 2, FolderID: "external", ResultID: "external-file"}
	service := &Service{db: s, ctx: context.Background()}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.Status != "failed" || task.Stage != "cache" || p.adds != 0 {
		t.Fatal(task)
	}
	p.entries["external-file"] = cloud115.File{ID: "external-file", ParentID: "0", Name: "permanent.mp4", Size: 100, PickCode: "private"}
	task.Status, task.ResultID = "discovering", "external-file"
	s.UpdateOfflineTask(task, task.Version)
	service.process(p, read(t, s, task.ID))
	if !strings.Contains(read(t, s, task.ID).Error, "拒绝") {
		t.Fatal("external result published")
	}
	task = read(t, s, task.ID)
	task.ExpiresAt = time.Now().Add(-time.Second).Unix()
	s.UpdateOfflineTask(task, task.Version)
	service.cleanup(p, read(t, s, task.ID))
	if p.removed != 0 || len(p.deleted) != 1 || p.deleted[0] != "cache" {
		t.Fatal("external task modified", p)
	}
}

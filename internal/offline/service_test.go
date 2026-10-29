package offline

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type providerFixture struct {
	adds     int
	remote   *cloud115.RemoteTask
	files    []cloud115.File
	addError error
	onInfo   func()
}

func (p *providerFixture) Check(context.Context, string) (string, error) { return "account", nil }
func (p *providerFixture) Find(context.Context, string) (*cloud115.RemoteTask, error) {
	return p.remote, nil
}
func (p *providerFixture) Add(context.Context, string, string) error { p.adds++; return p.addError }
func (p *providerFixture) Info(context.Context, string) (cloud115.File, error) {
	if p.onInfo != nil {
		p.onInfo()
	}
	return cloud115.File{ID: "result", Name: "result", Directory: true}, nil
}
func (p *providerFixture) List(context.Context, string, int) ([]cloud115.File, bool, error) {
	return p.files, false, nil
}
func (p *providerFixture) URL(context.Context, string, string) (string, error) { return "", nil }
func setup(t *testing.T) (*store.Store, store.Library, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	l, err := s.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	return s, l, r18.EncodeID("test", "digital")
}
func create(t *testing.T, s *store.Store, l store.Library, video string) store.OfflineTask {
	t.Helper()
	magnet, hash, _ := cloud115.NormalizeMagnet("magnet:?xt=urn:btih:" + strings.Repeat("a", 40))
	task, err := s.CreateOfflineTask("account", "work", magnet, hash, l, video, "1")
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func read(t *testing.T, s *store.Store, id string) store.OfflineTask {
	t.Helper()
	task, err := s.OfflineTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func TestOfflineDedupeLifecycleAndSelection(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ids <- create(t, s, l, video).ID }()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != task.ID {
			t.Fatal("duplicate task")
		}
	}
	service := &Service{db: s, ctx: context.Background(), wake: make(chan struct{}, 1)}
	p := &providerFixture{}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.Status != "downloading" || p.adds != 1 {
		t.Fatal(task, p.adds)
	}
	p.remote = &cloud115.RemoteTask{Status: 2, Progress: 100, ResultID: "result"}
	service.process(p, task)
	task = read(t, s, task.ID)
	p.files = []cloud115.File{{ID: "a", Name: "one.mp4", PickCode: "pick-a", Size: 100}, {ID: "b", Name: "two.mkv", PickCode: "pick-b", Size: 200}}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.Status != "waiting_selection" {
		t.Fatal(task.Status)
	}
	if err := s.SelectOfflineFile(l, video, task.ID, "forged", true); err == nil {
		t.Fatal("forged file accepted")
	}
	if err := s.SelectOfflineFile(l, video, task.ID, "b", true); err != nil {
		t.Fatal(err)
	}
	bindings, err := s.OfflineBindings(l, video, "account")
	if err != nil || len(bindings) != 1 || bindings[0].FileID != "b" || !bindings[0].Default {
		t.Fatal(bindings, err)
	}
	if p.adds != 1 {
		t.Fatal("resubmitted")
	}
}
func TestOfflineUncertainSubmissionRecoveryNeverResubmits(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	service := &Service{db: s, ctx: context.Background()}
	p := &providerFixture{addError: &cloud115.Error{Message: "timeout after submit", Retryable: true}}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.Status != "submitting" {
		t.Fatal(task.Status)
	}
	// A fresh service uses only the persisted uncertain state.
	restarted := &Service{db: s, ctx: context.Background()}
	restarted.process(p, task)
	if p.adds != 1 {
		t.Fatal("submitted twice")
	}
	p.remote = &cloud115.RemoteTask{Status: 1, Progress: 42}
	restarted.process(p, read(t, s, task.ID))
	task = read(t, s, task.ID)
	if task.Status != "downloading" || task.Progress != 42 {
		t.Fatal(task)
	}
}
func TestOfflineCancellationCASAuthAndRetry(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	task.Status = "discovering"
	task.ResultID = "result"
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s, task.ID)
	service := &Service{db: s, ctx: context.Background(), wake: make(chan struct{}, 1)}
	p := &providerFixture{files: []cloud115.File{{ID: "a", Name: "movie.mp4", PickCode: "pick", Size: 100}}}
	p.onInfo = func() {
		if err := service.Cancel(task.ID, task.Version); err != nil {
			t.Fatal(err)
		}
	}
	service.process(p, task)
	task = read(t, s, task.ID)
	if task.Status != "cancelled" {
		t.Fatal("cancel resurrected", task.Status)
	}
	if err := service.Retry(task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s, task.ID)
	if task.Status != "discovering" {
		t.Fatal(task.Status)
	}
	service.fail(task, task.Version, "discover", &cloud115.Error{Message: "expired", Auth: true})
	task = read(t, s, task.ID)
	if task.Status != "needs_auth" || !task.Retryable {
		t.Fatal(task)
	}
	if err := service.Retry(task.ID, task.Version-1); err == nil {
		t.Fatal("stale retry accepted")
	}
	service.fail(read(t, s, task.ID), task.Version, "test", errors.New("secret raw error"))
	if strings.Contains(read(t, s, task.ID).Error, "secret") {
		t.Fatal("raw error leaked")
	}
}
func TestOfflineTimeoutPreservesCreationAndSingleFileAutoBinds(t *testing.T) {
	s, l, video := setup(t)
	task := create(t, s, l, video)
	task.Deadline = time.Now().Add(-time.Second).Unix()
	s.UpdateOfflineTask(task, task.Version)
	task = read(t, s, task.ID)
	service := &Service{db: s, ctx: context.Background(), wake: make(chan struct{}, 1)}
	service.process(&providerFixture{}, task)
	task = read(t, s, task.ID)
	created := task.CreatedAt
	if task.Status != "timed_out" {
		t.Fatal(task.Status)
	}
	if err := service.Retry(task.ID, task.Version); err != nil {
		t.Fatal(err)
	}
	task = read(t, s, task.ID)
	if task.CreatedAt != created {
		t.Fatal("creation timestamp changed")
	}
	task.Status = "discovering"
	task.ResultID = "result"
	s.UpdateOfflineTask(task, task.Version)
	task = read(t, s, task.ID)
	service.process(&providerFixture{files: []cloud115.File{{ID: "a", Name: "movie.mp4", Size: 100, PickCode: "pick"}}}, task)
	bindings, _ := s.OfflineBindings(l, video, "account")
	if bindings[0].FileID != "a" || bindings[0].Task.Status != "ready" {
		t.Fatal(bindings)
	}
}

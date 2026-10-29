package offline

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

type latencyFixture struct {
	*cacheFixture
	requests []string
	remotes  map[string]*cloud115.RemoteTask
	onFind   func()
	onList   func()
}

func TestOfflineAcceptedVisibilityGracePersistsAndNeverResubmits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visibility.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	l, err := db.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	task := create(t, db, l, r18.EncodeID("test", "digital"))
	p := newCacheFixture()
	s := &Service{db: db, ctx: context.Background()}
	s.process(p, task)
	task = read(t, db, task.ID)
	deadline := task.VisibilityUntil
	if p.adds != 1 || deadline <= time.Now().Unix() {
		t.Fatal("accepted Add has no visibility grace", task)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	for i := 0; i < 3; i++ {
		s.process(p, read(t, db, task.ID))
		current := read(t, db, task.ID)
		if p.adds != 1 || current.Attempts != 0 || current.VisibilityUntil != deadline || current.NextPoll > time.Now().Unix()+1 {
			t.Fatal("absence delayed polling, extended grace, or resubmitted", current)
		}
	}
	current := read(t, db, task.ID)
	current.VisibilityUntil = time.Now().Add(-time.Second).Unix()
	db.UpdateOfflineTask(current, current.Version)
	s.process(p, read(t, db, task.ID))
	current = read(t, db, task.ID)
	if p.adds != 1 || current.Attempts != 1 || current.NextPoll < time.Now().Unix()+19 {
		t.Fatal("unbounded fast polling after grace", current)
	}
	p.remote = &cloud115.RemoteTask{Status: 1, Progress: 40, FolderID: "cache"}
	s.process(p, current)
	current = read(t, db, task.ID)
	if current.VisibilityUntil != 0 || current.Attempts != 0 || current.Progress != 40 {
		t.Fatal("visible task did not resume normal polling", current)
	}
}

func TestOfflineAmbiguousAddAndNetworkErrorsKeepBackoff(t *testing.T) {
	for _, scenario := range []string{"ambiguous_add", "network_after_accept"} {
		t.Run(scenario, func(t *testing.T) {
			db, l, video := setup(t)
			task := create(t, db, l, video)
			p := newCacheFixture()
			s := &Service{db: db, ctx: context.Background()}
			if scenario == "ambiguous_add" {
				p.addError = &cloud115.Error{Message: "timeout", Retryable: true}
			}
			s.process(p, task)
			if scenario == "network_after_accept" {
				p.findError = &cloud115.Error{Message: "network", Retryable: true}
			}
			s.process(p, read(t, db, task.ID))
			current := read(t, db, task.ID)
			if p.adds != 1 || current.Attempts < 1 || current.NextPoll < time.Now().Unix()+19 {
				t.Fatal("uncertain Add/network failure used visibility grace", current)
			}
		})
	}
}

func (p *latencyFixture) Info(ctx context.Context, id string) (cloud115.File, error) {
	p.requests = append(p.requests, "info:"+id)
	return p.cacheFixture.Info(ctx, id)
}
func (p *latencyFixture) Find(ctx context.Context, hash string) (*cloud115.RemoteTask, error) {
	p.requests = append(p.requests, "find:"+hash)
	if p.onFind != nil {
		p.onFind()
	}
	if p.remotes != nil {
		return p.remotes[hash], nil
	}
	return p.cacheFixture.Find(ctx, hash)
}
func (p *latencyFixture) List(ctx context.Context, id string, offset int) ([]cloud115.File, bool, error) {
	p.requests = append(p.requests, "list:"+id)
	if p.onList != nil {
		p.onList()
	}
	return p.cacheFixture.List(ctx, id, offset)
}
func (p *latencyFixture) Delete(ctx context.Context, id string) error {
	p.requests = append(p.requests, "delete:"+id)
	return p.cacheFixture.Delete(ctx, id)
}

func TestOfflineSubmissionSchedulesFirstPollAfterOneSecond(t *testing.T) {
	db, library, video := setup(t)
	task := create(t, db, library, video)
	p := newCacheFixture()
	s := &Service{db: db, ctx: context.Background()}
	before := time.Now().Unix()
	s.process(p, task)
	current := read(t, db, task.ID)
	if current.Status != "downloading" || p.adds != 1 || current.NextPoll < before+1 || current.NextPoll > time.Now().Unix()+1 || pollInterval != time.Second {
		t.Fatal("accepted submission must schedule its first poll after one second", current.NextPoll)
	}
}

func TestOfflineNormalPollSchedulePersistsWithoutChangingErrorBackoff(t *testing.T) {
	s, p, task := recoveryFixture(t)
	p.remote.Status = 1
	for _, progress := range []int{42, 42, 42, 43, 0, 0} {
		p.remote.Progress = progress
		before := time.Now().Unix()
		s.process(p, task)
		task = read(t, s.db, task.ID)
		if task.NextPoll < before+1 || task.NextPoll > time.Now().Unix()+1 {
			t.Fatal("normal polling must stay at one second even without progress", task.NextPoll)
		}
	}
	s.fail(task, task.Version, "poll", &cloud115.Error{Message: "network", Retryable: true})
	task = read(t, s.db, task.ID)
	if task.Attempts != 1 || task.NextPoll < time.Now().Unix()+19 {
		t.Fatal("error backoff was shortened", task.Attempts, task.NextPoll)
	}
}

func TestOfflineCompletedResultDiscoveredInOneTurnWithFreshInfoOnce(t *testing.T) {
	for _, scenario := range []string{"file", "directory", "nested", "restart_discovering"} {
		t.Run(scenario, func(t *testing.T) {
			s, cache, task := recoveryFixture(t)
			cache.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "movie.mp4", Size: 100, PickCode: "pick"}
			if scenario != "file" {
				cache.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "result", Directory: true}
				cache.entries["video"] = cloud115.File{ID: "video", ParentID: "old-result", Name: "movie.mp4", Size: 100, PickCode: "pick"}
			}
			if scenario == "nested" {
				cache.entries["parent"] = cloud115.File{ID: "parent", ParentID: "cache", Name: "parent", Directory: true}
				f := cache.entries["old-result"]
				f.ParentID = "parent"
				cache.entries["old-result"] = f
			}
			if scenario == "restart_discovering" {
				task.Status, task.ResultID = "discovering", "old-result"
			}
			if s.db.UpdateOfflineTask(task, task.Version) != nil {
				t.Fatal("fixture update")
			}
			p := &latencyFixture{cacheFixture: cache}
			s.process(p, read(t, s.db, task.ID))
			current := read(t, s.db, task.ID)
			if current.Status != "ready" || len(current.Files) != 1 || p.adds != 0 || current.ExpiresAt != task.ExpiresAt {
				t.Fatal("completion must publish within this turn without resubmitting or renewing", current.Status)
			}
			count := 0
			for _, call := range p.requests {
				if call == "info:old-result" {
					count++
				}
			}
			if count != 1 {
				t.Fatal("duplicate result Info", p.requests)
			}
		})
	}
}

func TestOfflineImmediateDiscoveryKeepsCancellationCAS(t *testing.T) {
	s, cache, task := recoveryFixture(t)
	cache.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "result", Directory: true}
	cache.entries["video"] = cloud115.File{ID: "video", ParentID: "old-result", Name: "movie.mp4", Size: 100, PickCode: "pick"}
	p := &latencyFixture{cacheFixture: cache}
	p.onList = func() {
		p.onList = nil
		current := read(t, s.db, task.ID)
		if current.Status != "discovering" || s.Cancel(current.ID, current.Version) != nil {
			t.Fatal("durable discovery boundary not cancellable")
		}
	}
	s.process(p, task)
	if current := read(t, s.db, task.ID); current.Status != "cancelled" || len(current.Files) != 0 {
		t.Fatal("late discovery resurrected cancellation", current.Status)
	}
}

func TestOfflineFreshMkdirSkipsLookupButUncertainMkdirReconciles(t *testing.T) {
	db, library, video := setup(t)
	task := create(t, db, library, video)
	p := &latencyFixture{cacheFixture: newCacheFixture()}
	p.mkdirTimeout = true
	s := &Service{db: db, ctx: context.Background()}
	s.process(p, task)
	for _, call := range p.requests {
		if call == "list:work" {
			t.Fatal("fresh task scanned cache root", p.requests)
		}
	}
	p.requests, p.mkdirTimeout = nil, false
	s.process(p, read(t, db, task.ID))
	if current := read(t, db, task.ID); current.CacheFolderID != "cache" || current.Status != "downloading" || p.adds != 1 {
		t.Fatal("uncertain mkdir not reconciled", current.Status, p.adds)
	}
	if !strings.Contains(strings.Join(p.requests, ","), "list:work") {
		t.Fatal("retry failed to reconcile durable mkdir intent", p.requests)
	}
}

func TestOfflineTickSeparatesCleanupAndRunsOneTaskPerDispatch(t *testing.T) {
	db, library, video := setup(t)
	p := &latencyFixture{cacheFixture: newCacheFixture(), remotes: map[string]*cloud115.RemoteTask{}}
	for _, hash := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)} {
		task, err := db.CreateOfflineTask("account", "work", "magnet:?xt=urn:btih:"+hash, hash, library, video, hash)
		if err != nil {
			t.Fatal(err)
		}
		task.Status, task.CacheFolderID = "downloading", hash
		task.ExpiresAt = time.Now().Add(time.Hour).Unix()
		if hash[0] == 'c' {
			task.ExpiresAt = time.Now().Add(-time.Second).Unix()
		} else {
			p.remotes[hash] = &cloud115.RemoteTask{Status: 1, Progress: 42, FolderID: hash}
		}
		p.entries[hash] = cloud115.File{ID: hash, ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
		if db.UpdateOfflineTask(task, task.Version) != nil {
			t.Fatal("fixture update")
		}
	}
	due, err := db.DueOfflineTasks("account", time.Now().Unix())
	if err != nil || len(due) != 2 {
		t.Fatal("expired work consumed a download slot", len(due), err)
	}
	s := &Service{db: db, ctx: context.Background(), wake: make(chan struct{}, 1), provider: func() (cloud115.Provider, string, error) { return p, "account", nil }}
	p.onFind = func() { p.onFind = nil; s.Wake() }
	s.tick()
	if !reflect.DeepEqual(p.requests, []string{"find:" + due[0].InfoHash}) {
		t.Fatal("download dispatch included cleanup or drained the entire queue", p.requests)
	}
	s.tickCleanup()
	want := []string{"find:" + due[0].InfoHash, "info:" + strings.Repeat("c", 40), "info:work", "find:" + strings.Repeat("c", 40), "delete:" + strings.Repeat("c", 40)}
	if !reflect.DeepEqual(p.requests, want) || len(s.wake) != 1 || read(t, db, due[1].ID).Progress != 0 {
		t.Fatal("separate queues lost work or repeated cleanup", p.requests)
	}
	<-s.wake
	s.tick()
	if read(t, db, due[1].ID).Progress != 42 || len(p.deleted) != 1 {
		t.Fatal("yield lost pending work or repeated cleanup", p.requests)
	}
}

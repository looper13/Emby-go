package offline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
	"emby-go/internal/store"
)

func recoveryFixture(t *testing.T) (*Service, *cacheFixture, store.OfflineTask) {
	t.Helper()
	db, l, video := setup(t)
	task := create(t, db, l, video)
	task.Status, task.CacheFolderID = "downloading", "cache"
	task.ExpiresAt = time.Now().Add(time.Hour).Unix()
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	p := newCacheFixture()
	p.entries["cache"] = cloud115.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
	p.remote = &cloud115.RemoteTask{Status: 2, Progress: 100, ResultID: "old-result", FolderID: "cache"}
	return &Service{db: db, ctx: context.Background(), wake: make(chan struct{}, 1)}, p, read(t, db, task.ID)
}

func runRecoveryUntilAdd(t *testing.T, s *Service, p *cacheFixture, id string) {
	t.Helper()
	for i := 0; i < 8 && p.adds == 0; i++ {
		s.process(p, read(t, s.db, id))
	}
	if p.adds != 1 {
		t.Fatal("missing result was not automatically resubmitted", read(t, s.db, id), p.adds)
	}
}

func TestOfflineMissingResultsAutomaticallyRedownload(t *testing.T) {
	for _, scenario := range []string{"owned_missing", "old_cache_record", "external_missing", "external_empty_result", "empty_result", "selected_file_missing", "record_already_gone", "cache_also_deleted"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, task := recoveryFixture(t)
			originalExpiry, originalCreation := task.ExpiresAt, task.CreatedAt
			expectedRemoves := 1
			switch scenario {
			case "old_cache_record":
				p.remote.FolderID = "retired-cache"
			case "external_missing":
				p.remote.FolderID = "external"
			case "external_empty_result":
				p.remote.FolderID = "external"
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "external", Name: "empty", Directory: true}
			case "empty_result":
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "empty", Directory: true}
			case "selected_file_missing":
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "result", Directory: true}
				p.entries["keep"] = cloud115.File{ID: "keep", ParentID: "old-result", Name: "attachment.mp4", Size: 10, PickCode: "keep"}
				task.Status, task.ResultID, task.RecoveryFileID = "discovering", "old-result", "deleted-main"
			case "record_already_gone":
				task.Status, task.ResultID = "discovering", "old-result"
				p.remote, expectedRemoves = nil, 0
			case "cache_also_deleted":
				delete(p.entries, "cache")
				p.newCacheID = "new-cache"
			}
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			runRecoveryUntilAdd(t, s, p, task.ID)
			current := read(t, s.db, task.ID)
			if p.removed != expectedRemoves || len(p.deleted) != 0 || current.ExpiresAt != originalExpiry || current.CreatedAt != originalCreation || current.Recoveries != 1 || current.RecoveryResultID != "" || current.Status != "downloading" {
				t.Fatal("repair changed ownership, cache expiry, or submission boundary", current, p.removed, p.deleted)
			}
			if scenario == "selected_file_missing" {
				if _, ok := p.entries["keep"]; !ok {
					t.Fatal("repair deleted an existing file")
				}
			}
			if scenario == "cache_also_deleted" && current.CacheFolderID != "new-cache" {
				t.Fatal("deleted cache child was not recreated", current)
			}
			// An accepted Add that is not yet visible must never be sent again.
			s.process(p, current)
			if p.adds != 1 {
				t.Fatal("repeated an uncertain submission")
			}
		})
	}
}

func TestOfflineRecoveryDoesNotTreatFailuresAsDeletion(t *testing.T) {
	for _, scenario := range []string{"network", "auth", "auth_missing", "existing_file", "external_existing_file", "existing_nonvideo_directory", "external_nonempty_directory", "uncompleted", "invalid_result", "recovery_limit"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, task := recoveryFixture(t)
			switch scenario {
			case "network":
				p.infoErrors = map[string]error{"old-result": &cloud115.Error{Message: "network unavailable", Retryable: true}}
			case "auth":
				p.infoErrors = map[string]error{"old-result": &cloud115.Error{Message: "validation required", Auth: true}}
			case "auth_missing":
				p.infoErrors = map[string]error{"old-result": &cloud115.Error{Message: "validation required", Auth: true, Missing: true}}
			case "existing_file", "external_existing_file":
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "movie.mp4", Size: 100, PickCode: "pick"}
				if scenario == "external_existing_file" {
					p.remote.FolderID = "external"
				}
			case "existing_nonvideo_directory", "external_nonempty_directory":
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "result", Directory: true}
				p.entries["photo"] = cloud115.File{ID: "photo", ParentID: "old-result", Name: "photo.jpg", Size: 100}
				if scenario == "external_nonempty_directory" {
					p.remote.FolderID = "external"
				}
			case "uncompleted":
				p.remote.Status = 1
			case "invalid_result":
				p.remote.ResultID = "0"
			case "recovery_limit":
				task.Recoveries = maxAutomaticRecoveries
			}
			if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				s.process(p, read(t, s.db, task.ID))
			}
			if p.removed != 0 || p.adds != 0 || len(p.deleted) != 0 || read(t, s.db, task.ID).RecoveryResultID != "" {
				t.Fatal("removed or resubmitted an unproven missing result", scenario, p, read(t, s.db, task.ID))
			}
		})
	}
}

func TestOfflineRecoveryPublishesOnlyReplacementFiles(t *testing.T) {
	db, l, video := setup(t)
	task := create(t, db, l, video)
	task.Status, task.CacheFolderID, task.ResultID = "ready", "cache", "old-result"
	task.ExpiresAt = time.Now().Add(time.Hour).Unix()
	task.Files = []cloud115.File{{ID: "old-result", Name: "old.mp4", Size: 100, PickCode: "old-pick"}}
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if err := db.SelectOfflineFile(l, video, task.ID, "old-result", true); err != nil {
		t.Fatal(err)
	}
	task = read(t, db, task.ID)
	// The streaming endpoint enqueues this state after a missing-file response.
	task.Status, task.RecoveryFileID, task.Files = "discovering", "old-result", []cloud115.File{}
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	p := newCacheFixture()
	p.entries["cache"] = cloud115.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
	p.remote = &cloud115.RemoteTask{Status: 2, ResultID: "old-result", FolderID: "cache"}
	s := &Service{db: db, ctx: context.Background()}
	runRecoveryUntilAdd(t, s, p, task.ID)
	bindings, err := db.OfflineBindings(l, video, "account")
	if err != nil || len(bindings) != 1 || bindings[0].FileID != "" || bindings[0].Default {
		t.Fatal("stale source remained published", bindings, err)
	}
	p.remote = &cloud115.RemoteTask{Status: 2, Progress: 100, ResultID: "replacement", FolderID: "cache"}
	p.entries["replacement"] = cloud115.File{ID: "replacement", ParentID: "cache", Name: "new.mp4", Size: 200, PickCode: "new-pick"}
	s.process(p, read(t, db, task.ID))
	s.process(p, read(t, db, task.ID))
	bindings, err = db.OfflineBindings(l, video, "account")
	if err != nil || len(bindings) != 1 || bindings[0].Task.Status != "ready" || bindings[0].FileID != "replacement" || len(bindings[0].Task.Files) != 1 || bindings[0].Task.Files[0].ID != "replacement" || p.adds != 1 {
		t.Fatal("replacement source was not published once", bindings, p.adds, err)
	}
}

func TestOfflineMissingPlaybackFollowsReplacementWithoutRemovingIt(t *testing.T) {
	s, p, task := recoveryFixture(t)
	task.Status, task.ResultID, task.RecoveryFileID = "discovering", "old-result", "deleted-main"
	if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	p.remote = &cloud115.RemoteTask{Status: 2, Progress: 100, ResultID: "replacement", FolderID: "cache"}
	p.entries["replacement"] = cloud115.File{ID: "replacement", ParentID: "cache", Name: "new.mp4", Size: 200, PickCode: "pick"}
	for i := 0; i < 3; i++ {
		s.process(p, read(t, s.db, task.ID))
	}
	current := read(t, s.db, task.ID)
	if p.removed != 0 || p.adds != 0 || current.Status != "ready" || current.ResultID != "replacement" {
		t.Fatal("old missing file caused the replacement record to be removed", current, p)
	}
}

func TestOfflineRecoveryRechecksRecordFilesAndCancellation(t *testing.T) {
	for _, scenario := range []string{"restored", "replaced", "replaced_during_probe", "cancelled", "cancelled_after_remove", "expired", "visibility_delay"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, task := recoveryFixture(t)
			s.process(p, task)
			task = read(t, s.db, task.ID)
			if task.RecoveryResultID == "" {
				t.Fatal("recovery intent not persisted", task)
			}
			switch scenario {
			case "restored":
				p.entries["old-result"] = cloud115.File{ID: "old-result", ParentID: "cache", Name: "restored.mp4", Size: 100, PickCode: "pick"}
			case "replaced":
				p.remote = &cloud115.RemoteTask{Status: 1, ResultID: "new-result", FolderID: "cache"}
			case "replaced_during_probe":
				p.onReadInfo = func(id string) {
					if id == "old-result" {
						p.onReadInfo = nil
						p.remote = &cloud115.RemoteTask{Status: 1, ResultID: "new-result", FolderID: "cache"}
					}
				}
			case "cancelled":
				p.onReadInfo = func(id string) {
					if id == "old-result" {
						p.onReadInfo = nil
						current := read(t, s.db, task.ID)
						if err := s.Cancel(current.ID, current.Version); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "cancelled_after_remove":
				p.onRemove = func() {
					current := read(t, s.db, task.ID)
					if err := s.Cancel(current.ID, current.Version); err != nil {
						t.Fatal(err)
					}
				}
			case "expired":
				task.ExpiresAt = time.Now().Add(-time.Second).Unix()
				if err := s.db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
				task = read(t, s.db, task.ID)
			case "visibility_delay":
				p.keepRemote = true
			}
			s.process(p, task)
			if scenario == "cancelled_after_remove" {
				s.process(p, read(t, s.db, task.ID))
				if p.removed != 1 || p.adds != 0 || read(t, s.db, task.ID).Status != "cancelled" {
					t.Fatal("late repair resurrected cancellation", p)
				}
				return
			}
			if scenario == "visibility_delay" {
				for i := 0; i < 10; i++ {
					s.process(p, read(t, s.db, task.ID))
				}
				if p.removed != 1 || p.adds != 0 || read(t, s.db, task.ID).Status != "failed" {
					t.Fatal("repeated deletion or Add while old record remained visible", p, read(t, s.db, task.ID))
				}
				return
			}
			if scenario == "expired" {
				if p.adds != 0 || read(t, s.db, task.ID).Status != "expired" {
					t.Fatal("expired task was repaired instead of cleaned", p)
				}
				return
			}
			if p.removed != 0 || p.adds != 0 || len(p.deleted) != 0 {
				t.Fatal("repair wrote after files/record/cancellation changed", p)
			}
			if scenario == "cancelled" && read(t, s.db, task.ID).Status != "cancelled" {
				t.Fatal("repair resurrected cancellation")
			}
		})
	}
}

func TestOfflineRecoveryPersistsAcrossSQLiteReopenAndAddTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.db")
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
	task.Status, task.CacheFolderID, task.ExpiresAt = "downloading", "cache", time.Now().Add(time.Hour).Unix()
	if err := db.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	p := newCacheFixture()
	p.entries["cache"] = cloud115.File{ID: "cache", ParentID: "work", Name: "emby-go-" + task.ID, Directory: true}
	p.remote = &cloud115.RemoteTask{Status: 2, ResultID: "old-result", FolderID: "cache"}
	s := &Service{db: db, ctx: context.Background()}
	s.process(p, read(t, db, task.ID))
	p.removeError = &cloud115.Error{Message: "remove timed out after write", Retryable: true}
	p.clearOnRemoveError = true
	s.process(p, read(t, db, task.ID))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	if read(t, db, task.ID).RecoveryResultID == "" {
		t.Fatal("lost ambiguous removal intent")
	}
	s.process(p, read(t, db, task.ID))
	p.addError = &cloud115.Error{Message: "Add timed out after write", Retryable: true}
	s.process(p, read(t, db, task.ID))
	if p.adds != 1 || read(t, db, task.ID).Status != "submitting" {
		t.Fatal("new Add boundary was not persisted", p, read(t, db, task.ID))
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.db = db
	s.process(p, read(t, db, task.ID))
	if p.adds != 1 || p.removed != 1 {
		t.Fatal("restart repeated a cloud write", p)
	}
}

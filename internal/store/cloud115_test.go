package store

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
)

func TestCloud115ConcurrentSelectionKeepsAtomicBindingsAndVersions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "selection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	l, err := s.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	video := r18.EncodeID("test", "digital")
	hash := strings.Repeat("a", 40)
	task, err := s.CreateOfflineTask("account", "work", "magnet:?xt=urn:btih:"+hash, hash, l, video, "1")
	if err != nil {
		t.Fatal(err)
	}
	task.Status, task.ExpiresAt = "waiting_selection", time.Now().Add(time.Hour).Unix()
	task.Files = []cloud115.File{{ID: "a", Name: "a.mp4", PickCode: "a", Size: 100}, {ID: "b", Name: "b.mp4", PickCode: "b", Size: 200}}
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	task, _ = s.OfflineTask(task.ID)
	start := make(chan struct{})
	errors := make(chan error, 24)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			file := "a"
			if i%2 == 1 {
				file = "b"
			}
			for j := 0; j < 2; j++ {
				errors <- s.SelectOfflineFile(l, video, task.ID, file, true)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal("concurrent selection lost a transaction", err)
		}
	}
	current, _ := s.OfflineTask(task.ID)
	bindings, err := s.OfflineBindings(l, video, "account")
	if err != nil || current.Version != task.Version+24 || current.ExpiresAt != task.ExpiresAt || len(bindings) != 1 || !bindings[0].Default || bindings[0].FileID != "a" && bindings[0].FileID != "b" {
		t.Fatal("selection lost version, expiry or binding atomicity", current, bindings, err)
	}
}

func TestCloud115ReopenAndStaleTokenWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "store.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := cloud115.OpenVault(filepath.Join(filepath.Dir(file), ".115.key"))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := v.Seal(cloud115.Tokens{Access: "access", Refresh: "refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCloud115Connection(Cloud115Connection{AccountID: "account", FolderID: "work", Secrets: secrets}, 0); err != nil {
		t.Fatal(err)
	}
	magnet, hash, _ := cloud115.NormalizeMagnet("magnet:?xt=urn:btih:" + strings.Repeat("a", 40))
	video := r18.EncodeID("test", "digital")
	task, err := s.CreateOfflineTask("account", "work", magnet, hash, l, video, "1")
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "submitting"
	if err = s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := s.Cloud115Connection()
	if err != nil || conn.AccountID != "account" {
		t.Fatal(conn, err)
	}
	tokens, err := v.Open(conn.Secrets)
	if err != nil || tokens.Access != "access" {
		t.Fatal(tokens, err)
	}
	read, err := s.OfflineTask(task.ID)
	if err != nil || read.Status != "submitting" || read.Version != 2 {
		t.Fatal(read, err)
	}
	bindings, err := s.OfflineBindings(l, video, "account")
	if err != nil || len(bindings) != 1 {
		t.Fatal(bindings, err)
	}
	if err = s.SaveCloud115Connection(conn, conn.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.RefreshCloud115Tokens("account", conn.Revision, secrets); err == nil {
		t.Fatal("old client replaced new config tokens")
	}
	if err = s.UpdateOfflineTask(read, read.Version-1); err == nil {
		t.Fatal("stale background update accepted")
	}
	rows, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestCloud115PollScheduleReopensAndExpiredWorkOnlyCleans(t *testing.T) {
	file := filepath.Join(t.TempDir(), "polling.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	l, err := s.CreateR18Library("R18", r18.Config{Host: "localhost", Port: 5432, Database: "test", Username: "user", SSLMode: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	magnet, hash, _ := cloud115.NormalizeMagnet("magnet:?xt=urn:btih:" + strings.Repeat("a", 40))
	task, err := s.CreateOfflineTask("account", "work", magnet, hash, l, r18.EncodeID("test", "digital"), "1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	task.Status, task.NextPoll, task.ExpiresAt = "downloading", now+1, now+3600
	if err := s.UpdateOfflineTask(task, task.Version); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.OfflineTask(task.ID)
	if err != nil || current.NextPoll != now+1 {
		t.Fatal("restart lost polling schedule", current.NextPoll, err)
	}
	due, err := s.DueOfflineTasks("account", now+1)
	if err != nil || len(due) != 1 {
		t.Fatal("unexpired download not due", len(due), err)
	}
	due, err = s.DueOfflineTasks("account", now+3600)
	if err != nil || len(due) != 0 {
		t.Fatal("expired task remained in download queue", len(due), err)
	}
	cleanup, err := s.DueOfflineCleanup("account", now+3600)
	if err != nil || len(cleanup) != 1 {
		t.Fatal("expired task lost its cleanup slot", len(cleanup), err)
	}
}

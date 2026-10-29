package store

import (
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/cloud115"
	"emby-go/internal/r18"
)

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

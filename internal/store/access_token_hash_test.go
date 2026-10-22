package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestAccessTokenHashUpgradeAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err := s.InitializeAdministrator("admin", "password-1234"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveUserToken("new-session-secret", 1); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow("SELECT token FROM access_tokens").Scan(&stored); err != nil || stored == "new-session-secret" {
		t.Fatalf("plaintext retained: %v", err)
	}
	if _, err := s.TokenUser(stored); err != sql.ErrNoRows {
		t.Fatalf("digest accepted as credential: %v", err)
	}
	if err := s.DeleteAccessToken("new-session-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TokenUser("new-session-secret"); err != sql.ErrNoRows {
		t.Fatalf("revoked credential accepted: %v", err)
	}
	// Reconstruct a database made by the previous version, retaining ownership.
	if _, err := s.db.Exec("DELETE FROM kv WHERE key=?", accessTokenHashMarker); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO access_tokens(token,created_at,user_id) VALUES('legacy-session-secret','original-time',1)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := s.TokenUser("legacy-session-secret"); err != nil || user.ID != 1 {
		t.Fatalf("legacy login lost: %+v %v", user, err)
	}
	var created string
	if err := s.db.QueryRow("SELECT token,created_at FROM access_tokens").Scan(&stored, &created); err != nil || stored != accessTokenDigest("legacy-session-secret") || created != "original-time" {
		t.Fatalf("migration changed identity or time: %v", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.HasAccessToken("legacy-session-secret"); err != nil || !ok {
		t.Fatalf("restart rehashed credential: %v", err)
	}
}

func TestAccessTokenHashMigrationRollsBack(t *testing.T) {
	s, _ := newFeatureStore(t)
	if _, err := s.db.Exec("DELETE FROM kv WHERE key=?", accessTokenHashMarker); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO access_tokens(token,created_at,user_id) VALUES('legacy','time',1);
CREATE TRIGGER reject_token_hash BEFORE UPDATE ON access_tokens BEGIN SELECT RAISE(ABORT,'reject upgrade'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateAccessTokenHashes(); err == nil {
		t.Fatal("failed update accepted")
	}
	var token string
	if err := s.db.QueryRow("SELECT token FROM access_tokens").Scan(&token); err != nil || token != "legacy" {
		t.Fatalf("partial upgrade: %v", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_token_hash"); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateAccessTokenHashes(); err != nil {
		t.Fatal(err)
	}
}

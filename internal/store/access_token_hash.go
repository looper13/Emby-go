package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
)

const accessTokenHashMarker = "access_tokens:sha256:v1"

func accessTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Hash persistent session credentials without changing the client token, owner,
// creation time or revocation rules. A transaction makes upgrades retryable.
func (s *Store) migrateAccessTokenHashes() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ready string
	err = tx.QueryRow("SELECT value FROM kv WHERE key=?", accessTokenHashMarker).Scan(&ready)
	if err == nil && ready == "1" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	rows, err := tx.Query("SELECT token FROM access_tokens")
	if err != nil {
		return err
	}
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, token)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if _, err := tx.Exec("UPDATE access_tokens SET token=? WHERE token=?", accessTokenDigest(token), token); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO kv(key,value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value='1'", accessTokenHashMarker); err != nil {
		return err
	}
	return tx.Commit()
}

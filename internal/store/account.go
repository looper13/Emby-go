package store

import (
	"errors"
	"strconv"
)

var ErrInvalidLibrary = errors.New("invalid library")
var ErrInvalidPassword = errors.New("invalid current password")

type LibraryAccessPolicy struct {
	EnableAllFolders bool
	LibraryIDs       []int64
}

// These views are the single account's browsing boundary, not the scanner's
// source of truth. Management and background jobs keep using the base tables.
func (s *Store) migrateAccount() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS account_policy (
 id INTEGER PRIMARY KEY CHECK(id=1), enable_all_folders INTEGER NOT NULL DEFAULT 1
);
INSERT OR IGNORE INTO account_policy(id) VALUES(1);
CREATE TABLE IF NOT EXISTS account_libraries (
 library_id INTEGER PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE
);
CREATE VIEW IF NOT EXISTS user_libraries AS
 SELECT * FROM libraries WHERE enabled=1 AND
 ((SELECT enable_all_folders FROM account_policy WHERE id=1)=1
 OR id IN (SELECT library_id FROM account_libraries));
CREATE VIEW IF NOT EXISTS user_movies AS
 SELECT * FROM movies WHERE library_id IN (SELECT id FROM user_libraries);
`)
	return err
}

func (s *Store) AccessVersion() string {
	return s.movieEpoch + ":user:" + s.userKey() + ":" + strconv.FormatUint(s.accessRevision.Load(), 10)
}

func (s *Store) LibraryAccessPolicy() (LibraryAccessPolicy, error) {
	p := LibraryAccessPolicy{LibraryIDs: []int64{}}
	if err := s.db.QueryRow("SELECT enable_all_folders FROM user_policy WHERE user_id=?", s.UserID()).Scan(&p.EnableAllFolders); err != nil {
		return p, err
	}
	rows, err := s.db.Query("SELECT library_id FROM user_library_grants WHERE user_id=? ORDER BY library_id", s.UserID())
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return p, err
		}
		p.LibraryIDs = append(p.LibraryIDs, id)
	}
	return p, rows.Err()
}

func (s *Store) SaveLibraryAccessPolicy(p LibraryAccessPolicy) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO user_policy(user_id,enable_all_folders) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET enable_all_folders=excluded.enable_all_folders", s.UserID(), p.EnableAllFolders); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM user_library_grants WHERE user_id=?", s.UserID()); err != nil {
		return err
	}
	for _, id := range p.LibraryIDs {
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM libraries WHERE id=? AND enabled=1)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalidLibrary
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO user_library_grants(user_id,library_id) VALUES(?,?)", s.UserID(), id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.accessRevision.Add(1)
	return nil
}

func (s *Store) UserLibraries() ([]Library, error) {
	return s.listLibraries("WHERE id IN (SELECT id FROM " + s.userLibrariesSQL() + ")")
}

func (s *Store) CanAccessLibrary(id int64) bool {
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM "+s.userLibrariesSQL()+" WHERE id=?)", id).Scan(&exists)
	return err == nil && exists
}

func (s *Store) CanAccessMovie(id int64) bool {
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM "+s.userMoviesSQL()+" WHERE id=?)", id).Scan(&exists)
	return err == nil && exists
}

func (s *Store) DeleteAccessToken(token string) error {
	_, err := s.db.Exec("DELETE FROM access_tokens WHERE token=?", accessTokenDigest(token))
	return err
}

// Changing either credential invalidates every password-login session in the
// same transaction. API keys are independent service credentials.
func (s *Store) UpdateAdministrator(username, currentPassword, newPassword string) error {
	return s.changeCredentials(username, currentPassword, newPassword)
}

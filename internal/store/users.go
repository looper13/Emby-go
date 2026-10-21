package store

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrUserConflict = errors.New("username already exists")
var ErrProtectedUser = errors.New("cannot remove or disable the original administrator")

type User struct {
	ID            int64
	Name          string
	Administrator bool
	Disabled      bool
}

// A request owns only its identity. Connection pools, locks and revision maps
// remain shared; never change the identity on the application's base store.
func (s *Store) ForUser(id int64) *Store { return &Store{storeState: s.storeState, userID: id} }
func (s *Store) UserID() int64 {
	if s.userID <= 0 {
		return 1
	}
	return s.userID
}
func (s *Store) userKey() string { return strconv.FormatInt(s.UserID(), 10) }
func (s *Store) userLibrariesSQL() string {
	return `(SELECT * FROM libraries WHERE enabled=1 AND ((SELECT enable_all_folders FROM user_policy WHERE user_id=` + s.userKey() + `)=1 OR id IN (SELECT library_id FROM user_library_grants WHERE user_id=` + s.userKey() + `)))`
}
func (s *Store) userMoviesSQL() string {
	return `(SELECT * FROM movies WHERE library_id IN (SELECT id FROM ` + s.userLibrariesSQL() + `))`
}
func (s *Store) userDataSQL() string {
	return `(SELECT * FROM userdata WHERE user_id=` + s.userKey() + `)`
}

func (s *Store) migrateUsers() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var migrated bool
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='users')").Scan(&migrated); err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY AUTOINCREMENT,username TEXT NOT NULL COLLATE NOCASE UNIQUE,password_hash TEXT NOT NULL,is_administrator INTEGER NOT NULL DEFAULT 0,is_disabled INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS user_policy(user_id INTEGER PRIMARY KEY,enable_all_folders INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS user_library_grants(user_id INTEGER NOT NULL,library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,PRIMARY KEY(user_id,library_id));`)
	if err != nil {
		return err
	}
	// Table creation and legacy import commit together. Existing multi-user
	// installations must never reapply obsolete single-account grants.
	if !migrated {
		_, err = tx.Exec(`INSERT INTO users(id,username,password_hash,is_administrator,created_at) SELECT id,username,password_hash,1,created_at FROM administrators;
INSERT INTO user_policy(user_id,enable_all_folders) SELECT 1,enable_all_folders FROM account_policy WHERE id=1;
INSERT INTO user_library_grants(user_id,library_id) SELECT 1,library_id FROM account_libraries;`)
		if err != nil {
			return err
		}
	}
	columns := func(table string) (map[string]bool, error) {
		rows, e := tx.Query("PRAGMA table_info(" + table + ")")
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if e = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); e != nil {
				return nil, e
			}
			out[name] = true
		}
		return out, rows.Err()
	}
	c, err := columns("access_tokens")
	if err != nil {
		return err
	}
	if !c["user_id"] {
		if _, err = tx.Exec("ALTER TABLE access_tokens ADD COLUMN user_id INTEGER NOT NULL DEFAULT 1"); err != nil {
			return err
		}
	}
	c, err = columns("userdata")
	if err != nil {
		return err
	}
	if !c["user_id"] {
		_, err = tx.Exec(`ALTER TABLE userdata RENAME TO userdata_legacy;
CREATE TABLE userdata(user_id INTEGER NOT NULL DEFAULT 1,movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,position_ticks INTEGER DEFAULT 0,play_count INTEGER DEFAULT 0,played INTEGER DEFAULT 0,last_played_at TEXT,last_stopped_ticks INTEGER DEFAULT -1,is_favorite INTEGER NOT NULL DEFAULT 0,likes INTEGER NOT NULL DEFAULT 0,hide_from_resume INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(user_id,movie_id));
INSERT INTO userdata SELECT 1,movie_id,position_ticks,play_count,played,last_played_at,last_stopped_ticks,is_favorite,likes,hide_from_resume FROM userdata_legacy;
DROP TABLE userdata_legacy;`)
		if err != nil {
			return err
		}
	}
	c, err = columns("item_favorites")
	if err != nil {
		return err
	}
	if !c["user_id"] {
		_, err = tx.Exec(`ALTER TABLE item_favorites RENAME TO item_favorites_legacy;
CREATE TABLE item_favorites(user_id INTEGER NOT NULL DEFAULT 1,item_id TEXT NOT NULL,PRIMARY KEY(user_id,item_id));
INSERT INTO item_favorites SELECT 1,item_id FROM item_favorites_legacy;
DROP TABLE item_favorites_legacy;`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Administrator, &u.Disabled)
	return u, err
}
func (s *Store) User(id int64) (User, error) {
	return scanUser(s.db.QueryRow("SELECT id,username,is_administrator,is_disabled FROM users WHERE id=?", id))
}
func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Query("SELECT id,username,is_administrator,is_disabled FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) AuthenticateUser(name, password string) (User, error) {
	var u User
	var hash string
	err := s.db.QueryRow("SELECT id,username,is_administrator,is_disabled,password_hash FROM users WHERE username=?", strings.TrimSpace(name)).Scan(&u.ID, &u.Name, &u.Administrator, &u.Disabled, &hash)
	if err != nil {
		return u, err
	}
	if u.Disabled || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, ErrInvalidPassword
	}
	return u, nil
}
func (s *Store) TokenUser(token string) (User, error) {
	return scanUser(s.db.QueryRow("SELECT u.id,u.username,u.is_administrator,u.is_disabled FROM users u JOIN access_tokens t ON t.user_id=u.id WHERE t.token=? AND u.is_disabled=0", token))
}
func (s *Store) SaveUserToken(token string, id int64) error {
	_, err := s.db.Exec("INSERT INTO access_tokens(token,created_at,user_id) VALUES(?,?,?)", token, time.Now().UTC().Format(time.RFC3339), id)
	return err
}
func (s *Store) CreateUser(name, password string) (User, error) {
	hash, err := passwordHash(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec("INSERT INTO users(username,password_hash,created_at) VALUES(?,?,?)", strings.TrimSpace(name), hash, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return User{}, ErrUserConflict
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec("INSERT INTO user_policy(user_id) VALUES(?)", id); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(id)
}
func (s *Store) EditUser(id int64, name, password string, disabled bool) error {
	if id == 1 && disabled {
		return ErrProtectedUser
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRow("SELECT password_hash FROM users WHERE id=?", id).Scan(&hash); err != nil {
		return err
	}
	if password != "" {
		if hash, err = passwordHash(password); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE users SET username=?,password_hash=?,is_disabled=? WHERE id=?", strings.TrimSpace(name), hash, disabled, id); err != nil {
		return ErrUserConflict
	}
	if _, err = tx.Exec("DELETE FROM access_tokens WHERE user_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteUser(id int64) error {
	if id == 1 {
		return ErrProtectedUser
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int64
	if err = tx.QueryRow("SELECT id FROM users WHERE id=?", id).Scan(&found); err != nil {
		return err
	}
	for _, table := range []string{"access_tokens", "userdata", "item_favorites", "user_policy", "user_library_grants"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE user_id=?", id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM users WHERE id=?", id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.accessRevision.Add(1)
	return nil
}

func (s *Store) changeCredentials(name, current, password string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRow("SELECT password_hash FROM users WHERE id=?", s.UserID()).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return ErrInvalidPassword
	}
	if password != "" {
		if hash, err = passwordHash(password); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE users SET username=?,password_hash=? WHERE id=?", strings.TrimSpace(name), hash, s.UserID()); err != nil {
		return ErrUserConflict
	}
	if _, err = tx.Exec("DELETE FROM access_tokens WHERE user_id=?", s.UserID()); err != nil {
		return err
	}
	return tx.Commit()
}

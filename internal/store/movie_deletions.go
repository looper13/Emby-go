package store

import (
	"database/sql"
	"errors"
	"strings"
)

func NormalizeDuplicateNumber(number string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("-_. \t\r\n", r) {
			return -1
		}
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, number)
}

func (s *Store) MovieDeletionState(id int64) (string, error) {
	var state string
	err := s.db.QueryRow("SELECT state FROM movie_deletions WHERE movie_id=?", id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

func (s *Store) SaveMovieDeletionState(id int64, state string) error {
	_, err := s.db.Exec("INSERT INTO movie_deletions(movie_id,state) VALUES(?,?) ON CONFLICT(movie_id) DO UPDATE SET state=excluded.state", id, state)
	return err
}

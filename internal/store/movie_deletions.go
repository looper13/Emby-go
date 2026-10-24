package store

import (
	"context"
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

// Check all indexed references, including other numbers and disabled libraries.
// Read only source columns; never load full movie metadata for this guard.
func (s *Store) VisitOtherMovieSources(ctx context.Context, id int64, visit func(int64, string) error) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id,source_path,additional_parts FROM movies WHERE id<>?", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var otherID int64
		var source, parts string
		if err := rows.Scan(&otherID, &source, &parts); err != nil {
			return err
		}
		for _, file := range append([]string{source}, parseStrings(parts)...) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(otherID, file); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

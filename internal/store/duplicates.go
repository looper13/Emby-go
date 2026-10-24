package store

import (
	"context"
	"database/sql"
	"strings"
)

type DuplicateGroup struct {
	Key    string `json:"key"`
	Number string `json:"number"`
	Count  int    `json:"count"`
}

type DuplicateGroups struct {
	Items       []DuplicateGroup `json:"items"`
	Total       int              `json:"total"`
	TotalMovies int              `json:"total_movies"`
	ExtraCopies int              `json:"extra_copies"`
}

type DuplicateMovie struct {
	ID              int64    `json:"id"`
	LibraryID       int64    `json:"library_id"`
	LibraryName     string   `json:"library_name"`
	Number          string   `json:"number"`
	Title           string   `json:"title"`
	SourcePath      string   `json:"source_path"`
	Status          string   `json:"status"`
	AdditionalParts []string `json:"additional_parts"`
}

type DuplicateMembers struct {
	Items []DuplicateMovie `json:"items"`
	Total int              `json:"total"`
}

// Compare only the indexed number, never guessed title/path fragments. Preserve
// digits (including leading zeroes) and version suffixes to avoid false matches.
const duplicateNumberSQL = `UPPER(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(COALESCE(number,''),'-',''),'_',''),'.',''),' ',''),CHAR(9),''),CHAR(10),''),CHAR(13),''))`

func (s *Store) duplicateScope(libraryID int64) (string, []any) {
	where := `television='null' AND library_id IN (SELECT id FROM ` + s.userLibrariesSQL() + `)`
	args := []any{}
	if libraryID > 0 {
		where += " AND library_id=?"
		args = append(args, libraryID)
	}
	return where, args
}

// DuplicateMovieGroups aggregates in SQLite and returns a bounded page, without
// loading all movies, touching files, or persisting a potentially stale report.
func (s *Store) DuplicateMovieGroups(ctx context.Context, libraryID int64, limit, offset int) (DuplicateGroups, error) {
	out := DuplicateGroups{Items: []DuplicateGroup{}}
	where, args := s.duplicateScope(libraryID)
	cte := `WITH numbered AS (SELECT ` + duplicateNumberSQL + ` AS number_key,UPPER(TRIM(COALESCE(number,''))) AS display_number FROM movies WHERE ` + where + `), groups AS (SELECT number_key,MIN(display_number) AS display_number,COUNT(*) AS copies FROM numbered WHERE number_key<>'' GROUP BY number_key HAVING COUNT(*)>1) `
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, cte+`SELECT COUNT(*),COALESCE(SUM(copies),0),COALESCE(SUM(copies-1),0) FROM groups`, args...).Scan(&out.Total, &out.TotalMovies, &out.ExtraCopies)
	if err != nil {
		return out, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := tx.QueryContext(ctx, cte+`SELECT number_key,display_number,copies FROM groups ORDER BY number_key LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var group DuplicateGroup
		if err = rows.Scan(&group.Key, &group.Number, &group.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, group)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// Members are paged independently so one number with many copies cannot cause
// an unbounded response. CD parts belong to their existing single movie row.
func (s *Store) DuplicateMovieMembers(ctx context.Context, libraryID int64, key string, limit, offset int) (DuplicateMembers, error) {
	out := DuplicateMembers{Items: []DuplicateMovie{}}
	if strings.TrimSpace(key) == "" {
		return out, nil
	}
	where, args := s.duplicateScope(libraryID)
	where += ` AND ` + duplicateNumberSQL + `=?`
	args = append(args, key)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies WHERE `+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := tx.QueryContext(ctx, `SELECT id,library_id,(SELECT name FROM libraries WHERE libraries.id=movies.library_id),COALESCE(number,''),COALESCE(title,''),source_path,status,additional_parts FROM movies WHERE `+where+` ORDER BY library_id,source_path,id LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var movie DuplicateMovie
		var parts string
		if err = rows.Scan(&movie.ID, &movie.LibraryID, &movie.LibraryName, &movie.Number, &movie.Title, &movie.SourcePath, &movie.Status, &parts); err != nil {
			rows.Close()
			return out, err
		}
		movie.AdditionalParts = parseStrings(parts)
		if movie.AdditionalParts == nil {
			movie.AdditionalParts = []string{}
		}
		out.Items = append(out.Items, movie)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

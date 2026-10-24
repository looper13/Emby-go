package store

import (
	"context"
	"errors"
)

var ErrDuplicateRule = errors.New("invalid duplicate selection rule")

type DuplicateSelectionMovie struct {
	DuplicateMovie
	CreatedAt   string `json:"created_at"`
	PathLength  int    `json:"path_length"`
	LibraryPath string `json:"-"`
}

type DuplicateSelectionItem struct {
	Key   string                  `json:"key"`
	Movie DuplicateSelectionMovie `json:"movie"`
	Keep  DuplicateSelectionMovie `json:"keep"`
}

// Select every surplus copy in one SQL snapshot, independent of browse pages.
// Only indexed metadata is read; no NFO, STRM, or remote file is opened here.
func (s *Store) DuplicateMovieSelection(ctx context.Context, libraryID int64, rule string) ([]DuplicateSelectionItem, error) {
	var order string
	switch rule {
	case "oldest", "newest":
		direction := "ASC"
		if rule == "newest" {
			direction = "DESC"
		}
		order = "(julianday(created_at) IS NULL) ASC,julianday(created_at) " + direction + ",id ASC"
	case "shortest":
		order = "length(source_path) ASC,id ASC"
	case "longest":
		order = "length(source_path) DESC,id ASC"
	default:
		return nil, ErrDuplicateRule
	}
	where, args := s.duplicateScope(libraryID)
	query := `WITH scoped AS (SELECT id,library_id,source_path,COALESCE(number,'') AS number,COALESCE(title,'') AS title,status,additional_parts,COALESCE(created_at,'') AS created_at,` + duplicateNumberSQL + ` AS number_key FROM movies WHERE ` + where + `),
		ranked AS (SELECT *,ROW_NUMBER() OVER (PARTITION BY number_key ORDER BY ` + order + `) AS position FROM scoped WHERE number_key<>''),
		keepers AS (SELECT * FROM ranked WHERE position=1)
		SELECT m.number_key,m.id,m.library_id,ml.name,m.number,m.title,m.source_path,m.status,m.additional_parts,m.created_at,length(m.source_path),ml.path,
		k.id,k.library_id,kl.name,k.number,k.title,k.source_path,k.status,k.additional_parts,k.created_at,length(k.source_path),kl.path
		FROM ranked m JOIN keepers k ON k.number_key=m.number_key JOIN libraries ml ON ml.id=m.library_id JOIN libraries kl ON kl.id=k.library_id
		WHERE m.position>1 ORDER BY m.number_key,m.position`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DuplicateSelectionItem{}
	for rows.Next() {
		var item DuplicateSelectionItem
		var movieParts, keepParts string
		m, k := &item.Movie, &item.Keep
		if err := rows.Scan(&item.Key, &m.ID, &m.LibraryID, &m.LibraryName, &m.Number, &m.Title, &m.SourcePath, &m.Status, &movieParts, &m.CreatedAt, &m.PathLength, &m.LibraryPath,
			&k.ID, &k.LibraryID, &k.LibraryName, &k.Number, &k.Title, &k.SourcePath, &k.Status, &keepParts, &k.CreatedAt, &k.PathLength, &k.LibraryPath); err != nil {
			return nil, err
		}
		m.AdditionalParts, k.AdditionalParts = parseStrings(movieParts), parseStrings(keepParts)
		if m.AdditionalParts == nil {
			m.AdditionalParts = []string{}
		}
		if k.AdditionalParts == nil {
			k.AdditionalParts = []string{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

package store

import (
	"database/sql"
	"time"
)

type JavdbListFavorite struct {
	LibraryID   int64  `json:"library_id"`
	MovieID     int64  `json:"movie_id"`
	ListID      string `json:"list_id"`
	RelatedPage int    `json:"related_page"`
	Name        string `json:"name"`
	MovieCount  *int   `json:"movie_count,omitempty"`
}

func (s *Store) JavdbListFavorites(search string, offset, limit int) ([]JavdbListFavorite, int, error) {
	var total int
	if err := s.db.QueryRow("SELECT count(*) FROM javdb_list_favorites WHERE user_id=? AND name LIKE ?", s.UserID(), "%"+search+"%").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT library_id,movie_id,list_id,related_page,name,movie_count
FROM javdb_list_favorites WHERE user_id=? AND name LIKE ?
ORDER BY name COLLATE NOCASE,library_id,list_id LIMIT ? OFFSET ?`, s.UserID(), "%"+search+"%", limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []JavdbListFavorite{}
	for rows.Next() {
		var item JavdbListFavorite
		var movieCount sql.NullInt64
		if err := rows.Scan(&item.LibraryID, &item.MovieID, &item.ListID, &item.RelatedPage, &item.Name, &movieCount); err != nil {
			return nil, 0, err
		}
		if movieCount.Valid {
			count := int(movieCount.Int64)
			item.MovieCount = &count
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) IsJavdbListFavorite(libraryID int64, listID string) bool {
	var exists bool
	_ = s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM javdb_list_favorites WHERE user_id=? AND library_id=? AND list_id=?)", s.UserID(), libraryID, listID).Scan(&exists)
	return exists
}

func (s *Store) SaveJavdbListFavorite(item JavdbListFavorite) error {
	var movieCount any
	if item.MovieCount != nil {
		movieCount = *item.MovieCount
	}
	_, err := s.db.Exec(`INSERT INTO javdb_list_favorites(user_id,library_id,movie_id,list_id,related_page,name,movie_count,updated_at)
VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,library_id,list_id) DO UPDATE SET
movie_id=excluded.movie_id,related_page=excluded.related_page,name=excluded.name,movie_count=excluded.movie_count,updated_at=excluded.updated_at`,
		s.UserID(), item.LibraryID, item.MovieID, item.ListID, item.RelatedPage, item.Name, movieCount, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) DeleteJavdbListFavorite(libraryID int64, listID string) error {
	_, err := s.db.Exec("DELETE FROM javdb_list_favorites WHERE user_id=? AND library_id=? AND list_id=?", s.UserID(), libraryID, listID)
	return err
}

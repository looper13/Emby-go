package store

import (
	"database/sql"
	"emby-go/internal/anime"
)

// Filter and count original media in SQL before merging the online projection.
// A JSON parameter keeps this bounded by one parameter even for large libraries.
func (s *Store) SearchWithoutAnime(exclude []int64, term, years, genre, tags, studios, person string, unplayed, favorite bool, sortBy string, desc bool, limit int, rating string) ([]Movie, int, error) {
	return s.searchExcluding(exclude, 0, term, "", years, genre, tags, studios, person, "", "", "", unplayed, favorite, sortBy, desc, limit, 0, rating)
}

// ResolveAnimeDisplay is an indexed lookup by the original media identity.
func (s *Store) ResolveAnimeDisplay(movieID int64) (string, error) {
	var subjectID, episodeID int64
	err := s.db.QueryRow(`SELECT subject_id,COALESCE(episode_id,0) FROM anime_media_bindings WHERE movie_id=? AND status='playable'`, movieID).Scan(&subjectID, &episodeID)
	if err != nil {
		return "", err
	}
	if episodeID > 0 {
		return anime.ID("episode", episodeID), nil
	}
	return anime.ID("movie", subjectID), nil
}

type AnimeProjection struct {
	SubjectID int64
	EpisodeID int64
	MovieID   int64
	Season    int
	Index     int
	Played    bool
	Position  int64
	PlayCount int64
	Hidden    bool
	Name      string
}

func (p AnimeProjection) ItemID() string {
	if p.EpisodeID > 0 {
		return anime.ID("episode", p.EpisodeID)
	}
	return anime.ID("movie", p.SubjectID)
}
func (s *Store) AnimeProjection() ([]AnimeProjection, error) {
	rows, err := s.db.Query(`SELECT b.subject_id,COALESCE(b.episode_id,0),b.movie_id,COALESCE(e.season,1),COALESCE(e.emby_index,1),COALESCE(u.played,0),COALESCE(u.position_ticks,0),COALESCE(u.play_count,0),COALESCE(u.hide_from_resume,0),s.name FROM anime_media_bindings b
JOIN anime_subjects s ON s.id=b.subject_id JOIN movies m ON m.id=b.movie_id JOIN libraries l ON l.id=m.library_id
LEFT JOIN anime_episodes e ON e.id=b.episode_id LEFT JOIN userdata u ON u.movie_id=m.id
WHERE m.status IN ('success','manual') AND l.enabled=1 AND l.source_type='nfo_directory'
ORDER BY s.name,b.subject_id,COALESCE(e.season,1) DESC,COALESCE(CAST(e.sort AS REAL),1),e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AnimeProjection{}
	for rows.Next() {
		var p AnimeProjection
		if err = rows.Scan(&p.SubjectID, &p.EpisodeID, &p.MovieID, &p.Season, &p.Index, &p.Played, &p.Position, &p.PlayCount, &p.Hidden, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AnimeBindingOccupied(movieID int64) (bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM anime_media_bindings WHERE movie_id=?`, movieID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) RecoverAnimeSync() error {
	_, err := s.db.Exec(`UPDATE anime_sync_state SET status='interrupted',error='服务重启中断了上次同步' WHERE status='running'`)
	return err
}

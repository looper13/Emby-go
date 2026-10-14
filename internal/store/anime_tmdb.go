package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"emby-go/internal/anime"
)

type AnimeTMDBConfig struct {
	APIKey     string `json:"-"`
	Revision   int64  `json:"revision"`
	Configured bool   `json:"configured"`
}
type AnimeTMDBSeries struct {
	SubjectID int64  `json:"subject_id"`
	SeriesID  int64  `json:"series_id"`
	Name      string `json:"name"`
	Revision  int64  `json:"revision"`
}
type AnimeTMDBEpisode struct {
	EpisodeID int64    `json:"episode_id"`
	SubjectID int64    `json:"subject_id"`
	SeriesID  int64    `json:"series_id"`
	Season    int      `json:"season"`
	Number    int      `json:"number"`
	TMDBID    int64    `json:"tmdb_id"`
	Name      string   `json:"name"`
	Stills    []string `json:"stills"`
	Revision  int64    `json:"revision"`
}

func (s *Store) migrateAnimeTMDB() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS anime_tmdb_config(id INTEGER PRIMARY KEY CHECK(id=1),api_key TEXT NOT NULL,revision INTEGER NOT NULL);
	INSERT OR IGNORE INTO anime_tmdb_config VALUES(1,'',1);
	CREATE TABLE IF NOT EXISTS anime_tmdb_series(subject_id INTEGER PRIMARY KEY REFERENCES anime_subjects(id),series_id INTEGER NOT NULL,name TEXT NOT NULL,revision INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS anime_tmdb_episodes(episode_id INTEGER PRIMARY KEY REFERENCES anime_episodes(id),subject_id INTEGER NOT NULL REFERENCES anime_subjects(id),series_id INTEGER NOT NULL,season INTEGER NOT NULL,number INTEGER NOT NULL,metadata TEXT NOT NULL,revision INTEGER NOT NULL);
	CREATE UNIQUE INDEX IF NOT EXISTS anime_tmdb_unique_episode ON anime_tmdb_episodes(subject_id,series_id,season,number) WHERE series_id>0;`)
	return err
}
func (s *Store) AnimeTMDBConfig() (AnimeTMDBConfig, error) {
	var v AnimeTMDBConfig
	err := s.db.QueryRow("SELECT api_key,revision FROM anime_tmdb_config WHERE id=1").Scan(&v.APIKey, &v.Revision)
	v.Configured = v.APIKey != ""
	return v, err
}
func (s *Store) SaveAnimeTMDBConfig(apiKey string, clear bool, revision int64) error {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) > 256 || strings.ContainsAny(apiKey, "\r\n\t ") || clear && apiKey != "" {
		return errors.New("invalid_tmdb_api_key")
	}
	if clear {
		return affectedOne(s.db.Exec("UPDATE anime_tmdb_config SET api_key='',revision=revision+1 WHERE id=1 AND revision=?", revision))
	}
	if apiKey == "" {
		return affectedOne(s.db.Exec("UPDATE anime_tmdb_config SET revision=revision+1 WHERE id=1 AND revision=?", revision))
	}
	return affectedOne(s.db.Exec("UPDATE anime_tmdb_config SET api_key=?,revision=revision+1 WHERE id=1 AND revision=?", apiKey, revision))
}

// A deliberate clear or replacement survives restart; defaults seed only a new configuration.
func (s *Store) InitializeAnimeTMDB(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) > 256 || strings.ContainsAny(apiKey, "\r\n\t ") {
		return errors.New("invalid_tmdb_api_key")
	}
	_, err := s.db.Exec("UPDATE anime_tmdb_config SET api_key=? WHERE id=1 AND revision=1 AND api_key=''", apiKey)
	return err
}
func (s *Store) AnimeTMDBSeries(subject int64) (AnimeTMDBSeries, error) {
	v := AnimeTMDBSeries{SubjectID: subject}
	err := s.db.QueryRow("SELECT series_id,name,revision FROM anime_tmdb_series WHERE subject_id=?", subject).Scan(&v.SeriesID, &v.Name, &v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return v, err
}
func (s *Store) SaveAnimeTMDBSeries(ctx context.Context, v AnimeTMDBSeries, configRevision int64) error {
	tx, err := s.beginAnimeWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, rev int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM anime_tmdb_config WHERE id=1").Scan(&current); err != nil {
		return err
	}
	if current != configRevision {
		return ErrConflict
	}
	err = tx.QueryRowContext(ctx, "SELECT revision FROM anime_tmdb_series WHERE subject_id=?", v.SubjectID).Scan(&rev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if rev != v.Revision {
		return ErrConflict
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM anime_tmdb_episodes WHERE subject_id=? AND series_id>0 AND series_id!=?", v.SubjectID, v.SeriesID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO anime_tmdb_series VALUES(?,?,?,?) ON CONFLICT(subject_id) DO UPDATE SET series_id=excluded.series_id,name=excluded.name,revision=excluded.revision`, v.SubjectID, v.SeriesID, v.Name, v.Revision+1)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) AnimeTMDBEpisode(id int64) (AnimeTMDBEpisode, error) {
	v := AnimeTMDBEpisode{EpisodeID: id, Stills: []string{}}
	var raw string
	err := s.db.QueryRow("SELECT metadata,revision FROM anime_tmdb_episodes WHERE episode_id=?", id).Scan(&raw, &v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (s *Store) AnimeTMDBEpisodes(subject int64) (map[int64]AnimeTMDBEpisode, error) {
	rows, err := s.db.Query("SELECT metadata FROM anime_tmdb_episodes WHERE subject_id=?", subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]AnimeTMDBEpisode{}
	for rows.Next() {
		var raw string
		var v AnimeTMDBEpisode
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out[v.EpisodeID] = v
	}
	return out, rows.Err()
}
func (s *Store) SaveAnimeTMDBEpisode(ctx context.Context, v AnimeTMDBEpisode, seriesRevision, configRevision int64) error {
	if v.SeriesID < 0 || v.Season < 0 || v.Number < 0 || len(v.Stills) > 20 {
		return errors.New("invalid_tmdb_mapping")
	}
	for _, p := range v.Stills {
		if anime.TMDBImageURL(p) == "" {
			return errors.New("invalid_tmdb_still")
		}
	}
	tx, err := s.beginAnimeWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cfg, series, rev int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM anime_tmdb_config WHERE id=1").Scan(&cfg); err != nil {
		return err
	}
	if cfg != configRevision {
		return ErrConflict
	}
	if err = tx.QueryRowContext(ctx, "SELECT series_id,revision FROM anime_tmdb_series WHERE subject_id=?", v.SubjectID).Scan(&series, &rev); err != nil {
		return err
	}
	if rev != seriesRevision || v.SeriesID > 0 && series != v.SeriesID {
		return ErrConflict
	}
	var owner int64
	if err = tx.QueryRowContext(ctx, "SELECT subject_id FROM anime_episodes WHERE id=?", v.EpisodeID).Scan(&owner); err != nil {
		return err
	}
	if owner != v.SubjectID {
		return ErrConflict
	}
	rev = 0
	err = tx.QueryRowContext(ctx, "SELECT revision FROM anime_tmdb_episodes WHERE episode_id=?", v.EpisodeID).Scan(&rev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if rev != v.Revision {
		return ErrConflict
	}
	if v.SeriesID > 0 {
		var n int
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM anime_tmdb_episodes WHERE subject_id=? AND series_id=? AND season=? AND number=? AND episode_id!=?", v.SubjectID, v.SeriesID, v.Season, v.Number, v.EpisodeID).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict
		}
	}
	v.Revision++
	_, err = tx.ExecContext(ctx, `INSERT INTO anime_tmdb_episodes VALUES(?,?,?,?,?,?,?) ON CONFLICT(episode_id) DO UPDATE SET subject_id=excluded.subject_id,series_id=excluded.series_id,season=excluded.season,number=excluded.number,metadata=excluded.metadata,revision=excluded.revision`, v.EpisodeID, v.SubjectID, v.SeriesID, v.Season, v.Number, animeJSON(v), v.Revision)
	if err != nil {
		return err
	}
	return tx.Commit()
}

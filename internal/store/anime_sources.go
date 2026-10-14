package store

import (
	"context"
	"database/sql"

	"emby-go/internal/anime"
)

func (s *Store) migrateAnimeSources() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS anime_subject_sources(subject_id INTEGER PRIMARY KEY REFERENCES anime_subjects(id),archive_digest TEXT NOT NULL DEFAULT '',api_updated_at TEXT NOT NULL DEFAULT '');`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AnimeSubjectByBangumiID(id int64) (anime.Subject, error) {
	var sid int64
	if err := s.db.QueryRow(`SELECT id FROM anime_subjects WHERE bangumi_id=?`, id).Scan(&sid); err != nil {
		return anime.Subject{}, err
	}
	return s.AnimeSubject(sid)
}

// Import only the viewed subject into the stable playback model. An older
// archive must not overwrite metadata fetched more recently through the API.
func (s *Store) ImportAnimeSubject(ctx context.Context, sub anime.Subject, eps []anime.Episode, digest, publishedAt string) (int64, error) {
	return s.saveAnimeSubject(ctx, sub, eps, digest, publishedAt)
}

func (s *Store) AnimeSubjectEntries(id int64) ([]anime.Entry, error) {
	rows, err := s.db.Query(`SELECT `+animeEntryColumns+` FROM anime_mikan_entries e WHERE e.subject_id=? ORDER BY e.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []anime.Entry{}
	for rows.Next() {
		e, err := readAnimeEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AnimeArchiveSchedule() error {
	tx, err := s.beginAnimeWrite(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var value string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key='anime.archive_schedule_initialized'`).Scan(&value)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = tx.Exec(`INSERT INTO scheduled_tasks(name,type,cron,params,enabled,created_at,updated_at) VALUES('Bangumi 目录更新','anime_archive','30 5 * * 3','{}',1,?,?);`, animeNow(), animeNow())
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES('anime.archive_schedule_initialized','1')`); err != nil {
		return err
	}
	return tx.Commit()
}

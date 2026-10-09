package store

import (
	"database/sql"
	"strings"
	"time"
)

type ScannedMovie struct {
	Movie       Movie
	Size        int64
	ModTime     time.Time
	Actors      []ActorRef
	Fingerprint string
}

func (database *Store) SaveScannedMovies(movies []ScannedMovie) error {
	if len(movies) == 0 {
		return nil
	}
	transaction, err := database.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	upsert, err := transaction.Prepare(upsertMovieSQL)
	if err != nil {
		return err
	}
	defer upsert.Close()
	fingerprint, err := transaction.Prepare(`INSERT INTO scan_fingerprints(movie_id, fingerprint) VALUES(?,?)
ON CONFLICT(movie_id) DO UPDATE SET fingerprint=excluded.fingerprint`)
	if err != nil {
		return err
	}
	defer fingerprint.Close()
	actorUpsert, err := transaction.Prepare(`INSERT INTO actors(name,avatar_url,updated_at) VALUES(?,?,?)
 ON CONFLICT(name) DO UPDATE SET avatar_url=CASE WHEN excluded.avatar_url<>'' THEN excluded.avatar_url ELSE actors.avatar_url END, updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer actorUpsert.Close()
	actorInsert, err := transaction.Prepare("INSERT OR IGNORE INTO movie_actors(movie_id,actor_name) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer actorInsert.Close()
	actorDelete, err := transaction.Prepare("DELETE FROM movie_actors WHERE movie_id=?")
	if err != nil {
		return err
	}
	defer actorDelete.Close()
	actorSelect, err := transaction.Prepare("SELECT actor_name FROM movie_actors WHERE movie_id=?")
	if err != nil {
		return err
	}
	defer actorSelect.Close()
	ids := make([]int64, 0, len(movies))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, entry := range movies {
		var movieID int64
		if err := upsert.QueryRow(movieValues(entry.Movie, entry.Size, entry.ModTime, now)...).Scan(&movieID); err != nil {
			return err
		}
		ids = append(ids, movieID)
		if err := saveScanActors(movieID, entry.Actors, now, actorSelect, actorDelete, actorUpsert, actorInsert); err != nil {
			return err
		}
		if err := saveFeatureValuesTx(transaction, movieID, movieFeatureValues(entry.Movie, entry.Actors)); err != nil {
			return err
		}
		if entry.Fingerprint == "" {
			if _, err := transaction.Exec("DELETE FROM scan_fingerprints WHERE movie_id=?", movieID); err != nil {
				return err
			}
		} else if _, err := fingerprint.Exec(movieID, entry.Fingerprint); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	for index, id := range ids {
		database.movieLibraries.Store(id, movies[index].Movie.LibraryID)
		database.TouchMovie(id)
	}
	return nil
}

func saveScanActors(movieID int64, actors []ActorRef, now string, selectNames, deleteNames, upsert, insert *sql.Stmt) error {
	next := make(map[string]bool, len(actors))
	for _, actor := range actors {
		if name := strings.TrimSpace(actor.Name); name != "" {
			next[name] = true
		}
	}
	rows, err := selectNames.Query(movieID)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	same := len(next) == len(existing)
	for name := range next {
		if !existing[name] {
			same = false
			break
		}
	}
	if !same {
		if _, err := deleteNames.Exec(movieID); err != nil {
			return err
		}
	}
	for _, actor := range actors {
		name := strings.TrimSpace(actor.Name)
		if name == "" {
			continue
		}
		if _, err := upsert.Exec(name, strings.TrimSpace(actor.AvatarURL), now); err != nil {
			return err
		}
		if !same {
			if _, err := insert.Exec(movieID, name); err != nil {
				return err
			}
		}
	}
	return nil
}

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
	_, err := database.SaveScannedMoviesWithStats(movies)
	return err
}

// ScanWriteStats measures wall time, including any connection/lock waits inside
// database calls. Begin and the first upsert cannot identify lock waits alone.
type ScanWriteStats struct {
	Total, Begin, Prepare, Movie, Actors, Features, Fingerprint, Commit time.Duration
}

func (database *Store) SaveScannedMoviesWithStats(movies []ScannedMovie) (stats ScanWriteStats, err error) {
	if len(movies) == 0 {
		return stats, nil
	}
	checked := map[int64]bool{}
	for _, entry := range movies {
		if !checked[entry.Movie.LibraryID] {
			if err := database.RequireDirectory(entry.Movie.LibraryID); err != nil {
				return stats, err
			}
			checked[entry.Movie.LibraryID] = true
		}
	}
	started := time.Now()
	defer func() { stats.Total = time.Since(started) }()
	stage := time.Now()
	transaction, err := database.db.Begin()
	stats.Begin = time.Since(stage)
	if err != nil {
		return stats, err
	}
	defer transaction.Rollback()
	stage = time.Now()
	prepareStarted := stage
	preparing := true
	// Record partially completed preparation even if a statement fails.
	defer func() {
		if preparing {
			stats.Prepare = time.Since(prepareStarted)
		}
	}()
	upsert, err := transaction.Prepare(upsertMovieSQL)
	if err != nil {
		return stats, err
	}
	defer upsert.Close()
	fingerprint, err := transaction.Prepare(`INSERT INTO scan_fingerprints(movie_id, fingerprint) VALUES(?,?)
ON CONFLICT(movie_id) DO UPDATE SET fingerprint=excluded.fingerprint`)
	if err != nil {
		return stats, err
	}
	defer fingerprint.Close()
	actorUpsert, err := transaction.Prepare(`INSERT INTO actors(name,avatar_url,updated_at) VALUES(?,?,?)
 ON CONFLICT(name) DO UPDATE SET avatar_url=CASE WHEN excluded.avatar_url<>'' THEN excluded.avatar_url ELSE actors.avatar_url END, updated_at=excluded.updated_at`)
	if err != nil {
		return stats, err
	}
	defer actorUpsert.Close()
	actorInsert, err := transaction.Prepare("INSERT OR IGNORE INTO movie_actors(movie_id,actor_name) VALUES(?,?)")
	if err != nil {
		return stats, err
	}
	defer actorInsert.Close()
	actorDelete, err := transaction.Prepare("DELETE FROM movie_actors WHERE movie_id=?")
	if err != nil {
		return stats, err
	}
	defer actorDelete.Close()
	actorSelect, err := transaction.Prepare("SELECT actor_name FROM movie_actors WHERE movie_id=?")
	if err != nil {
		return stats, err
	}
	defer actorSelect.Close()
	features, err := prepareFeatureStatements(transaction)
	if err != nil {
		return stats, err
	}
	defer features.close()
	stats.Prepare = time.Since(stage)
	preparing = false
	ids := make([]int64, 0, len(movies))
	now := time.Now().UTC().Format(time.RFC3339)
	stage = time.Now()
	// A shared actor may occur in every movie. Preserve the last nonempty NFO
	// avatar in batch order, then write that actor once before inserting links.
	actors := make(map[string]string)
	for _, entry := range movies {
		for _, actor := range entry.Actors {
			name, avatar := strings.TrimSpace(actor.Name), strings.TrimSpace(actor.AvatarURL)
			if name == "" {
				continue
			}
			if _, exists := actors[name]; !exists || avatar != "" {
				actors[name] = avatar
			}
		}
	}
	for name, avatar := range actors {
		if _, err = actorUpsert.Exec(name, avatar, now); err != nil {
			stats.Actors += time.Since(stage)
			return stats, err
		}
	}
	stats.Actors += time.Since(stage)
	for _, entry := range movies {
		var movieID int64
		stage = time.Now()
		err = upsert.QueryRow(movieValues(entry.Movie, entry.Size, entry.ModTime, now)...).Scan(&movieID)
		stats.Movie += time.Since(stage)
		if err != nil {
			return stats, err
		}
		ids = append(ids, movieID)
		stage = time.Now()
		err = saveScanActors(movieID, entry.Actors, actorSelect, actorDelete, actorInsert)
		stats.Actors += time.Since(stage)
		if err != nil {
			return stats, err
		}
		stage = time.Now()
		err = features.save(movieID, movieFeatureValues(entry.Movie, entry.Actors))
		stats.Features += time.Since(stage)
		if err != nil {
			return stats, err
		}
		stage = time.Now()
		if entry.Fingerprint == "" {
			_, err = transaction.Exec("DELETE FROM scan_fingerprints WHERE movie_id=?", movieID)
		} else {
			_, err = fingerprint.Exec(movieID, entry.Fingerprint)
		}
		stats.Fingerprint += time.Since(stage)
		if err != nil {
			return stats, err
		}
	}
	stage = time.Now()
	err = transaction.Commit()
	stats.Commit = time.Since(stage)
	if err != nil {
		return stats, err
	}
	for index, id := range ids {
		database.movieLibraries.Store(id, movies[index].Movie.LibraryID)
		database.TouchMovie(id)
	}
	return stats, nil
}

func saveScanActors(movieID int64, actors []ActorRef, selectNames, deleteNames, insert *sql.Stmt) error {
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
		for name := range next {
			if _, err := insert.Exec(movieID, name); err != nil {
				return err
			}
		}
	}
	return nil
}

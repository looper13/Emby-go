package store

import "time"

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
	now := time.Now().UTC().Format(time.RFC3339)
	for _, entry := range movies {
		var movieID int64
		if err := upsert.QueryRow(movieValues(entry.Movie, entry.Size, entry.ModTime, now)...).Scan(&movieID); err != nil {
			return err
		}
		if err := replaceActorsTx(transaction, movieID, entry.Actors); err != nil {
			return err
		}
		if err := refreshFeaturesTx(transaction, movieID); err != nil {
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
	return transaction.Commit()
}

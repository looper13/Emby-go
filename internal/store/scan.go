package store

import (
	"path/filepath"
	"strings"
)

func (s *Store) ScanFingerprints(libraryID int64) (map[string]string, error) {
	return s.scanFingerprints("movies.library_id=?", libraryID)
}

func (s *Store) DirectoryScanFingerprints(libraryID int64, directory string, recursive bool) (map[string]string, error) {
	if !recursive {
		return s.scanFingerprints("movies.library_id=? AND movies.output_dir=?", libraryID, directory)
	}
	prefix := directory
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return s.scanFingerprints("movies.library_id=? AND (movies.output_dir=? OR (movies.output_dir>=? AND movies.output_dir<?))", libraryID, directory, prefix, prefix+"\U0010ffff")
}

func (s *Store) scanFingerprints(condition string, args ...any) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT movies.source_path, COALESCE(scan_fingerprints.fingerprint, '')
FROM movies LEFT JOIN scan_fingerprints ON scan_fingerprints.movie_id=movies.id WHERE `+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fingerprints := make(map[string]string)
	for rows.Next() {
		var path, fingerprint string
		if err := rows.Scan(&path, &fingerprint); err != nil {
			return nil, err
		}
		fingerprints[path] = fingerprint
	}
	return fingerprints, rows.Err()
}

func (s *Store) DeleteScannedSources(libraryID int64, paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	transaction, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	deleted := 0
	for _, path := range paths {
		result, err := transaction.Exec("DELETE FROM movies WHERE library_id=? AND source_path=?", libraryID, path)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += int(count)
	}
	return deleted, transaction.Commit()
}

func (s *Store) SaveScanFingerprint(libraryID int64, path, fingerprint string) error {
	_, err := s.db.Exec(`INSERT INTO scan_fingerprints(movie_id, fingerprint)
SELECT id, ? FROM movies WHERE library_id=? AND source_path=?
ON CONFLICT(movie_id) DO UPDATE SET fingerprint=excluded.fingerprint`, fingerprint, libraryID, path)
	return err
}

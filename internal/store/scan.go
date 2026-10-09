package store

import (
	"database/sql"
	"path/filepath"
	"strings"
)

// ScannedSource keeps metadata freshness separate from source-file membership.
type ScannedSource struct {
	Fingerprint     string
	AdditionalParts []string
}

func fingerprintsOnly(sources map[string]ScannedSource, err error) (map[string]string, error) {
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sources))
	for path, source := range sources {
		out[path] = source.Fingerprint
	}
	return out, nil
}

func (s *Store) ScanFingerprints(libraryID int64) (map[string]string, error) {
	return fingerprintsOnly(s.ScanSources(libraryID))
}
func (s *Store) DirectoryScanFingerprints(libraryID int64, directory string, recursive bool) (map[string]string, error) {
	return fingerprintsOnly(s.DirectoryScanSources(libraryID, directory, recursive))
}
func (s *Store) SourcePrefixScanFingerprints(libraryID int64, directory string, prefixes []string) (map[string]string, error) {
	return fingerprintsOnly(s.SourcePrefixScanSources(libraryID, directory, prefixes))
}

func (s *Store) ScanSources(libraryID int64) (map[string]ScannedSource, error) {
	return s.scanSources("movies.library_id=?", libraryID)
}

func (s *Store) DirectoryScanSources(libraryID int64, directory string, recursive bool) (map[string]ScannedSource, error) {
	if !recursive {
		return s.scanSources("movies.library_id=? AND movies.output_dir=?", libraryID, directory)
	}
	prefix := directory
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return s.scanSources("movies.library_id=? AND (movies.output_dir=? OR (movies.output_dir>=? AND movies.output_dir<?))", libraryID, directory, prefix, prefix+"\U0010ffff")
}

// SourcePrefixScanSources limits returned fingerprints and parts to the
// source families implicated by a file event. The scanner still applies its
// exact CD grouping rules to this conservative prefix match.
func (s *Store) SourcePrefixScanSources(libraryID int64, directory string, prefixes []string) (map[string]ScannedSource, error) {
	if len(prefixes) == 0 {
		return map[string]ScannedSource{}, nil
	}
	// SQLite LIKE only folds ASCII case. Preserve Go's Unicode CD grouping, and
	// avoid a large OR expression when a batch already covers much of a folder.
	if len(prefixes) > 32 {
		return s.DirectoryScanSources(libraryID, directory, false)
	}
	for _, prefix := range prefixes {
		for _, character := range filepath.Base(prefix) {
			if character > 127 {
				return s.DirectoryScanSources(libraryID, directory, false)
			}
		}
	}
	conditions := make([]string, len(prefixes))
	args := []any{libraryID, directory}
	escape := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	for index, prefix := range prefixes {
		conditions[index] = "movies.source_path LIKE ? ESCAPE '!'"
		args = append(args, escape.Replace(prefix)+"%")
	}
	return s.scanSources("movies.library_id=? AND movies.output_dir=? AND ("+strings.Join(conditions, " OR ")+")", args...)
}

func (s *Store) scanSources(condition string, args ...any) (map[string]ScannedSource, error) {
	rows, err := s.db.Query(`SELECT movies.source_path, COALESCE(scan_fingerprints.fingerprint, ''), COALESCE(movies.additional_parts, '[]')
FROM movies LEFT JOIN scan_fingerprints ON scan_fingerprints.movie_id=movies.id WHERE `+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := make(map[string]ScannedSource)
	for rows.Next() {
		var path, fingerprint, parts string
		if err := rows.Scan(&path, &fingerprint, &parts); err != nil {
			return nil, err
		}
		sources[path] = ScannedSource{Fingerprint: fingerprint, AdditionalParts: parseStrings(parts)}
	}
	return sources, rows.Err()
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
	ids := []int64{}
	for _, path := range paths {
		var id int64
		err := transaction.QueryRow("DELETE FROM movies WHERE library_id=? AND source_path=? RETURNING id", libraryID, path).Scan(&id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return 0, err
		}
		deleted++
		ids = append(ids, id)
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.TouchMovie(id)
	}
	return deleted, nil
}

func (s *Store) SaveScanFingerprint(libraryID int64, path, fingerprint string) error {
	_, err := s.db.Exec(`INSERT INTO scan_fingerprints(movie_id, fingerprint)
SELECT id, ? FROM movies WHERE library_id=? AND source_path=?
ON CONFLICT(movie_id) DO UPDATE SET fingerprint=excluded.fingerprint`, fingerprint, libraryID, path)
	return err
}

package store

import (
	"strconv"
	"strings"
	"sync/atomic"
)

func (s *Store) BumpVersion(libraryID int64) error {
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO kv(key,value) VALUES('g:version','1') ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`); err != nil {
		return err
	}
	key := "lib:" + strconv.FormatInt(libraryID, 10) + ":version"
	if _, err = tx.Exec(`INSERT INTO kv(key,value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`, key); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.gversion.Add(1)
	s.libraryVersions[key]++
	return nil
}
func (s *Store) Version(key string) string {
	if key == "g:version" {
		return strconv.FormatInt(s.gversion.Load(), 10)
	}
	if strings.HasPrefix(key, "lib:") && strings.HasSuffix(key, ":version") {
		s.versionMu.RLock()
		n := s.libraryVersions[key]
		s.versionMu.RUnlock()
		return strconv.FormatInt(n, 10)
	}
	var value string
	_ = s.db.QueryRow("SELECT value FROM kv WHERE key=?", key).Scan(&value)
	return value
}

// MovieVersion combines metadata and user-state revisions for detail responses.
// A fresh process epoch prevents reuse after restart or database replacement.
func (s *Store) MovieVersion(id int64) string {
	var userVersion uint64
	if v, ok := s.userDataVersions.Load(id); ok {
		userVersion = v.(*atomic.Uint64).Load()
	}
	return s.MovieMetadataVersion(id) + ":" + strconv.FormatUint(userVersion, 10)
}

// MovieMetadataVersion excludes playback and other user state. Image paths and
// source metadata remain reusable through frequent playback heartbeats.
func (s *Store) MovieMetadataVersion(id int64) string {
	var n uint64
	if v, ok := s.movieVersions.Load(id); ok {
		n = v.(*atomic.Uint64).Load()
	}
	libraryID, ok := s.movieLibraries.Load(id)
	if !ok {
		var lib int64
		if err := s.db.QueryRow("SELECT library_id FROM movies WHERE id=?", id).Scan(&lib); err == nil {
			libraryID, _ = s.movieLibraries.LoadOrStore(id, lib)
		}
	}
	var libraryVersion uint64
	if libraryID != nil {
		if version, ok := s.libraryMovieVersions.Load(libraryID.(int64)); ok {
			libraryVersion = version.(*atomic.Uint64).Load()
		}
	}
	return strconv.FormatUint(libraryVersion, 10) + ":" + s.movieEpoch + ":" + strconv.FormatUint(s.actorVersion.Load(), 10) + ":" + strconv.FormatUint(n, 10)
}
func (s *Store) ActorVersion() string { return strconv.FormatUint(s.actorVersion.Load(), 10) }
func (s *Store) touchUserData(id int64) {
	v, _ := s.userDataVersions.LoadOrStore(id, &atomic.Uint64{})
	v.(*atomic.Uint64).Add(1)
}
func (s *Store) TouchMovie(id int64) {
	if v, ok := s.movieVersions.Load(id); ok {
		v.(*atomic.Uint64).Add(1)
		return
	}
	v, _ := s.movieVersions.LoadOrStore(id, &atomic.Uint64{})
	v.(*atomic.Uint64).Add(1)
}

func (s *Store) ScrapeVersion(libraryID int64) string {
	if libraryID == 0 {
		return strconv.FormatUint(s.scrapeVersion.Load(), 10)
	}
	var n uint64
	if v, ok := s.scrapeVersions.Load(libraryID); ok {
		n = v.(*atomic.Uint64).Load()
	}
	return strconv.FormatUint(n, 10)
}

// Full-library reconciliation evicts disk dependencies after scanning. Advance
// this library's detail epoch afterwards so concurrent old fills cannot survive.
func (s *Store) InvalidateLibraryMovies(libraryID int64) {
	version, _ := s.libraryMovieVersions.LoadOrStore(libraryID, &atomic.Uint64{})
	version.(*atomic.Uint64).Add(1)
}

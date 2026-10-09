package store

import (
	"path/filepath"
	"testing"
)

func TestMovieCacheVersionsFollowSuccessfulWritesOnly(t *testing.T) {
	s, libID := newFeatureStore(t)
	m := Movie{LibraryID: libID, SourcePath: "/tmp/av/revision.strm", Status: "success", Title: "Movie"}
	id := upsertFixtureMovie(t, s, libID, m)
	other := m
	other.SourcePath = "/tmp/av/other.strm"
	otherID := upsertFixtureMovie(t, s, libID, other)
	version, otherVersion := s.MovieVersion(id), s.MovieVersion(otherID)
	entry := ScannedMovie{Movie: m}
	entry.Movie.Title = "Changed"
	bad := entry
	bad.Movie.SourcePath = "/tmp/av/bad.strm"
	bad.Movie.LibraryID = libID + 999
	if err := s.SaveScannedMovies([]ScannedMovie{entry, bad}); err == nil {
		t.Fatal("invalid write accepted")
	}
	if s.MovieVersion(id) != version {
		t.Fatal("rollback advanced revision")
	}
	if err := s.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		t.Fatal(err)
	}
	if s.MovieVersion(id) == version || s.MovieVersion(otherID) != otherVersion {
		t.Fatal("incorrect movie revision scope")
	}
	version = s.MovieVersion(id)
	metadataVersion := s.MovieMetadataVersion(id)
	if err := s.SavePlayback(id, 123, 1, "", 0); err != nil {
		t.Fatal(err)
	}
	if s.MovieVersion(id) == version || s.MovieVersion(otherID) != otherVersion {
		t.Fatal("playback revision scope")
	}
	if s.MovieMetadataVersion(id) != metadataVersion {
		t.Fatal("playback invalidated media metadata")
	}
	version = s.MovieVersion(id)
	if _, err := s.DeleteScannedSources(libID, []string{m.SourcePath}); err != nil {
		t.Fatal(err)
	}
	if s.MovieVersion(id) == version {
		t.Fatal("deletion did not invalidate movie")
	}
}
func TestLibraryCacheVersionsPersistAndRemainScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BumpVersion(1); err != nil {
		t.Fatal(err)
	}
	if s.Version("lib:1:version") != "1" || s.Version("lib:2:version") != "0" {
		t.Fatal("wrong library version")
	}
	before := s.MovieVersion(1)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Version("lib:1:version") != "1" {
		t.Fatal("library version not restored")
	}
	if s.MovieVersion(1) == before {
		t.Fatal("restart reused movie generation")
	}
	if err := s.BumpVersion(2); err != nil {
		t.Fatal(err)
	}
	if s.Version("lib:1:version") != "1" || s.Version("lib:2:version") != "1" {
		t.Fatal("unrelated library invalidated")
	}
}

package store

import (
	"testing"
	"time"
)

func TestSaveScannedMoviesAtomicAndComplete(test *testing.T) {
	database, libraryID := newFeatureStore(test)
	entry := ScannedMovie{
		Movie:  Movie{LibraryID: libraryID, SourcePath: "/tmp/av/batch.strm", Status: "success", Title: "Batch", Genres: []string{"Drama"}},
		Actors: []ActorRef{{Name: "Actor", AvatarURL: "https://example.test/actor.jpg"}},
		Size:   10, ModTime: time.Unix(1700000000, 0), Fingerprint: "v1:initial",
	}
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		test.Fatal(err)
	}
	id, err := database.MovieIDByPath(entry.Movie.SourcePath)
	if err != nil {
		test.Fatal(err)
	}
	before, err := database.Movie(id)
	if err != nil {
		test.Fatal(err)
	}
	var featureCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM movie_features WHERE movie_id=?", id).Scan(&featureCount); err != nil || featureCount != 2 {
		test.Fatalf("genre and actor features: count=%d err=%v", featureCount, err)
	}
	entry.Movie.Title, entry.Movie.Genres = "Updated", []string{"Comedy"}
	entry.Actors = []ActorRef{{Name: "Actor"}}
	entry.Fingerprint = "v1:updated"
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		test.Fatal(err)
	}
	after, err := database.Movie(id)
	if err != nil || after.Title != "Updated" || after.CreatedAt != before.CreatedAt {
		test.Fatalf("upsert changed identity: %+v %v", after, err)
	}
	actors, err := database.Actors(id)
	if err != nil || len(actors) != 1 || actors[0].AvatarURL != "https://example.test/actor.jpg" {
		test.Fatalf("actor avatar lost: %+v %v", actors, err)
	}
	fingerprints, err := database.ScanFingerprints(libraryID)
	if err != nil || fingerprints[entry.Movie.SourcePath] != "v1:updated" {
		test.Fatalf("fingerprint not saved: %+v %v", fingerprints, err)
	}
	entry.Movie.Title = "Must roll back"
	entry.Actors = []ActorRef{{Name: "Wrong actor"}}
	entry.Fingerprint = "v1:wrong"
	invalid := entry
	invalid.Movie.SourcePath = "/tmp/av/invalid.strm"
	invalid.Movie.LibraryID = libraryID + 999
	if err := database.SaveScannedMovies([]ScannedMovie{entry, invalid}); err == nil {
		test.Fatal("invalid batch should fail")
	}
	after, err = database.Movie(id)
	fingerprints, fingerprintErr := database.ScanFingerprints(libraryID)
	actors, actorErr := database.Actors(id)
	if err != nil || fingerprintErr != nil || actorErr != nil || after.Title != "Updated" || fingerprints[entry.Movie.SourcePath] != "v1:updated" || len(fingerprints) != 1 || len(actors) != 1 || actors[0].Name != "Actor" {
		test.Fatalf("partial batch committed: movie=%+v fingerprints=%v actors=%v errors=%v/%v/%v", after, fingerprints, actors, err, fingerprintErr, actorErr)
	}
	entry.Movie.Title = "Unstable"
	entry.Fingerprint = ""
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		test.Fatal(err)
	}
	fingerprints, err = database.ScanFingerprints(libraryID)
	if err != nil || fingerprints[entry.Movie.SourcePath] != "" {
		test.Fatalf("unstable scan retained stale fingerprint: %v %v", fingerprints, err)
	}
}

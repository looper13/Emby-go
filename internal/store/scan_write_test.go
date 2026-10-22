package store

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestScanBatchMergesActorAvatarsAndRollsBackFeatureFailure(t *testing.T) {
	s, libID := newFeatureStore(t)
	if err := s.SetActorAvatar("Shared", "https://example.test/old.jpg", "local-tag"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TABLE actor_write_count(name TEXT);
CREATE TRIGGER count_actor_update AFTER UPDATE ON actors BEGIN INSERT INTO actor_write_count VALUES(NEW.name); END;
CREATE TRIGGER count_actor_insert AFTER INSERT ON actors BEGIN INSERT INTO actor_write_count VALUES(NEW.name); END;`); err != nil {
		t.Fatal(err)
	}
	entries := make([]ScannedMovie, 3)
	for i, avatar := range []string{"https://example.test/first.jpg", "", " https://example.test/last.jpg "} {
		entries[i] = ScannedMovie{Movie: Movie{LibraryID: libID, SourcePath: fmt.Sprintf("/tmp/av/merged-%d.strm", i), Status: "success", Title: "Merged"}, Actors: []ActorRef{{Name: " Shared ", AvatarURL: avatar}, {Name: "Shared"}, {Name: " "}}}
	}
	if err := s.SaveScannedMovies(entries); err != nil {
		t.Fatal(err)
	}
	var avatar, tag string
	var writes int
	if err := s.db.QueryRow("SELECT avatar_url,avatar_tag FROM actors WHERE name='Shared'").Scan(&avatar, &tag); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM actor_write_count WHERE name='Shared'").Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if avatar != "https://example.test/last.jpg" || tag != "local-tag" || writes != 1 {
		t.Fatalf("avatar=%q tag=%q writes=%d", avatar, tag, writes)
	}
	for _, entry := range entries {
		id, err := s.MovieIDByPath(entry.Movie.SourcePath)
		if err != nil {
			t.Fatal(err)
		}
		actors, err := s.Actors(id)
		if err != nil || len(actors) != 1 || actors[0].Name != "Shared" {
			t.Fatalf("actor links=%v err=%v", actors, err)
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_feature BEFORE INSERT ON movie_features WHEN NEW.value='Rejected' BEGIN SELECT RAISE(ABORT,'feature rejected'); END;`); err != nil {
		t.Fatal(err)
	}
	entries[0].Movie.Title = "Must roll back"
	entries[0].Actors = []ActorRef{{Name: "Shared", AvatarURL: "https://example.test/wrong.jpg"}}
	entries[1].Actors = []ActorRef{{Name: "Rejected"}}
	if err := s.SaveScannedMovies(entries); err == nil {
		t.Fatal("feature failure accepted")
	}
	var title string
	if err := s.db.QueryRow("SELECT title FROM movies WHERE source_path=?", entries[0].Movie.SourcePath).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT avatar_url FROM actors WHERE name='Shared'").Scan(&avatar); err != nil {
		t.Fatal(err)
	}
	var rejected int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM actors WHERE name='Rejected'").Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if title != "Merged" || avatar != "https://example.test/last.jpg" || rejected != 0 {
		t.Fatalf("partial transaction survived: title=%q avatar=%q rejected=%d", title, avatar, rejected)
	}
}

func TestScanFeaturesLargeBatchAndRemoval(t *testing.T) {
	s, libID := newFeatureStore(t)
	entry := ScannedMovie{Movie: Movie{LibraryID: libID, SourcePath: "/tmp/av/large-features.strm", Status: "success"}}
	for i := 0; i < 300; i++ {
		entry.Movie.Tags = append(entry.Movie.Tags, fmt.Sprintf("Tag %03d", i))
	}
	for _, count := range []int{300, 129, 128, 0} {
		entry.Movie.Tags = entry.Movie.Tags[:count]
		if err := s.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
			t.Fatal(err)
		}
		var actual int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM movie_features").Scan(&actual); err != nil || actual != count {
			t.Fatalf("features=%d want=%d err=%v", actual, count, err)
		}
	}
}

func TestSaveScannedMoviesAtomicAndComplete(test *testing.T) {
	database, libraryID := newFeatureStore(test)
	entry := ScannedMovie{
		Movie:  Movie{LibraryID: libraryID, SourcePath: "/tmp/av/batch.strm", Status: "success", Title: "Batch", Genres: []string{"Drama"}},
		Actors: []ActorRef{{Name: "Actor", AvatarURL: "https://example.test/actor.jpg"}},
		Size:   10, ModTime: time.Unix(1700000000, 0), Fingerprint: "v1:initial",
	}
	stats, err := database.SaveScannedMoviesWithStats([]ScannedMovie{entry})
	if err != nil {
		test.Fatal(err)
	}
	stages := stats.Begin + stats.Prepare + stats.Movie + stats.Actors + stats.Features + stats.Fingerprint + stats.Commit
	if stats.Total <= 0 || stats.Total < stages {
		test.Fatalf("committed batch timing does not cover its stages: %+v", stats)
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
	stats, err = database.SaveScannedMoviesWithStats([]ScannedMovie{entry, invalid})
	if err == nil {
		test.Fatal("invalid batch should fail")
	}
	if stats.Total < stats.Movie || stats.Commit != 0 {
		test.Fatalf("failed batch timing omitted SQL failure or reported a commit: %+v", stats)
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

func TestScanFeaturesMatchMetadataAndKeepUnchangedRelations(t *testing.T) {
	database, libraryID := newFeatureStore(t)
	entry := ScannedMovie{
		Movie:  Movie{LibraryID: libraryID, SourcePath: "/tmp/av/parity.strm", Status: "success", Genres: []string{" Drama ", "Drama"}, Tags: []string{"Tag"}, Studios: []string{"Studio"}, Director: " Director ", Collection: "Collection", Series: "Fallback"},
		Actors: []ActorRef{{Name: " Actor "}, {Name: "Actor"}, {Name: " "}},
	}
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		t.Fatal(err)
	}
	id, err := database.MovieIDByPath(entry.Movie.SourcePath)
	if err != nil {
		t.Fatal(err)
	}
	read := func() map[string]int {
		t.Helper()
		rows, err := database.db.Query("SELECT kind,value,weight FROM movie_features WHERE movie_id=?", id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := map[string]int{}
		for rows.Next() {
			var kind, value string
			var weight int
			if err := rows.Scan(&kind, &value, &weight); err != nil {
				t.Fatal(err)
			}
			result[kind+"\x00"+value] = weight
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := read()
	if err := database.refreshFeatures(id); err != nil {
		t.Fatal(err)
	}
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatalf("scan features differ from metadata features: %v / %v", before, after)
	}
	if _, err := database.db.Exec(`CREATE TRIGGER reject_actor_delete BEFORE DELETE ON movie_actors BEGIN SELECT RAISE(ABORT,'unchanged actors rewritten'); END;
 CREATE TRIGGER reject_feature_delete BEFORE DELETE ON movie_features BEGIN SELECT RAISE(ABORT,'unchanged features rewritten'); END;`); err != nil {
		t.Fatal(err)
	}
	entry.Movie.Title = "Only title changed"
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec("DROP TRIGGER reject_actor_delete; DROP TRIGGER reject_feature_delete;"); err != nil {
		t.Fatal(err)
	}
	entry.Actors = []ActorRef{{Name: "New actor"}}
	entry.Movie.Collection = ""
	if err := database.SaveScannedMovies([]ScannedMovie{entry}); err != nil {
		t.Fatal(err)
	}
	actors, err := database.Actors(id)
	if err != nil || len(actors) != 1 || actors[0].Name != "New actor" {
		t.Fatalf("actor replacement: %v %v", actors, err)
	}
	before = read()
	if err := database.refreshFeatures(id); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, read()) {
		t.Fatal("changed features differ, including fallback series")
	}
}

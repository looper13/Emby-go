package store

import (
	"database/sql"
	"testing"
)

func TestNullableMetadataAcrossCatalogReads(t *testing.T) {
	s, libraryID := newFeatureStore(t)
	<-s.featuresReady
	result, err := s.db.Exec(`INSERT INTO movies(library_id,source_path,status,nfo_path,collection,created_at,updated_at)
		VALUES(?, '/tmp/av/null.strm', 'success', '/tmp/av/null.nfo', 'Null series', '', '')`, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	movie, err := s.Movie(id)
	if err != nil || movie.Title != "" || movie.Year != 0 || movie.Rating != 0 || movie.RuntimeSeconds != 0 || movie.Television != nil {
		t.Fatalf("schema-legal NULL metadata failed: movie=%+v err=%v", movie, err)
	}
	movies, total, err := s.SearchAdmin(AdminQuery{LibraryID: libraryID, Limit: 10})
	if err != nil || total != 1 || len(movies) != 1 || movies[0].ID != id {
		t.Fatalf("catalog omitted nullable movie: movies=%v total=%d err=%v", movies, total, err)
	}
	byID, err := s.MoviesByIDs([]int64{id})
	if err != nil || len(byID) != 1 {
		t.Fatalf("batch read failed: movies=%v err=%v", byID, err)
	}
	probe, err := s.MoviesForProbe(libraryID, "success", 10)
	if err != nil || len(probe) != 1 || probe[0].Title != "" {
		t.Fatalf("probe read failed: movies=%v err=%v", probe, err)
	}
	stats, err := s.CollectionStats(1)
	if err != nil || stats["Null series"].Count != 1 {
		t.Fatalf("collection read failed: stats=%v err=%v", stats, err)
	}
	count, genres, err := s.CollectionSummary("Null series")
	if err != nil || count != 1 || len(genres) != 0 {
		t.Fatalf("collection summary failed: count=%d genres=%v err=%v", count, genres, err)
	}
	var storedTitle sql.NullString
	if err := s.db.QueryRow("SELECT title FROM movies WHERE id=?", id).Scan(&storedTitle); err != nil || storedTitle.Valid {
		t.Fatalf("read defaults must not rewrite NULL: title=%v err=%v", storedTitle, err)
	}
	if _, err := s.db.Exec("UPDATE movies SET year='invalid-year' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Movie(id); err == nil {
		t.Fatal("non-null conversion errors must remain visible")
	}
}

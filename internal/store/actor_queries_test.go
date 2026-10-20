package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActorBatchQueries(t *testing.T) {
	s, library := newFeatureStore(t)
	s.waitFeaturesReady()
	other, err := s.AddLibrary("Other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, fixture := range []struct {
		library                              int64
		status, poster, collection, director string
		names                                []string
	}{
		{library, "success", "old.jpg", "", "", []string{"A"}},
		{library, "manual", "", "Group", "", []string{"A", "B"}},
		{library, "pending", "hidden.jpg", "", "", []string{"A"}},
		{other.ID, "success", "other.jpg", "", "", []string{"A"}},
		{library, "success", "director.jpg", "", "Director", nil},
	} {
		id, err := s.UpsertMovie(Movie{LibraryID: fixture.library, SourcePath: fmt.Sprintf("movie-%d.strm", i), Status: fixture.status, PosterPath: fixture.poster, Collection: fixture.collection, Director: fixture.director}, 0, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		refs := []ActorRef{}
		for _, name := range fixture.names {
			refs = append(refs, ActorRef{Name: name})
		}
		if err := s.ReplaceActors(id, refs); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetActorAvatar("A", "https://example.invalid/a", "tag"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActorAvatar("Orphan", "", ""); err != nil {
		t.Fatal(err)
	}
	names := []string{"A", "B", "Director", "Missing", "A"}
	avatars, err := s.ActorAvatars(context.Background(), names)
	if err != nil || len(avatars) != 2 || avatars["A"].AvatarTag != "tag" || avatars["A"].AvatarURL != "https://example.invalid/a" {
		t.Fatalf("%+v %v", avatars, err)
	}
	posters, err := s.PersonPosters(context.Background(), names)
	if err != nil || len(posters) != 2 || posters["A"] != "other.jpg" || posters["B"] != "movie-1.strm" {
		t.Fatalf("%+v %v", posters, err)
	}
	for _, name := range names {
		path, err := s.EntityPoster("Person", name, 0, "")
		if err != nil || path != posters[name] {
			t.Fatalf("single/batch mismatch %s: %s %v", name, path, err)
		}
	}
	for _, collection := range []string{"", "*", "Group"} {
		path, err := s.EntityPoster("Person", "A", library, collection)
		if err != nil || path != "movie-1.strm" {
			t.Fatalf("scoped %q: %s %v", collection, path, err)
		}
	}
	summaries, err := s.ActorSummaries(context.Background(), library)
	if err != nil || len(summaries) != 2 || summaries[0].MovieCount != 2 || summaries[0].AvatarTag != "tag" || summaries[1].MovieCount != 1 {
		t.Fatalf("%+v %v", summaries, err)
	}
	for _, query := range []func(context.Context, []string) error{
		func(ctx context.Context, names []string) error { _, err := s.ActorAvatars(ctx, names); return err },
		func(ctx context.Context, names []string) error { _, err := s.PersonPosters(ctx, names); return err },
	} {
		if err := query(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := query(ctx, names); !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation ignored", err)
		}
	}
	// Cross multiple SQL bind batches without dropping the final names.
	refs := make([]ActorRef, actorQueryBatchSize*2+3)
	names = names[:0]
	for i := range refs {
		refs[i] = ActorRef{Name: fmt.Sprintf("Chunk%04d", i)}
		names = append(names, refs[i].Name)
	}
	if err := s.ReplaceActors(1, refs); err != nil {
		t.Fatal(err)
	}
	avatars, err = s.ActorAvatars(context.Background(), names)
	if err != nil || len(avatars) != len(names) {
		t.Fatalf("avatars=%d %v", len(avatars), err)
	}
	posters, err = s.PersonPosters(context.Background(), names)
	if err != nil || len(posters) != len(names) || posters[names[len(names)-1]] != "old.jpg" {
		t.Fatalf("posters=%d %v", len(posters), err)
	}
}

func TestActorLookupIndexUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.waitFeaturesReady()
	if _, err := s.db.Exec("DROP INDEX idx_movie_actors_actor_movie"); err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.waitFeaturesReady()
	var id, parent, unused int
	var detail string
	if err := s.db.QueryRow("EXPLAIN QUERY PLAN SELECT movie_id FROM movie_actors WHERE actor_name=?", "A").Scan(&id, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "idx_movie_actors_actor_movie") {
		t.Fatal("actor lookup did not use inverse index", detail)
	}
}

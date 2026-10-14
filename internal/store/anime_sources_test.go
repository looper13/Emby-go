package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"emby-go/internal/anime"
)

func TestAnimeArchivePreservesIdentityAndNewerAPI(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	sub := anime.Subject{BangumiID: 123, Name: "Archive", Platform: "TV", Date: "2026-10-01"}
	eps := []anime.Episode{{BangumiID: 456, Sort: json.Number("1"), Number: json.Number("1"), Season: 1}}
	id, err := s.ImportAnimeSubject(ctx, sub, eps, "a", "2000-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.AnimeEpisodes(id)
	sub.Name = "API"
	sub.Image = "https://lain.bgm.tv/pic/cover/l/a.jpg"
	id2, err := s.SaveAnimeSubject(ctx, sub, eps)
	if err != nil || id != id2 {
		t.Fatal(id, id2, err)
	}
	sub.Name = "Old archive"
	sub.Image = ""
	if _, err = s.ImportAnimeSubject(ctx, sub, eps, "b", "2001-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	current, _ := s.AnimeSubject(id)
	if current.Name != "API" || current.Image == "" {
		t.Fatal("old archive overwrote API", current)
	}
	if _, err = s.ImportAnimeSubject(ctx, sub, eps, "c", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	current, _ = s.AnimeSubject(id)
	second, _ := s.AnimeEpisodes(id)
	if current.Name != "Old archive" || current.Image == "" || second[0].ID != first[0].ID || second[0].Index != first[0].Index {
		t.Fatal(current, second)
	}
}

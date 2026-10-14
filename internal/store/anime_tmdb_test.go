package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"emby-go/internal/anime"
)

func TestAnimeTMDBMappingsConflictsAndSecrets(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.InitializeAnimeTMDB("initial-fixture-key"); err != nil {
		t.Fatal(err)
	}
	sub, err := s.SaveAnimeSubject(ctx, anime.Subject{BangumiID: 11, Name: "Test", Platform: "TV"}, []anime.Episode{{BangumiID: 12, Sort: json.Number("1"), Season: 1}, {BangumiID: 13, Sort: json.Number("2"), Season: 1}})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := s.AnimeEpisodes(sub)
	cfg, _ := s.AnimeTMDBConfig()
	if err = s.SaveAnimeTMDBConfig("fixture-secret", false, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.AnimeTMDBConfig()
	raw, _ := json.Marshal(cfg)
	if !cfg.Configured || strings.Contains(string(raw), "fixture-secret") {
		t.Fatal("secret response leak")
	}
	if err = s.SaveAnimeTMDBConfig("bad", false, cfg.Revision-1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	series := AnimeTMDBSeries{SubjectID: sub, SeriesID: 123, Name: "Remote"}
	if err = s.SaveAnimeTMDBSeries(ctx, series, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	series.Revision = 1
	m := AnimeTMDBEpisode{EpisodeID: eps[0].ID, SubjectID: sub, SeriesID: 123, Season: 0, Number: 2, TMDBID: 456, Name: "Special", Stills: []string{"/one.jpg"}}
	if err = s.SaveAnimeTMDBEpisode(ctx, m, series.Revision, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAnimeTMDBEpisode(ctx, m, series.Revision, cfg.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write", err)
	}
	m.EpisodeID = eps[1].ID
	if err = s.SaveAnimeTMDBEpisode(ctx, m, series.Revision, cfg.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate remote mapping", err)
	}
	series.SeriesID = 321
	if err = s.SaveAnimeTMDBSeries(ctx, series, cfg.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("orphaned mappings", err)
	}
	saved, _ := s.AnimeTMDBEpisode(eps[0].ID)
	if saved.SeriesID != 123 || saved.TMDBID != 456 || len(saved.Stills) != 1 {
		t.Fatal(saved)
	}
	clear := AnimeTMDBEpisode{EpisodeID: eps[0].ID, SubjectID: sub, Revision: saved.Revision, Stills: []string{}}
	if err = s.SaveAnimeTMDBEpisode(ctx, clear, series.Revision, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAnimeTMDBSeries(ctx, series, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAnimeTMDBConfig("", true, cfg.Revision); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.AnimeTMDBConfig()
	if cfg.Configured {
		t.Fatal("clear failed")
	}
	if err = s.InitializeAnimeTMDB("initial-fixture-key"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.AnimeTMDBConfig()
	if cfg.Configured {
		t.Fatal("restart restored cleared default")
	}
}

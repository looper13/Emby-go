package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTMDBDefaultPriority(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anime-tmdb.local.yaml"), []byte("tmdb_api_key: local-fixture-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("port: 18080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.TMDBAPIKey != "local-fixture-key" {
		t.Fatal("local default failed", err)
	}
	if err := os.WriteFile(path, []byte("tmdb_api_key: explicit-fixture-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil || cfg.TMDBAPIKey != "explicit-fixture-key" {
		t.Fatal("explicit setting failed", err)
	}
	t.Setenv("TMDB_API_KEY", "environment-fixture-key")
	cfg, err = Load(path)
	if err != nil || cfg.TMDBAPIKey != "environment-fixture-key" {
		t.Fatal("environment setting failed", err)
	}
}

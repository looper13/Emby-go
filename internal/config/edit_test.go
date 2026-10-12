package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func editableConfig(t *testing.T) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "# keep header\nport: 18080 # keep port comment\nredis_addr: localhost:6379\nredis_password: secret-value\ncustom_future:\n  nested: retained # keep unknown\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestConfigEditPreservesDocumentAndSecrets(t *testing.T) {
	cfg := editableConfig(t)
	_, rev, err := cfg.FileSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	saved, next, err := cfg.UpdateFile(Patch{Port: ptr(18081), ServerName: ptr("新服务"), RedisPassword: ptr(""), ServerDomains: ptr([]ServerDomain{{Name: "内网", URL: "http://localhost:18081/emby"}})}, rev)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RedisPassword != "secret-value" || saved.Port != 18081 || next == rev {
		t.Fatal("save result incorrect")
	}
	raw, _ := os.ReadFile(cfg.ConfigPath())
	for _, retained := range []string{"# keep header", "# keep port comment", "custom_future:", "nested: retained", "# keep unknown"} {
		if !strings.Contains(string(raw), retained) {
			t.Fatalf("lost %s", retained)
		}
	}
	reloaded, err := Load(cfg.ConfigPath())
	if err != nil || reloaded.ServerName != "新服务" || len(reloaded.ServerDomains) != 1 {
		t.Fatalf("reload failed: %v", err)
	}
	_, rev, _ = cfg.FileSnapshot()
	saved, _, err = cfg.UpdateFile(Patch{ClearRedisPassword: true}, rev)
	if err != nil || saved.RedisPassword != "" {
		t.Fatalf("clear: %v", err)
	}
}

func TestConfigEditRejectsInvalidAndStaleWrites(t *testing.T) {
	cfg := editableConfig(t)
	_, rev, _ := cfg.FileSnapshot()
	original, _ := os.ReadFile(cfg.ConfigPath())
	for _, patch := range []Patch{
		{Port: ptr(0)}, {Port: ptr(65536)}, {LibraryMonitorMode: ptr("typo")}, {DBPath: ptr("")},
		{ProbeConcurrency: ptr(9)}, {ProbeTimeoutSec: ptr(-1)}, {RedisDB: ptr(-1)}, {RedisAddr: ptr("no-port")},
		{ServerName: ptr(" ")}, {ServerDomains: ptr([]ServerDomain{{Name: "test", URL: "javascript:alert(1)"}})},
		{ServerDomains: ptr([]ServerDomain{{Name: "test", URL: "https://user:password@example.com"}})},
		{ClearRedisPassword: true, RedisPassword: ptr("new")},
	} {
		_, _, err := cfg.UpdateFile(patch, rev)
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("expected validation error, got %v", err)
		}
		now, _ := os.ReadFile(cfg.ConfigPath())
		if string(now) != string(original) {
			t.Fatal("invalid write changed file")
		}
	}
	if _, _, err := cfg.UpdateFile(Patch{Port: ptr(18082)}, "stale"); !errors.Is(err, ErrConfigConflict) {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), append(original, []byte("# external edit\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.UpdateFile(Patch{Port: ptr(18082)}, rev); !errors.Is(err, ErrConfigConflict) {
		t.Fatal(err)
	}
	if _, _, err := (Config{}).UpdateFile(Patch{}, rev); !errors.Is(err, ErrNoConfigFile) {
		t.Fatal(err)
	}
}

func TestConfigEditRejectsBrokenDocumentWithoutLeakingValues(t *testing.T) {
	cfg := editableConfig(t)
	for _, body := range []string{"redis_db: secret-value\n", "port: 18080\n---\nredis_password: secret-value\n", "port: 18080\nport: 80\n", "- secret-value\n"} {
		if err := os.WriteFile(cfg.ConfigPath(), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, _, err := cfg.FileSnapshot()
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("bad document handling: %v", err)
		}
	}
}

func TestConfigReplaceFailureKeepsOriginal(t *testing.T) {
	cfg := editableConfig(t)
	before, _ := os.ReadFile(cfg.ConfigPath())
	// A path whose parent component is a regular file cannot be replaced.
	invalid := Config{path: filepath.Join(cfg.ConfigPath(), "nested.yaml")}
	if err := invalid.replaceFile([]byte("invalid")); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	after, _ := os.ReadFile(cfg.ConfigPath())
	if string(before) != string(after) {
		t.Fatal("failed write damaged existing file")
	}
}

func TestConfigEditKeepsAliasesValid(t *testing.T) {
	cfg := editableConfig(t)
	if err := os.WriteFile(cfg.ConfigPath(), []byte("server_name: &name old\ncustom_copy: *name\nredis_addr: localhost:6379\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := cfg.FileSnapshot()
	if _, _, err := cfg.UpdateFile(Patch{ServerName: ptr("new")}, rev); err != nil {
		t.Fatal(err)
	}
	if loaded, err := Load(cfg.ConfigPath()); err != nil || loaded.ServerName != "new" {
		t.Fatalf("reload after alias edit: %v", err)
	}
	raw, _ := os.ReadFile(cfg.ConfigPath())
	if !strings.Contains(string(raw), "&name") || !strings.Contains(string(raw), "*name") {
		t.Fatal("anchor or alias lost")
	}
}

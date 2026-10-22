package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLogRetentionConfigBounds(t *testing.T) {
	for _, days := range []int{-1, 0, 7, 3650, 3651} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("log_retention_days: %d\n", days)), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		valid := days >= 0 && days <= 3650
		if (err == nil) != valid || (valid && cfg.LogRetentionDays != days) {
			t.Fatalf("days=%d cfg=%+v err=%v", days, cfg, err)
		}
	}
}

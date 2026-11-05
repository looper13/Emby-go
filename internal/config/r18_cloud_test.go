package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferredR18CloudDefaultsAndLoadValidation(t *testing.T) {
	if got := (Config{}).PreferredR18Cloud(); got != "guangya" {
		t.Fatalf("零值配置未默认使用光鸭：%q", got)
	}
	if cfg, err := Load(""); err != nil || cfg.R18PreferredCloud != "guangya" {
		t.Fatalf("无配置文件时未默认使用光鸭：%q %v", cfg.R18PreferredCloud, err)
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"未配置", "", "guangya"},
		{"光鸭", "r18_preferred_cloud: guangya\n", "guangya"},
		{"115字符串", "r18_preferred_cloud: '115'\n", "115"},
		{"115数字写法", "r18_preferred_cloud: 115\n", "115"},
		{"空字符串", "r18_preferred_cloud: ''\n", ""},
		{"未知网盘", "r18_preferred_cloud: other\n", ""},
		{"大小写不符", "r18_preferred_cloud: Guangya\n", ""},
		{"额外空格", "r18_preferred_cloud: ' 115 '\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "r18_preferred_cloud") {
					t.Fatalf("未拒绝非法网盘配置：%v", err)
				}
				return
			}
			if err != nil || cfg.R18PreferredCloud != tc.want || cfg.PreferredR18Cloud() != tc.want {
				t.Fatalf("加载的优先网盘错误：字段=%q 有效值=%q 错误=%v", cfg.R18PreferredCloud, cfg.PreferredR18Cloud(), err)
			}
		})
	}
}

func TestPreferredR18CloudEditPersistsAndPreservesDocument(t *testing.T) {
	cfg := editableConfig(t)
	snapshot, rev, err := cfg.FileSnapshot()
	if err != nil || snapshot.R18PreferredCloud != "guangya" {
		t.Fatalf("旧配置的文件快照缺少默认网盘：%q %v", snapshot.R18PreferredCloud, err)
	}
	for _, provider := range []string{"115", "guangya"} {
		saved, next, err := cfg.UpdateFile(Patch{R18PreferredCloud: ptr(provider)}, rev)
		if err != nil || saved.PreferredR18Cloud() != provider || next == rev {
			t.Fatalf("优先网盘未保存：%q %v", saved.PreferredR18Cloud(), err)
		}
		rev = next
		reloaded, err := Load(cfg.ConfigPath())
		if err != nil || reloaded.R18PreferredCloud != provider || reloaded.RedisPassword != "secret-value" {
			t.Fatalf("优先网盘或既有凭据持久化错误：网盘=%q 错误=%v", reloaded.R18PreferredCloud, err)
		}
		raw, err := os.ReadFile(cfg.ConfigPath())
		if err != nil {
			t.Fatal(err)
		}
		for _, retained := range []string{"# keep header", "# keep port comment", "custom_future:", "nested: retained", "# keep unknown"} {
			if !strings.Contains(string(raw), retained) {
				t.Fatalf("网盘配置更新丢失原文档内容：%s", retained)
			}
		}
		// 不包含该字段的更新应保留已选网盘。
		saved, rev, err = cfg.UpdateFile(Patch{ServerName: ptr("服务-" + provider)}, rev)
		if err != nil || saved.R18PreferredCloud != provider {
			t.Fatalf("其它字段更新重置了网盘：%q %v", saved.R18PreferredCloud, err)
		}
	}
}

func TestPreferredR18CloudEditRejectsInvalidWithoutWriting(t *testing.T) {
	cfg := editableConfig(t)
	_, rev, err := cfg.FileSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "other", "Guangya", " 115 "} {
		_, _, err := cfg.UpdateFile(Patch{R18PreferredCloud: ptr(value), ServerName: ptr("不应保存")}, rev)
		var validation *ValidationError
		if !errors.As(err, &validation) || !strings.Contains(err.Error(), "r18_preferred_cloud") {
			t.Fatalf("非法优先网盘未产生校验错误：%q %v", value, err)
		}
		after, err := os.ReadFile(cfg.ConfigPath())
		if err != nil || string(after) != string(original) {
			t.Fatalf("非法优先网盘更新修改了文件：%q %v", value, err)
		}
	}
}

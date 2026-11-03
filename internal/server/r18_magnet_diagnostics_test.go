package server

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"emby-go/internal/cloud115"
)

func TestR18NativeMagnet503LogsSafeReasonAndStage(t *testing.T) {
	for _, tc := range []struct {
		name, code, stage, hint string
	}{
		{"needs_auth", "GuangyaAuthorizationRequired", "cache", "在网页重试该任务"},
		{"expired_cache", "MagnetCacheExpired", "", "缓存已到期"},
		{"key", "GuangyaUnavailable", "key", ".guangya.key"},
		{"ciphertext", "GuangyaUnavailable", "credentials", "恢复与数据库匹配"},
		{"token_json", "GuangyaUnavailable", "credentials", "恢复与数据库匹配"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newR18MagnetPlaybackFixture(t)
			task := f.seed(t, r18PlaybackHashA, "ready", []cloud115.File{r18PlaybackTestFile("file", "正片.mkv", 100)}, "file", false)
			switch tc.name {
			case "needs_auth", "expired_cache":
				if tc.name == "needs_auth" {
					task.Status, task.Stage = "needs_auth", "cache"
					task.Error = "private-upstream-body https://private.invalid/?token=private-access"
				} else {
					task.ExpiresAt = time.Now().Add(-time.Second).Unix()
				}
				if err := f.h.a.db.UpdateOfflineTask(task, task.Version); err != nil {
					t.Fatal(err)
				}
			case "key":
				f.h.a.guangyaClient, f.h.a.guangyaVault = nil, nil
				keyPath := filepath.Join(filepath.Dir(f.h.a.cfg.DBPath), ".guangya.key")
				if err := os.WriteFile(keyPath, []byte("损坏的测试密钥"), 0600); err != nil {
					t.Fatal(err)
				}
			case "ciphertext", "token_json":
				connection, err := f.h.a.db.CloudGuangyaConnection()
				if err != nil {
					t.Fatal(err)
				}
				secrets := []byte("private-corrupt-ciphertext")
				if tc.name == "token_json" {
					secrets, err = f.h.a.guangyaVault.SealValue(map[string]any{
						"access_token":  map[string]string{"private": "private-access"},
						"refresh_token": "private-refresh",
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := f.h.a.db.RefreshCloudGuangyaTokens(connection.AccountID, connection.Revision, secrets); err != nil {
					t.Fatal(err)
				}
				f.h.a.guangyaClient = nil
			}

			var mu sync.Mutex
			var lines []string
			prior := slog.Default()
			slog.SetDefault(slog.New(&captureHandler{mu: &mu, lines: &lines}))
			t.Cleanup(func() { slog.SetDefault(prior) })
			source := r18MagnetSourceID(r18PlaybackHashA)
			path := "/emby/Videos/" + r18ItemID(f.h.library.ID, f.h.video.ID) + "/stream?MediaSourceId=" + source + "&Static=true&playback_token=private-playback-ticket"
			headers, out := f.h.call(t, http.MethodGet, path, f.h.admin, nil, http.StatusServiceUnavailable)
			if out["ErrorCode"] != tc.code || headers.Get("Retry-After") != "3" {
				t.Fatal("503 缺少具体错误码或重试提示", out, headers)
			}
			reason, _ := out["error"].(string)
			if !strings.Contains(reason, tc.hint) {
				t.Fatal("503 原因未说明对应处理方式", reason)
			}
			mu.Lock()
			logs := strings.Join(lines, "\n")
			mu.Unlock()
			for _, want := range []string{"光鸭磁力播放失败", "method=GET", "status=503", "error_code=" + tc.code, "reason=" + reason} {
				if !strings.Contains(logs, want) {
					t.Fatalf("服务日志缺少 %q：%s", want, logs)
				}
			}
			if tc.stage != "" && !strings.Contains(logs, "stage="+tc.stage) {
				t.Fatal("服务日志未标明失败阶段", logs)
			}
			for _, secret := range []string{f.h.admin, "private-access", "private-refresh", "private-corrupt-ciphertext", "private-upstream-body", "private-playback-ticket", "guangya-secret-access", "guangya-secret-refresh", source} {
				if strings.Contains(logs, secret) || strings.Contains(reason, secret) {
					t.Fatal("播放诊断泄露凭据、请求来源或上游正文")
				}
			}
			if f.cloud.creates.Load() != 0 || f.cloud.urlCalls.Load() != 0 {
				t.Fatal("503 诊断触发了新增下载或直链获取")
			}
		})
	}
}

func TestGuangyaProviderLoadErrorPreservesMissingConnection(t *testing.T) {
	h := newGuangyaServerHarness(t)
	_, _, err := h.a.guangyaProvider()
	var loadError *guangyaProviderLoadError
	if !errors.Is(err, sql.ErrNoRows) || !errors.As(err, &loadError) || loadError.stage != "connection" {
		t.Fatal("光鸭连接装载错误丢失底层类型或阶段", err)
	}
}

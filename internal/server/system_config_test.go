package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"emby-go/internal/cache"
	"emby-go/internal/config"
	"gopkg.in/yaml.v3"
)

func newConfigTestApp(t *testing.T) (*App, *httptest.Server, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	raw, _ := yaml.Marshal(map[string]any{"db_path": filepath.Join(root, "app.db"), "redis_addr": "localhost:6379", "redis_password": "fixture-redis-secret", "server_id": "fixed-id", "disable_library_monitor": true})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(cfg, cache.NewMemory(64))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(func() { ts.Close(); app.Close() })
	embyRaw(t, ts, "POST", "/api/auth/initialize", "", `{"Username":"admin","Pw":"password-1234"}`)
	_, authRaw := embyRaw(t, ts, "POST", "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"password-1234"}`)
	var auth struct{ AccessToken string }
	if err := json.Unmarshal(authRaw, &auth); err != nil || auth.AccessToken == "" {
		t.Fatal("login failed")
	}
	return app, ts, auth.AccessToken
}

type configTestResponse struct {
	Revision      string         `json:"revision"`
	RestartFields []string       `json:"restart_fields"`
	Config        map[string]any `json:"config"`
	Effective     map[string]any `json:"effective"`
}

func configRequest(t *testing.T, ts *httptest.Server, token, method, body string, status int) configTestResponse {
	t.Helper()
	resp, raw := embyRaw(t, ts, method, "/api/admin/config", token, body)
	if resp.StatusCode != status {
		t.Fatalf("status %d wanted %d: %s", resp.StatusCode, status, raw)
	}
	if strings.Contains(string(raw), "fixture-redis-secret") || strings.Contains(string(raw), `"redis_password":`) {
		t.Fatal("secret exposed")
	}
	var result configTestResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func configBody(rev string, values map[string]any) string {
	body, _ := json.Marshal(map[string]any{"revision": rev, "config": values})
	return string(body)
}

func TestSystemConfigSaveHotAndRestartFields(t *testing.T) {
	app, ts, token := newConfigTestApp(t)
	state := configRequest(t, ts, token, "GET", "", 200)
	before := app.configSnapshot()
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{
		"server_name": "在线更新", "port": 18081, "actors_dir": "new-actors", "redis_password": "",
		"server_domains":        []map[string]string{{"name": "内网", "url": "http://localhost:18081/emby"}},
		"probe_timeout_seconds": 45, "probe_concurrency": 3, "ffprobe_path": "custom-ffprobe",
	}), 200)
	if !slices.Contains(state.RestartFields, "port") || !slices.Contains(state.RestartFields, "actors_dir") || slices.Contains(state.RestartFields, "redis_password") {
		t.Fatalf("restart fields: %v", state.RestartFields)
	}
	if app.configSnapshot().Port != before.Port || app.actorAssets().Dir != before.ActorsDir || app.configSnapshot().ProbeWorkers() != 3 || app.configSnapshot().FFProbePath != "custom-ffprobe" {
		t.Fatal("incorrect runtime state")
	}
	_, raw := embyRaw(t, ts, "GET", "/System/Info/Public", "", "")
	if !strings.Contains(string(raw), "在线更新") || !strings.Contains(string(raw), "fixed-id") {
		t.Fatalf("name not applied: %s", raw)
	}
	_, raw = embyRaw(t, ts, "GET", "/System/Ext/ServerDomains", token, "")
	if !strings.Contains(string(raw), "localhost:18081/emby") {
		t.Fatal("domains not applied")
	}
	state = configRequest(t, ts, token, "GET", "", 200)
	if state.Config["port"] != float64(18081) || state.Effective["port"] != float64(before.Port) {
		t.Fatal("saved/effective distinction lost")
	}
	loaded, err := config.Load(before.ConfigPath())
	if err != nil || loaded.Port != 18081 || loaded.RedisPassword != before.RedisPassword {
		t.Fatal("disk persistence incorrect")
	}
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"port": before.Port, "actors_dir": ""}), 200)
	if len(state.RestartFields) != 0 {
		t.Fatalf("revert should clear pending restart: %v", state.RestartFields)
	}
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"clear_redis_password": true}), 200)
	if !slices.Contains(state.RestartFields, "redis_password") || state.Config["redis_password_configured"] != false || state.Effective["redis_password_configured"] != true {
		t.Fatal("password clear boundary incorrect")
	}
}

func TestSystemConfigAListTokenIsPrivatePersistentAndLive(t *testing.T) {
	app, ts, token := newConfigTestApp(t)
	state := configRequest(t, ts, token, "GET", "", 200)
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"alist_url": "http://alist.test/proxy", "alist_token": "alist-fixture-secret"}), 200)
	if state.Config["alist_token_configured"] != true || state.Effective["alist_token_configured"] != true || len(state.RestartFields) != 0 {
		t.Fatal("AList not applied live")
	}
	for _, endpoint := range []string{"/api/admin/config", "/api/admin/settings"} {
		_, raw := embyRaw(t, ts, "GET", endpoint, token, "")
		if strings.Contains(string(raw), "alist-fixture-secret") || strings.Contains(string(raw), `"alist_token":`) {
			t.Fatal("token exposed")
		}
	}
	loaded, err := config.Load(app.configSnapshot().ConfigPath())
	if err != nil || loaded.AListToken != "alist-fixture-secret" {
		t.Fatal("token not persisted")
	}
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"alist_token": ""}), 200)
	if app.configSnapshot().AListToken != "alist-fixture-secret" {
		t.Fatal("blank token cleared secret")
	}
	configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"alist_url": "http://user:pass@alist.test"}), 400)
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"clear_alist_token": true}), 200)
	if app.configSnapshot().AListToken != "" || state.Config["alist_token_configured"] != false {
		t.Fatal("token not cleared")
	}
}

func TestSystemConfigRejectsUnauthenticatedInvalidConflictAndWriteFailure(t *testing.T) {
	app, ts, token := newConfigTestApp(t)
	configRequest(t, ts, "", "GET", "", 401)
	configRequest(t, ts, "", "PUT", `{}`, 401)
	state := configRequest(t, ts, token, "GET", "", 200)
	configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"port": 99999, "server_name": "must-not-apply"}), 400)
	configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"server_id": "forbidden"}), 400)
	configRequest(t, ts, token, "PUT", configBody("stale", map[string]any{"server_name": "must-not-apply"}), 409)
	configRequest(t, ts, token, "PUT", `{} {}`, 400)
	if app.currentServerName() != "Emby-go" {
		t.Fatal("failed request modified runtime")
	}
	// Losing the writable source must not turn the request into a runtime-only update.
	if err := os.Remove(app.configSnapshot().ConfigPath()); err != nil {
		t.Fatal(err)
	}
	configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"server_name": "must-not-apply"}), 500)
	if app.currentServerName() != "Emby-go" {
		t.Fatal("failed persistence modified runtime")
	}
}

func TestSystemConfigRebuildsMonitors(t *testing.T) {
	app, ts, token := newConfigTestApp(t)
	library, err := app.db.AddLibrary("test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := configRequest(t, ts, token, "GET", "", 200)
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"disable_library_monitor": false, "library_monitor_mode": "realtime"}), 200)
	app.watchMu.Lock()
	first := app.monitors[library.ID]
	app.watchMu.Unlock()
	if first == nil {
		t.Fatal("enabling did not create monitor")
	}
	state = configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"library_monitor_mode": "polling"}), 200)
	app.watchMu.Lock()
	second := app.monitors[library.ID]
	app.watchMu.Unlock()
	if second == nil || first == second || app.configSnapshot().MonitorMode() != "polling" {
		t.Fatal("mode change did not rebuild monitor")
	}
	configRequest(t, ts, token, "PUT", configBody(state.Revision, map[string]any{"disable_library_monitor": true}), 200)
	app.watchMu.Lock()
	defer app.watchMu.Unlock()
	if len(app.monitors) != 0 {
		t.Fatal("disabling left monitors running")
	}
}

func TestSystemConfigConcurrentSaveOnlyOneWins(t *testing.T) {
	_, ts, token := newConfigTestApp(t)
	state := configRequest(t, ts, token, "GET", "", 200)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			req, _ := http.NewRequest("PUT", ts.URL+"/api/admin/config", strings.NewReader(configBody(state.Revision, map[string]any{"server_name": name})))
			req.Header.Set("X-Emby-Token", token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				codes <- 0
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}(name)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent saves: %v", counts)
	}
}

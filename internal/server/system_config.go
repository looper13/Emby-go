package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"

	"emby-go/internal/config"
	"github.com/gin-gonic/gin"
)

func (a *App) configSnapshot() config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	cfg := a.cfg
	cfg.ServerDomains = append([]config.ServerDomain{}, cfg.ServerDomains...)
	return cfg
}

func (a *App) currentServerName() string {
	name := a.configSnapshot().ServerName
	if name == "" {
		return "Emby-go"
	}
	return name
}

// Never serialize Config directly: credentials belong only in the YAML file.
func publicConfig(cfg config.Config) gin.H {
	domains := append([]config.ServerDomain{}, cfg.ServerDomains...)
	return gin.H{
		"port": cfg.Port, "debug": cfg.Debug, "server_name": cfg.ServerName,
		"disable_library_monitor": cfg.DisableLibraryMonitor, "library_monitor_mode": cfg.MonitorMode(),
		"db_path": cfg.DBPath, "actors_dir": cfg.ActorsDir,
		"redis_addr": cfg.RedisAddr, "redis_db": cfg.RedisDB, "redis_password_configured": cfg.RedisPassword != "",
		"ffprobe_path": cfg.FFProbePath, "probe_timeout_seconds": int(cfg.ProbeTimeout().Seconds()),
		"probe_concurrency": cfg.ProbeWorkers(), "server_domains": domains,
	}
}

var restartConfigFields = []string{"port", "debug", "db_path", "actors_dir", "redis_addr", "redis_db", "redis_password"}

func pendingConfigFields(saved, running config.Config) []string {
	// Compare resolved resources so an unchanged relative path does not require a restart.
	_ = saved.ResolveStorage()
	_ = running.ResolveStorage()
	left, right := publicConfig(saved), publicConfig(running)
	left["redis_password"], right["redis_password"] = saved.RedisPassword, running.RedisPassword
	fields := []string{}
	for _, key := range restartConfigFields {
		if !reflect.DeepEqual(left[key], right[key]) {
			fields = append(fields, key)
		}
	}
	return fields
}

func (a *App) configResponse(saved config.Config, revision string, warnings []string) gin.H {
	running := a.configSnapshot()
	if warnings == nil {
		warnings = []string{}
	}
	return gin.H{
		"config_path": running.ConfigPath(), "writable": running.ConfigPath() != "", "revision": revision,
		"config": publicConfig(saved), "effective": publicConfig(running), "server_id": a.serverID,
		"restart_fields": pendingConfigFields(saved, running), "warnings": warnings,
	}
}

func (a *App) adminConfig(c *gin.Context) {
	a.configEditMu.Lock()
	defer a.configEditMu.Unlock()
	running := a.configSnapshot()
	saved, rev, err := running.FileSnapshot()
	if errors.Is(err, config.ErrNoConfigFile) {
		saved, err = running, nil
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取配置失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, a.configResponse(saved, rev, a.configWarnings))
}

func (a *App) adminSaveConfig(c *gin.Context) {
	var request struct {
		Revision string       `json:"revision"`
		Config   config.Patch `json:"config"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置请求格式不正确或包含未知字段"})
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置请求只能包含一个 JSON 对象"})
		return
	}
	a.configEditMu.Lock()
	defer a.configEditMu.Unlock()
	before := a.configSnapshot()
	saved, rev, err := before.UpdateFile(request.Config, request.Revision)
	if err != nil {
		status := http.StatusInternalServerError
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, config.ErrConfigConflict) || errors.Is(err, config.ErrNoConfigFile) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": "配置未保存：" + err.Error()})
		return
	}
	// Only replace fields whose consumers support runtime changes.
	a.cfgMu.Lock()
	a.cfg.ServerName = saved.ServerName
	a.cfg.ServerDomains = append([]config.ServerDomain{}, saved.ServerDomains...)
	a.cfg.DisableLibraryMonitor, a.cfg.LibraryMonitorMode = saved.DisableLibraryMonitor, saved.MonitorMode()
	a.cfg.FFProbePath, a.cfg.ProbeTimeoutSec, a.cfg.ProbeConcurrency = saved.FFProbePath, saved.ProbeTimeoutSec, saved.ProbeConcurrency
	a.cfgMu.Unlock()
	if before.DisableLibraryMonitor != saved.DisableLibraryMonitor || before.MonitorMode() != saved.MonitorMode() {
		a.closeLibraryMonitors()
		a.configWarnings = a.startLibraryMonitoring()
	}
	c.JSON(http.StatusOK, a.configResponse(saved, rev, a.configWarnings))
}

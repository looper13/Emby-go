package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerDomain 备用线路候选。JSON 字段名必须是小写 name/url，
// 与真实 Emby /System/Ext/ServerDomains 的返回一致（客户端按小写键解析）。
type ServerDomain struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
}

type Config struct {
	DisableLibraryMonitor bool   `yaml:"disable_library_monitor"`
	LibraryMonitorMode    string `yaml:"library_monitor_mode"`

	Port          int            `yaml:"port"` // 监听端口（如 18080）
	DBPath        string         `yaml:"db_path"`
	ActorsDir     string         `yaml:"actors_dir"`
	ServerName    string         `yaml:"server_name"` // 对外站点名（System/Info 的 ServerName）
	ServerID      string         `yaml:"server_id"`   // Emby ServerId；留空则首次启动生成稳定 UUID 存 DB 并回写
	Debug         bool           `yaml:"debug"`       // 开启后启用 gin 调试模式与详细请求日志
	RedisAddr     string         `yaml:"redis_addr"`
	RedisPassword string         `yaml:"redis_password"`
	TMDBAPIKey    string         `yaml:"tmdb_api_key" json:"-"`
	RedisDB       int            `yaml:"redis_db"`
	ServerDomains []ServerDomain `yaml:"server_domains"`

	// 媒体信息探测（独立于扫库）：调用系统 ffprobe 读取真实技术参数并写回 NFO。
	FFProbePath         string `yaml:"ffprobe_path"`          // ffprobe 可执行文件路径；留空则从 PATH 查找
	ProbeTimeoutSec     int    `yaml:"probe_timeout_seconds"` // 单条探测超时（秒），默认 90
	ProbeConcurrency    int    `yaml:"probe_concurrency"`     // 并发探测数，默认 2
	FFmpegEnabled       bool   `yaml:"ffmpeg_enabled"`
	FFmpegPath          string `yaml:"ffmpeg_path"`
	PlaybackConcurrency int    `yaml:"playback_concurrency"` // 默认 2，上限 4
	PlaybackCacheMB     int    `yaml:"playback_cache_mb"`    // 每个播放会话的临时磁盘上限，默认 2048 MiB

	path string // 配置文件来源路径（非 yaml 字段），供写回使用
}

// 探测相关默认值：超时给足远程直链建连+读取的时间；并发压低避免把网盘/代理打爆。
const (
	defaultProbeTimeoutSec  = 90
	defaultProbeConcurrency = 2
)

// ProbeTimeout 返回单条探测超时。
func (c Config) ProbeTimeout() time.Duration {
	seconds := c.ProbeTimeoutSec
	if seconds <= 0 {
		seconds = defaultProbeTimeoutSec
	}
	return time.Duration(seconds) * time.Second
}

// ProbeWorkers 返回并发探测协程数，上限 8（再高对远程源没有收益，只会互相拖慢）。
func (c Config) ProbeWorkers() int {
	workers := c.ProbeConcurrency
	if workers <= 0 {
		return defaultProbeConcurrency
	}
	if workers > 8 {
		return 8
	}
	return workers
}

// Addr 返回 http 监听地址（":"+port；默认 18080）。
func (c Config) Addr() string {
	if c.Port <= 0 {
		return ":18080"
	}
	return fmt.Sprintf(":%d", c.Port)
}

func (cfg Config) MonitorMode() string {
	if cfg.LibraryMonitorMode == "" {
		return "realtime"
	}
	return cfg.LibraryMonitorMode
}

// Redis 为必选缓存后端：服务启动时即连接并 Ping，连不上直接拒绝启动。
func Load(path string) (Config, error) {
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return Config{}, err
		}
		path = absolute
	}
	cfg := Config{Port: 18080, DBPath: "emby-go.db", ServerName: "Emby-go", LibraryMonitorMode: "realtime", path: path}
	if path == "" {
		if err := cfg.loadTMDBDefault(); err != nil {
			return cfg, err
		}
		return cfg, cfg.ResolveStorage()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Port <= 0 {
		cfg.Port = 18080
	}
	cfg.LibraryMonitorMode = cfg.MonitorMode()
	if cfg.LibraryMonitorMode != "realtime" && cfg.LibraryMonitorMode != "polling" {
		return cfg, fmt.Errorf("library_monitor_mode 必须为 realtime 或 polling，当前为 %q", cfg.LibraryMonitorMode)
	}
	cfg.path = path
	if err := cfg.loadTMDBDefault(); err != nil {
		return cfg, err
	}
	return cfg, cfg.ResolveStorage()
}

func (c *Config) loadTMDBDefault() error {
	if key := strings.TrimSpace(os.Getenv("TMDB_API_KEY")); key != "" {
		c.TMDBAPIKey = key
		return nil
	}
	if strings.TrimSpace(c.TMDBAPIKey) != "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(c.path), "anime-tmdb.local.yaml"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read local TMDB configuration")
	}
	var local struct {
		APIKey string `yaml:"tmdb_api_key"`
	}
	if len(data) > 8192 || yaml.Unmarshal(data, &local) != nil {
		return fmt.Errorf("invalid local TMDB configuration")
	}
	c.TMDBAPIKey = strings.TrimSpace(local.APIKey)
	return nil
}

// ResolveStorage anchors relative actor storage to the database, never to the config file.
func (c *Config) ResolveStorage() error {
	if strings.TrimSpace(c.DBPath) == "" {
		c.DBPath = "emby-go.db"
	}
	db, err := filepath.Abs(c.DBPath)
	if err != nil {
		return err
	}
	c.DBPath = db
	dir := strings.TrimSpace(c.ActorsDir)
	if dir == "" {
		dir = "actors"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(db), dir)
	}
	c.ActorsDir = filepath.Clean(dir)
	return nil
}

// PersistServerID 把自动生成的 ServerId 原子写回 YAML，保留注释与其它字段。
// 无配置文件来源（path 为空）或写失败时返回错误，由调用方决定是否忽略。
func (c Config) PersistServerID(serverID string) error {
	if c.path == "" {
		return nil
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	doc, _, _, err := c.readDocument()
	if err != nil {
		return err
	}
	if err := setNode(doc, "server_id", serverID); err != nil {
		return err
	}
	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "server_id" {
			root.Content[i+1].Style = yaml.DoubleQuotedStyle
		}
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return c.replaceFile(data)
}

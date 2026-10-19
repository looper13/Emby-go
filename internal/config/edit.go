package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	ErrNoConfigFile   = errors.New("未指定配置文件，无法保存；请使用 -c 指定配置文件后启动")
	ErrConfigConflict = errors.New("配置文件已被其他页面或程序修改，请重新加载后再保存")
	fileMu            sync.Mutex
)

// Patch only permits editable fields. Server identity and the destination file
// cannot be changed through the administration API. Nil means leave unchanged.
type Patch struct {
	Port                  *int            `json:"port" yaml:"port"`
	Debug                 *bool           `json:"debug" yaml:"debug"`
	ServerName            *string         `json:"server_name" yaml:"server_name"`
	DisableLibraryMonitor *bool           `json:"disable_library_monitor" yaml:"disable_library_monitor"`
	LibraryMonitorMode    *string         `json:"library_monitor_mode" yaml:"library_monitor_mode"`
	DBPath                *string         `json:"db_path" yaml:"db_path"`
	ActorsDir             *string         `json:"actors_dir" yaml:"actors_dir"`
	RedisAddr             *string         `json:"redis_addr" yaml:"redis_addr"`
	RedisPassword         *string         `json:"redis_password" yaml:"redis_password"`
	ClearRedisPassword    bool            `json:"clear_redis_password" yaml:"-"`
	RedisDB               *int            `json:"redis_db" yaml:"redis_db"`
	FFProbePath           *string         `json:"ffprobe_path" yaml:"ffprobe_path"`
	FFmpegEnabled         *bool           `json:"ffmpeg_enabled" yaml:"ffmpeg_enabled"`
	FFmpegPath            *string         `json:"ffmpeg_path" yaml:"ffmpeg_path"`
	PlaybackConcurrency   *int            `json:"playback_concurrency" yaml:"playback_concurrency"`
	PlaybackCacheMB       *int            `json:"playback_cache_mb" yaml:"playback_cache_mb"`
	ProbeTimeoutSec       *int            `json:"probe_timeout_seconds" yaml:"probe_timeout_seconds"`
	ProbeConcurrency      *int            `json:"probe_concurrency" yaml:"probe_concurrency"`
	ServerDomains         *[]ServerDomain `json:"server_domains" yaml:"server_domains"`
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{Message: message} }

func (c Config) ConfigPath() string { return c.path }

// FileSnapshot returns editable values as written, retaining relative paths.
// The revision detects stale forms without returning secret material.
func (c Config) FileSnapshot() (Config, string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	_, cfg, data, err := c.readDocument()
	if err != nil {
		return Config{}, "", err
	}
	return cfg, revision(data), nil
}

func revision(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func (c Config) readDocument() (*yaml.Node, Config, []byte, error) {
	if c.path == "" {
		return nil, Config{}, nil, ErrNoConfigFile
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, Config{}, nil, err
	}
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	err = decoder.Decode(&doc)
	if err != nil && err != io.EOF {
		return nil, Config{}, nil, errors.New("配置文件 YAML 格式不正确")
	}
	var extra yaml.Node
	if err == nil && decoder.Decode(&extra) != io.EOF {
		return nil, Config{}, nil, errors.New("配置文件只能包含一个 YAML 文档")
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, Config{}, nil, errors.New("配置文件必须为 YAML 键值映射")
	}
	cfg, err := decodeDocument(&doc, c.path)
	return &doc, cfg, data, err
}

func decodeDocument(doc *yaml.Node, path string) (Config, error) {
	cfg := Config{Port: 18080, DBPath: "emby-go.db", ServerName: "Emby-go", LibraryMonitorMode: "realtime", path: path}
	if err := doc.Decode(&cfg); err != nil {
		return Config{}, errors.New("配置文件字段格式不正确")
	}
	return cfg, nil
}

func setNode(doc *yaml.Node, key string, value any) error {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return err
	}
	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			old := root.Content[i+1]
			node.HeadComment, node.LineComment, node.FootComment = old.HeadComment, old.LineComment, old.FootComment
			if old.Kind == node.Kind && old.Tag == node.Tag {
				node.Style = old.Style
			}
			// Keep alias targets connected when an edited field owns an anchor.
			node.Anchor = old.Anchor
			*old = node
			return nil
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &node)
	return nil
}

// UpdateFile validates the complete candidate and only publishes it once the
// temporary file is flushed. A failed write never changes the runtime config.
func (c Config) UpdateFile(patch Patch, expectedRevision string) (Config, string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	doc, _, original, err := c.readDocument()
	if err != nil {
		return Config{}, "", err
	}
	if expectedRevision == "" || expectedRevision != revision(original) {
		return Config{}, "", ErrConfigConflict
	}
	if patch.ClearRedisPassword && patch.RedisPassword != nil && *patch.RedisPassword != "" {
		return Config{}, "", invalid("不能同时设置和清除 Redis 密码")
	}
	if patch.RedisPassword != nil && *patch.RedisPassword == "" {
		patch.RedisPassword = nil
	}
	if patch.ClearRedisPassword {
		empty := ""
		patch.RedisPassword = &empty
	}
	v := reflect.ValueOf(patch)
	for i := 0; i < v.NumField(); i++ {
		key := v.Type().Field(i).Tag.Get("yaml")
		if key == "-" || v.Field(i).Kind() != reflect.Pointer || v.Field(i).IsNil() {
			continue
		}
		value := v.Field(i).Elem().Interface()
		if err := setNode(doc, key, value); err != nil {
			return Config{}, "", err
		}
	}
	candidate, err := decodeDocument(doc, c.path)
	if err != nil {
		return Config{}, "", err
	}
	if err := candidate.validateEdit(); err != nil {
		return Config{}, "", err
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return Config{}, "", err
	}
	var check Config
	if err := yaml.Unmarshal(data, &check); err != nil {
		return Config{}, "", errors.New("更新后的 YAML 无法重新解析，原配置未修改")
	}
	// Detect external edits that happened while the candidate was prepared.
	latest, err := os.ReadFile(c.path)
	if err != nil {
		return Config{}, "", err
	}
	if !bytes.Equal(original, latest) {
		return Config{}, "", ErrConfigConflict
	}
	if err := c.replaceFile(data); err != nil {
		return Config{}, "", err
	}
	return candidate, revision(data), nil
}

func (c Config) validateEdit() error {
	if c.Port < 1 || c.Port > 65535 {
		return invalid("端口必须为 1–65535")
	}
	if strings.TrimSpace(c.ServerName) == "" || len(c.ServerName) > 200 {
		return invalid("服务名称不能为空且不能超过 200 字节")
	}
	if c.LibraryMonitorMode != "realtime" && c.LibraryMonitorMode != "polling" {
		return invalid("监控模式必须为 realtime 或 polling")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return invalid("数据库路径不能为空")
	}
	for _, path := range []string{c.DBPath, c.ActorsDir, c.FFProbePath, c.FFmpegPath} {
		if strings.ContainsAny(path, "\x00\r\n") {
			return invalid("路径不能包含空字符或换行")
		}
	}
	host, port, err := net.SplitHostPort(c.RedisAddr)
	p, convErr := strconv.Atoi(port)
	if err != nil || convErr != nil || strings.TrimSpace(host) == "" || p < 1 || p > 65535 {
		return invalid("Redis 地址须为主机:端口，例如 127.0.0.1:6379")
	}
	if c.RedisDB < 0 {
		return invalid("Redis 数据库编号不能为负数")
	}
	if c.ProbeTimeoutSec < 0 || c.ProbeTimeoutSec > 3600 {
		return invalid("探测超时必须为 1–3600 秒（0 使用默认值 90）")
	}
	if c.ProbeConcurrency < 0 || c.ProbeConcurrency > 8 {
		return invalid("探测并发必须为 1–8（0 使用默认值 2）")
	}
	if c.PlaybackConcurrency < 0 || c.PlaybackConcurrency > 4 {
		return invalid("播放处理并发必须为 1–4（0 使用默认值 2）")
	}
	if c.PlaybackCacheMB != 0 && (c.PlaybackCacheMB < 64 || c.PlaybackCacheMB > 65536) {
		return invalid("播放缓存须为 64–65536 MiB（0 使用默认值 2048）")
	}
	if len(c.ServerDomains) > 32 {
		return invalid("备用域名最多 32 条")
	}
	for _, domain := range c.ServerDomains {
		u, err := url.Parse(domain.URL)
		if strings.TrimSpace(domain.Name) == "" || len(domain.Name) > 200 || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			return invalid("备用域名须填写名称和有效的 HTTP/HTTPS 地址，不能包含账号密码或片段")
		}
	}
	return nil
}

func (c Config) replaceFile(data []byte) error {
	// Replace the target of a symbolic link instead of replacing the link itself.
	target, err := filepath.EvalSymlinks(c.path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("配置路径不是普通文件")
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(f.Name(), target)
}

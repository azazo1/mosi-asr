// Package config 负责 mosi-asr 的配置文件定位, 解析, 校验与自动迁移.
//
// 配置文件默认位于 ~/.config/mosi-asr/config.toml, 可以通过 MOSS_ASR_CONFIG
// 环境变量或 --config 参数覆盖. 配置内始终带有 version 字段, 供新版程序对旧
// 配置进行自动迁移, 迁移逻辑集中在 migrate.go.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/azazo1/mosi-asr/internal/mossapi"
)

// CurrentVersion 是当前程序使用的配置版本号.
// 配置结构出现不兼容变化时递增, 并在 migrate.go 中登记对应迁移步骤.
const CurrentVersion = 1

// 默认值常量.
const (
	DefaultBaseURL      = "https://api.mosi.cn"
	DefaultModel        = "moss-transcribe-1.0"
	DefaultDiarizeModel = "moss-transcribe-diarize-pro"
	DefaultAPIKeyEnv    = "MOSS_API_KEY"

	// DirName 是配置目录名, FileName 是配置文件名.
	DirName  = "mosi-asr"
	FileName = "config.toml"
)

// Config 是 mosi-asr 的完整配置.
type Config struct {
	Version    int        `toml:"version"`
	API        API        `toml:"api"`
	Transcribe Transcribe `toml:"transcribe"`
	Output     Output     `toml:"output"`
	Log        Log        `toml:"log"`
}

// API 描述服务端连接参数.
type API struct {
	BaseURL        string `toml:"base_url"`
	APIKey         string `toml:"api_key"`
	APIKeyEnv      string `toml:"api_key_env"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
	MaxRetries     int    `toml:"max_retries"`
}

// Transcribe 描述转写默认行为.
type Transcribe struct {
	Model             string   `toml:"model"`
	DiarizeModel      string   `toml:"diarize_model"`
	Diarize           bool     `toml:"diarize"`
	Stream            bool     `toml:"stream"`
	Async             bool     `toml:"async"`
	AutoAsyncOverMB   int      `toml:"auto_async_over_mb"`
	PollIntervalSec   int      `toml:"poll_interval_seconds"`
	PollTimeoutSec    int      `toml:"poll_timeout_seconds"`
	Keyterms          []string `toml:"keyterms"`
	MergeGapSeconds   float64  `toml:"merge_gap_seconds"`
	IncludeTimestamps bool     `toml:"include_timestamps"`
	SpeakerNamePrefix bool     `toml:"speaker_name_prefix"`
}

// Output 描述结果输出行为.
type Output struct {
	Format        string `toml:"format"`
	Dir           string `toml:"dir"`
	OverwriteFile bool   `toml:"overwrite"`
}

// Log 描述日志行为.
type Log struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// Default 返回一份带全部默认值的配置, 供首次生成配置文件和加载时补全使用.
func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		API: API{
			BaseURL:        DefaultBaseURL,
			APIKeyEnv:      DefaultAPIKeyEnv,
			TimeoutSeconds: 600,
			MaxRetries:     2,
		},
		Transcribe: Transcribe{
			Model:             DefaultModel,
			DiarizeModel:      DefaultDiarizeModel,
			Diarize:           false,
			Stream:            false,
			Async:             false,
			AutoAsyncOverMB:   32,
			PollIntervalSec:   3,
			PollTimeoutSec:    3600,
			Keyterms:          []string{},
			MergeGapSeconds:   1.2,
			SpeakerNamePrefix: true,
		},
		Output: Output{
			Format:        "txt",
			Dir:           "",
			OverwriteFile: true,
		},
		Log: Log{
			Level: "info",
			File:  "",
		},
	}
}

// DefaultPath 返回默认配置路径, 优先使用 XDG_CONFIG_HOME.
func DefaultPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// ConfigDir 返回配置目录, 优先使用 XDG_CONFIG_HOME.
func ConfigDir() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(ExpandHome(xdg), DirName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定用户主目录: %w", err)
	}
	return filepath.Join(home, ".config", DirName), nil
}

// ExpandHome 把开头的 ~ 展开为用户主目录.
func ExpandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// LoadResult 描述一次配置加载的结果.
type LoadResult struct {
	Config *Config
	Path   string
	// Exists 表示配置文件是否真实存在于磁盘.
	Exists bool
	// Migrated 表示本次加载过程中是否执行了配置迁移.
	Migrated bool
	// MigratedFrom 是迁移前的版本号, 未迁移时为 0.
	MigratedFrom int
	// UnknownFields 记录配置里出现但程序不认识的字段, 通常是拼写错误.
	UnknownFields []string
}

// Load 读取配置文件.
//
// path 为空时按 MOSS_ASR_CONFIG 环境变量, 再到默认路径依次查找. 文件不存在
// 不算错误: 返回默认配置并标记 Exists=false, 由调用方决定是否提示用户初始化.
// 读到的配置会补齐默认值, 并自动迁移到 CurrentVersion.
func Load(path string) (*LoadResult, error) {
	resolved, explicit, err := ResolvePath(path)
	if err != nil {
		return nil, err
	}

	cfg := Default()
	res := &LoadResult{Config: cfg, Path: resolved}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return res, nil
		}
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", resolved, err)
	}
	res.Exists = true

	meta, err := toml.Decode(string(raw), cfg)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", resolved, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		// 未知字段不致命, 但需要提示, 便于发现拼写错误.
		res.UnknownFields = make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			res.UnknownFields = append(res.UnknownFields, k.String())
		}
	}

	// 没有 version 字段的配置文件视为版本 0, 交给迁移逻辑升级.
	sourceVersion := cfg.Version
	if !meta.IsDefined("version") {
		sourceVersion = 0
	}
	from := sourceVersion
	cfg.Version = sourceVersion
	if err := Migrate(cfg); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置文件 %s 内容有误: %w", resolved, err)
	}
	if cfg.Version != from {
		res.Migrated = true
		res.MigratedFrom = from
	}
	return res, nil
}

// ResolvePath 决定实际使用的配置路径.
// explicit 表示路径是用户明确指定的 (参数或环境变量), 此时文件不存在应报错.
func ResolvePath(path string) (resolved string, explicit bool, err error) {
	if p := strings.TrimSpace(path); p != "" {
		abs, aerr := filepath.Abs(ExpandHome(p))
		if aerr != nil {
			return "", true, aerr
		}
		return abs, true, nil
	}
	if p := strings.TrimSpace(os.Getenv("MOSS_ASR_CONFIG")); p != "" {
		abs, aerr := filepath.Abs(ExpandHome(p))
		if aerr != nil {
			return "", true, aerr
		}
		return abs, true, nil
	}
	def, derr := DefaultPath()
	if derr != nil {
		return "", false, derr
	}
	return def, false, nil
}

// Save 把配置写回磁盘, 必要时创建父目录.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(Render(cfg)), 0o600); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}

// NewTemplate 返回用于初始化的配置模板, api_key 留空等待用户填写.
func NewTemplate() *Config {
	cfg := Default()
	return cfg
}

// ResolveAPIKey 按 api_key 字段, 专用环境变量, 再到 MOSS_API_KEY 的顺序取 Key.
func (c *Config) ResolveAPIKey() (string, string) {
	if k := strings.TrimSpace(c.API.APIKey); k != "" {
		return k, "配置文件 api.api_key"
	}
	if name := strings.TrimSpace(c.API.APIKeyEnv); name != "" {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, "环境变量 " + name
		}
	}
	if v := strings.TrimSpace(os.Getenv(DefaultAPIKeyEnv)); v != "" {
		return v, "环境变量 " + DefaultAPIKeyEnv
	}
	return "", ""
}

// applyDefaults 把零值字段补成默认值, 保证缺失字段不会导致异常行为.
func (c *Config) applyDefaults() {
	d := Default()
	if c.API.BaseURL == "" {
		c.API.BaseURL = d.API.BaseURL
	}
	if c.API.APIKeyEnv == "" {
		c.API.APIKeyEnv = d.API.APIKeyEnv
	}
	if c.API.TimeoutSeconds <= 0 {
		c.API.TimeoutSeconds = d.API.TimeoutSeconds
	}
	if c.API.MaxRetries < 0 {
		c.API.MaxRetries = d.API.MaxRetries
	}
	if c.Transcribe.Model == "" {
		c.Transcribe.Model = d.Transcribe.Model
	}
	if c.Transcribe.DiarizeModel == "" {
		c.Transcribe.DiarizeModel = d.Transcribe.DiarizeModel
	}
	if c.Transcribe.PollIntervalSec <= 0 {
		c.Transcribe.PollIntervalSec = d.Transcribe.PollIntervalSec
	}
	if c.Transcribe.PollTimeoutSec <= 0 {
		c.Transcribe.PollTimeoutSec = d.Transcribe.PollTimeoutSec
	}
	if c.Transcribe.AutoAsyncOverMB <= 0 {
		c.Transcribe.AutoAsyncOverMB = d.Transcribe.AutoAsyncOverMB
	}
	if c.Transcribe.MergeGapSeconds <= 0 {
		c.Transcribe.MergeGapSeconds = d.Transcribe.MergeGapSeconds
	}
	if c.Output.Format == "" {
		c.Output.Format = d.Output.Format
	}
	if c.Log.Level == "" {
		c.Log.Level = d.Log.Level
	}
	c.API.BaseURL = strings.TrimRight(c.API.BaseURL, "/")
	if c.Output.Dir != "" {
		c.Output.Dir = ExpandHome(c.Output.Dir)
	}
	c.Log.File = ExpandHome(c.Log.File)
}

// Validate 校验关键字段取值.
func (c *Config) Validate() error {
	if c.Version > CurrentVersion {
		return fmt.Errorf("配置版本 %d 高于当前程序支持的版本 %d, 请升级 mosi-asr", c.Version, CurrentVersion)
	}
	if c.API.BaseURL == "" {
		return fmt.Errorf("api.base_url 不能为空")
	}
	if !strings.HasPrefix(c.API.BaseURL, "http://") && !strings.HasPrefix(c.API.BaseURL, "https://") {
		return fmt.Errorf("api.base_url 必须以 http:// 或 https:// 开头, 当前为 %q", c.API.BaseURL)
	}
	if _, ok := ValidFormats[c.Output.Format]; !ok {
		return fmt.Errorf("output.format 取值 %q 不支持, 可选: %s", c.Output.Format, FormatList())
	}
	if len(c.Transcribe.Keyterms) > mossapi.MaxKeyterms {
		return fmt.Errorf("transcribe.keyterms 最多 %d 个, 当前 %d 个", mossapi.MaxKeyterms, len(c.Transcribe.Keyterms))
	}
	for _, k := range c.Transcribe.Keyterms {
		if len([]rune(k)) > mossapi.MaxKeytermRunes {
			return fmt.Errorf("热词 %q 超过 %d 个字符", k, mossapi.MaxKeytermRunes)
		}
	}
	if !ValidLogLevels[strings.ToLower(c.Log.Level)] {
		return fmt.Errorf("log.level 取值 %q 不支持, 可选: debug, info, warn, error", c.Log.Level)
	}
	return nil
}

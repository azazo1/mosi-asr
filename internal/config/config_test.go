package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig 在临时目录写入配置文件并返回路径.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	return path
}

func TestLoadMigratesLegacyConfig(t *testing.T) {
	// 早期配置没有 version 字段, 加载时应自动补到当前版本.
	path := writeConfig(t, `
[api]
base_url = "https://example.com"
api_key = "legacy-key"
`)
	res, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if !res.Migrated || res.MigratedFrom != 0 {
		t.Fatalf("迁移状态异常: migrated=%v from=%d", res.Migrated, res.MigratedFrom)
	}
	if res.Config.Version != CurrentVersion {
		t.Fatalf("版本未迁移到当前版本: %d", res.Config.Version)
	}
	if res.Config.API.BaseURL != "https://example.com" {
		t.Errorf("已有配置项被覆盖: %s", res.Config.API.BaseURL)
	}
	if res.Config.Transcribe.Model != DefaultModel {
		t.Errorf("缺失字段未补默认值: %s", res.Config.Transcribe.Model)
	}
}

func TestLoadRejectsNewerVersion(t *testing.T) {
	path := writeConfig(t, "version = 999\n")
	if _, err := Load(path); err == nil {
		t.Fatal("更高版本的配置应当报错")
	}
}

func TestLoadReportsUnknownFields(t *testing.T) {
	path := writeConfig(t, "version = 1\n\n[transcribe]\nmodel_type = \"x\"\n")
	res, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if len(res.UnknownFields) != 1 || !strings.Contains(res.UnknownFields[0], "model_type") {
		t.Fatalf("未知字段未识别: %v", res.UnknownFields)
	}
}

func TestLoadValidatesValues(t *testing.T) {
	path := writeConfig(t, "version = 1\n\n[output]\nformat = \"pdf\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("不支持输出格式时应当报错")
	}

	path = writeConfig(t, "version = 1\n\n[transcribe]\nkeyterms = [\"a\", \"b\", \"c\", \"d\", \"e\", \"f\", \"g\", \"h\", \"i\", \"j\", \"k\", \"l\", \"m\", \"n\", \"o\", \"p\", \"q\", \"r\", \"s\", \"t\", \"u\"]\n")
	if _, err := Load(path); err == nil {
		t.Fatal("热词超出上限时应当报错")
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOSS_ASR_CONFIG", "")
	res, err := Load("")
	if err != nil {
		t.Fatalf("缺少配置文件时不应报错: %v", err)
	}
	if res.Exists {
		t.Fatal("配置文件不应存在")
	}
	if res.Config.Version != CurrentVersion || res.Config.API.BaseURL != DefaultBaseURL {
		t.Fatalf("默认值异常: %+v", res.Config)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	cfg := NewTemplate()
	cfg.API.APIKey = "round-trip-key"
	cfg.Transcribe.Diarize = true
	cfg.Transcribe.Keyterms = []string{"矩池云", "SGLang"}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}

	res, err := Load(path)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	if res.Config.API.APIKey != "round-trip-key" {
		t.Errorf("api_key 未保留: %q", res.Config.API.APIKey)
	}
	if !res.Config.Transcribe.Diarize {
		t.Error("diarize 未保留")
	}
	if len(res.Config.Transcribe.Keyterms) != 2 {
		t.Errorf("热词未保留: %v", res.Config.Transcribe.Keyterms)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("临时文件未清理: %s", path+".tmp")
	}
}

func TestResolveAPIKeyPrecedence(t *testing.T) {
	t.Setenv("MOSS_ASR_TEST_KEY", "env-key")
	t.Setenv("MOSS_API_KEY", "fallback-key")

	cfg := Default()
	if key, source := cfg.ResolveAPIKey(); key != "fallback-key" || !strings.Contains(source, "MOSS_API_KEY") {
		t.Fatalf("默认环境变量未生效: %q %q", key, source)
	}

	cfg.API.APIKeyEnv = "MOSS_ASR_TEST_KEY"
	if key, _ := cfg.ResolveAPIKey(); key != "env-key" {
		t.Fatalf("自定义环境变量未生效: %q", key)
	}

	cfg.API.APIKey = "inline-key"
	if key, source := cfg.ResolveAPIKey(); key != "inline-key" || !strings.Contains(source, "api_key") {
		t.Fatalf("配置文件优先规则未生效: %q %q", key, source)
	}
}

func TestRenderTemplateIsLoadable(t *testing.T) {
	path := writeConfig(t, Render(Default()))
	res, err := Load(path)
	if err != nil {
		t.Fatalf("模板无法被自身解析: %v", err)
	}
	if res.Migrated {
		t.Error("模板不应触发迁移")
	}
	if len(res.UnknownFields) > 0 {
		t.Errorf("模板包含未知字段: %v", res.UnknownFields)
	}
}

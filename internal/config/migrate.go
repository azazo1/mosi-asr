package config

import (
	"fmt"
	"log/slog"
)

// migration 描述一次配置迁移: 把版本 From 的配置就地升级到版本 To.
type migration struct {
	From int
	To   int
	Name string
	// Apply 只做结构层面的搬运, 默认值补全统一由 applyDefaults 负责.
	Apply func(cfg *Config) error
}

// migrations 按 From 升序排列, 版本 1 是首个正式版本.
//
// 新增配置版本时的步骤:
//  1. 递增 CurrentVersion;
//  2. 在下方登记 migration, 把旧字段搬运到新字段;
//  3. 不要删除旧字段的迁移逻辑, 否则老用户升级会丢配置.
var migrations = []migration{
	{
		From: 0,
		To:   1,
		Name: "补齐首版配置版本号与基础字段",
		Apply: func(cfg *Config) error {
			// 版本 0 表示没有 version 字段的早期配置, 只需补版本号,
			// 缺失字段交给 applyDefaults 填默认值.
			cfg.Version = 1
			return nil
		},
	},
}

// Migrate 把 cfg 的版本升级到 CurrentVersion.
//
// 调用方需要保证 cfg.Version 反映的是配置文件里的真实版本, 没有写 version
// 的老配置应先置为 0.
func Migrate(cfg *Config) error {
	if cfg.Version > CurrentVersion {
		return fmt.Errorf("配置版本 %d 高于当前程序支持的版本 %d, 请升级 mosi-asr", cfg.Version, CurrentVersion)
	}
	for cfg.Version < CurrentVersion {
		step, ok := findMigration(cfg.Version)
		if !ok {
			return fmt.Errorf("没有从配置版本 %d 到版本 %d 的迁移步骤", cfg.Version, CurrentVersion)
		}
		if err := step.Apply(cfg); err != nil {
			return fmt.Errorf("配置迁移 %q 失败: %w", step.Name, err)
		}
		slog.Debug("配置迁移完成", "from", step.From, "to", step.To, "name", step.Name)
		cfg.Version = step.To
	}
	return nil
}

func findMigration(from int) (migration, bool) {
	for _, m := range migrations {
		if m.From == from {
			return m, true
		}
	}
	return migration{}, false
}

package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/azazo1/mosi-asr/internal/config"
)

// runConfig 实现 config 子命令.
func (a *App) runConfig(global globalOptions, args []string) int {
	if len(args) == 0 {
		a.printConfigHelp()
		return ExitOK
	}

	switch sub := args[0]; sub {
	case "init":
		return a.runConfigInit(global, args[1:])
	case "path":
		return a.runConfigPath(global, args[1:])
	case "show", "list":
		return a.runConfigShow(global, args[1:])
	case "help", "--help", "-h":
		a.printConfigHelp()
		return ExitOK
	default:
		fmt.Fprintf(a.Stderr, "未知的 config 子命令 %q\n\n", sub)
		a.printConfigHelp()
		return ExitUsage
	}
}

// runConfigInit 生成配置文件.
func (a *App) runConfigInit(g globalOptions, args []string) int {
	fs := a.newFlagSet("config init")
	var force bool
	registerGlobal(fs, &g)
	fs.BoolVar(&force, "force", false, "覆盖已存在的配置文件")
	if err := fs.Parse(args); err != nil {
		return a.parseErr(err)
	}

	path, err := a.resolveConfigPath(g)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintf(a.Stderr, "配置文件已存在: %s\n如需重新生成请加 --force\n", path)
		return ExitError
	}

	cfg := config.NewTemplate()
	if err := config.Save(path, cfg); err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	fmt.Fprintf(a.Stdout, "已生成配置文件 %s\n", path)
	fmt.Fprintf(a.Stdout, "请填写 api_key, 或改用环境变量 %s\n", cfg.API.APIKeyEnv)
	return ExitOK
}

// runConfigPath 打印实际使用的配置文件路径.
func (a *App) runConfigPath(g globalOptions, args []string) int {
	fs := a.newFlagSet("config path")
	registerGlobal(fs, &g)
	if err := fs.Parse(args); err != nil {
		return a.parseErr(err)
	}
	path, err := a.resolveConfigPath(g)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	fmt.Fprintln(a.Stdout, path)
	return ExitOK
}

// runConfigShow 打印当前生效的配置, 密钥会被遮蔽.
func (a *App) runConfigShow(g globalOptions, args []string) int {
	fs := a.newFlagSet("config show")
	registerGlobal(fs, &g)
	if err := fs.Parse(args); err != nil {
		return a.parseErr(err)
	}

	s, err := a.loadSettings(g)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	defer s.close()
	cfg := s.Config

	status := "未找到, 使用内置默认值"
	if _, statErr := os.Stat(s.Path); statErr == nil {
		status = "已加载"
	}
	fmt.Fprintf(a.Stdout, "配置文件: %s (%s)\n", s.Path, status)
	key, source := cfg.ResolveAPIKey()
	keyDesc := "未配置"
	if key != "" {
		keyDesc = maskKey(key) + " (来源: " + source + ")"
	}
	fmt.Fprintf(a.Stdout, "API Key: %s\n", keyDesc)

	printed := *cfg
	printed.API.APIKey = maskOrEmpty(cfg.API.APIKey)
	fmt.Fprintf(a.Stdout, "\n%s", config.Render(&printed))
	return ExitOK
}

// resolveConfigPath 推导当前使用的配置路径.
func (a *App) resolveConfigPath(g globalOptions) (string, error) {
	path, _, err := config.ResolvePath(g.configPath)
	if err != nil {
		return "", err
	}
	return path, nil
}

// maskKey 只保留密钥前后少量字符, 避免泄露完整内容.
func maskKey(key string) string {
	rs := []rune(key)
	if len(rs) <= 8 {
		return strings.Repeat("*", len(rs))
	}
	return string(rs[:4]) + strings.Repeat("*", len(rs)-8) + string(rs[len(rs)-4:])
}

func maskOrEmpty(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return maskKey(key)
}

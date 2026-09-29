// Package cli 实现 mosi-asr 的命令行入口.
//
// 子命令划分:
//   - transcribe (默认): 转写音频;
//   - config: 查看与初始化配置;
//   - task: 查询异步任务;
//   - help / version.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/azazo1/mosi-asr/internal/config"
	"github.com/azazo1/mosi-asr/internal/logger"
)

// 退出码约定.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// App 是命令行应用.
type App struct {
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
}

// New 创建应用实例.
func New(version string) *App {
	return &App{Version: version, Stdout: os.Stdout, Stderr: os.Stderr}
}

// globalOptions 是各子命令共用的全局参数.
type globalOptions struct {
	configPath string
	verbose    bool
	quiet      bool
	showHelp   bool
}

// Run 执行命令行, 返回进程退出码.
func (a *App) Run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 先摘出写在子命令之前的全局参数, 保证 "mosi-asr --config x config show" 这类写法可用.
	global, rest := splitGlobalArgs(args)
	if len(rest) == 0 {
		if global.showHelp {
			a.printHelp()
			return ExitOK
		}
		return a.runTranscribe(ctx, global, nil)
	}

	switch cmd := rest[0]; cmd {
	case "transcribe", "tr", "asr":
		return a.runTranscribe(ctx, global, rest[1:])
	case "config", "cfg":
		return a.runConfig(global, rest[1:])
	case "task":
		return a.runTask(ctx, global, rest[1:])
	case "help", "--help", "-h":
		a.printHelp()
		return ExitOK
	case "version", "--version", "-V":
		fmt.Fprintf(a.Stdout, "mosi-asr %s\n", a.Version)
		return ExitOK
	default:
		// 既不是子命令就当作音频文件处理.
		return a.runTranscribe(ctx, global, rest)
	}
}

// splitGlobalArgs 拆出位于子命令之前的全局参数.
func splitGlobalArgs(args []string) (globalOptions, []string) {
	var g globalOptions
	for i := 0; i < len(args); {
		arg := args[i]
		switch {
		case arg == "--config" || arg == "-config":
			if i+1 < len(args) {
				g.configPath = args[i+1]
				i += 2
				continue
			}
			i++
		case strings.HasPrefix(arg, "--config="), strings.HasPrefix(arg, "-config="):
			g.configPath = arg[strings.Index(arg, "=")+1:]
			i++
		case arg == "-v", arg == "--verbose":
			g.verbose = true
			i++
		case arg == "-q", arg == "--quiet":
			g.quiet = true
			i++
		case arg == "-h", arg == "--help":
			g.showHelp = true
			i++
		default:
			return g, args[i:]
		}
	}
	return g, nil
}

// newFlagSet 构造带统一错误处理的 FlagSet.
//
// 输出被丢弃, 参数错误统一由 parseErr 汇报, 避免同一条错误打印两次.
func (a *App) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// registerGlobal 注册全局参数.
//
// 默认值取自 g 的当前内容, 这样写在子命令之前的全局参数不会被覆盖.
func registerGlobal(fs *flag.FlagSet, g *globalOptions) {
	fs.StringVar(&g.configPath, "config", g.configPath, "配置文件路径, 默认 ~/.config/mosi-asr/config.toml")
	fs.BoolVar(&g.verbose, "v", g.verbose, "输出调试日志")
	fs.BoolVar(&g.verbose, "verbose", g.verbose, "输出调试日志")
	fs.BoolVar(&g.quiet, "q", g.quiet, "只输出错误日志")
	fs.BoolVar(&g.quiet, "quiet", g.quiet, "只输出错误日志")
}

// settings 汇总一次运行所需的配置与日志器.
type settings struct {
	Config *config.Config
	Path   string
	Logger *slog.Logger
	// closeLog 用于关闭日志文件.
	closeLog func()
}

// loadSettings 读取配置并初始化日志.
func (a *App) loadSettings(g globalOptions) (*settings, error) {
	res, err := config.Load(g.configPath)
	if err != nil {
		return nil, err
	}
	cfg := res.Config

	log, closeLog, err := logger.Setup(logger.Options{
		Level:  cfg.Log.Level,
		File:   cfg.Log.File,
		Quiet:  g.quiet,
		Writer: a.Stderr,
	})
	if err != nil {
		return nil, err
	}
	if g.verbose && !g.quiet {
		// 打开调试日志需要重建日志器, 保持单一输出通道.
		closeLog()
		log, closeLog, err = logger.Setup(logger.Options{
			Level:  "debug",
			File:   cfg.Log.File,
			Writer: a.Stderr,
		})
		if err != nil {
			return nil, err
		}
	}

	s := &settings{Config: cfg, Path: res.Path, Logger: log, closeLog: closeLog}
	if !res.Exists {
		log.Debug("未找到配置文件, 使用内置默认值", "path", res.Path)
	}
	if res.Migrated {
		log.Info("配置已自动迁移", "from", res.MigratedFrom, "to", cfg.Version, "path", res.Path)
	}
	for _, f := range res.UnknownFields {
		log.Warn("配置文件包含未知字段, 请检查拼写", "field", f, "path", res.Path)
	}
	return s, nil
}

// close 释放资源.
func (s *settings) close() {
	if s != nil && s.closeLog != nil {
		s.closeLog()
	}
}

// parseErr 把 flag 解析错误转换成统一的返回码.
func (a *App) parseErr(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	fmt.Fprintf(a.Stderr, "参数错误: %v\n", err)
	return ExitUsage
}

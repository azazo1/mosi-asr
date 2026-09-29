// Package logger 统一 mosi-asr 的日志输出.
//
// 日志始终写到 stderr, 保证 stdout 只承载转写结果, 方便管道使用.
// 配置了 log.file 时同时落盘一份, 便于事后排查长音频任务.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Options 是日志初始化参数.
type Options struct {
	// Level 取值 debug, info, warn, error, 空值按 info 处理.
	Level string
	// File 非空时追加写入该文件.
	File string
	// Quiet 为 true 时只保留 warn 及以上级别, 覆盖 Level.
	Quiet bool
	// Writer 是日志终点, 为空时使用 stderr.
	Writer io.Writer
}

// Setup 构建日志器, 返回清理函数用于关闭日志文件.
func Setup(opts Options) (*slog.Logger, func(), error) {
	level := ParseLevel(opts.Level)
	if opts.Quiet {
		level = slog.LevelWarn
	}

	var (
		writer  io.Writer = os.Stderr
		closers []io.Closer
	)
	if opts.Writer != nil {
		writer = opts.Writer
	}
	if opts.File != "" {
		path := opts.File
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, func() {}, fmt.Errorf("创建日志目录失败: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, func() {}, fmt.Errorf("打开日志文件失败: %w", err)
		}
		closers = append(closers, f)
		writer = io.MultiWriter(writer, f)
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// 时间只保留到秒, 日志更紧凑.
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05"))
			}
			return a
		},
	})
	log := slog.New(handler)

	cleanup := func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}
	return log, cleanup, nil
}

// ParseLevel 把字符串解析成 slog 级别.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Discard 返回一个不输出任何内容的日志器, 供测试使用.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/azazo1/mosi-asr/internal/config"
	"github.com/azazo1/mosi-asr/internal/mossapi"
	"github.com/azazo1/mosi-asr/internal/output"
)

// runTask 实现 task 子命令, 用于查询异步任务结果.
func (a *App) runTask(ctx context.Context, g globalOptions, args []string) int {
	fs := a.newFlagSet("task")
	registerGlobal(fs, &g)

	var (
		wait     bool
		format   string
		outPath  string
		stdout   bool
		timeout  int
		interval int
	)
	fs.BoolVar(&wait, "wait", false, "任务未完成时持续轮询")
	fs.StringVar(&format, "f", "", "输出格式: "+config.FormatList())
	fs.StringVar(&format, "format", "", "输出格式: "+config.FormatList())
	fs.StringVar(&outPath, "o", "", "结果文件路径, 传 - 表示输出到标准输出")
	fs.StringVar(&outPath, "output", "", "结果文件路径, 传 - 表示输出到标准输出")
	fs.BoolVar(&stdout, "stdout", false, "结果输出到标准输出")
	fs.IntVar(&timeout, "timeout", 0, "最长等待秒数, 0 表示用配置值")
	fs.IntVar(&interval, "interval", 0, "轮询间隔秒数, 0 表示用配置值")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return a.parseErr(err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(a.Stderr, "用法: mosi-asr task <task_id> [--wait] [-f srt] [-o 路径]")
		return ExitUsage
	}
	taskID := strings.TrimSpace(rest[0])

	s, err := a.loadSettings(g)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	defer s.close()

	client, err := a.newClient(s)
	if err != nil {
		fmt.Fprintf(a.Stderr, "初始化客户端失败: %v\n", err)
		return ExitError
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.Transcribe.PollTimeoutSec)*time.Second)
	defer cancel()

	var task *mossapi.Task
	if wait {
		pollInterval := time.Duration(s.Config.Transcribe.PollIntervalSec) * time.Second
		if interval > 0 {
			pollInterval = time.Duration(interval) * time.Second
		}
		limit := time.Duration(s.Config.Transcribe.PollTimeoutSec) * time.Second
		if timeout > 0 {
			limit = time.Duration(timeout) * time.Second
		}
		task, err = client.WaitTask(ctx, taskID, pollInterval, limit, func(t *mossapi.Task) {
			s.Logger.Info("查询任务", "task_id", t.TaskKey(), "status", t.Status)
		})
	} else {
		task, err = client.GetTask(ctx, taskID)
	}
	if err != nil {
		fmt.Fprintf(a.Stderr, "查询任务失败: %v\n", err)
		return ExitError
	}

	fmt.Fprintf(a.Stdout, "task_id: %s\n状态: %s\n", task.TaskKey(), task.Status)
	if task.Status != mossapi.StatusSuccess {
		if task.Error != nil {
			fmt.Fprintf(a.Stdout, "失败原因: %s\n", task.Error.Error())
		}
		if !wait {
			fmt.Fprintln(a.Stderr, "任务尚未完成, 可加 --wait 等待结果")
		}
		return ExitOK
	}

	format = normalizeFormatArg(format, s.Config)
	dest := outPath
	if stdout {
		dest = "-"
	}
	target, err := output.ResolveTarget("", dest, s.Config.Output.Dir, format, s.Config.Output.OverwriteFile)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}

	res := output.New("", task.ToTranscription(), s.Config.Transcribe.MergeGapSeconds)
	opts := output.Options{
		Format:        format,
		SpeakerPrefix: s.Config.Transcribe.SpeakerNamePrefix,
		Timestamps:    s.Config.Transcribe.IncludeTimestamps,
	}
	if target.Stdout {
		if err := output.Render(a.Stdout, res, opts); err != nil {
			fmt.Fprintf(a.Stderr, "%v\n", err)
			return ExitError
		}
		return ExitOK
	}
	content, err := output.Marshal(res, opts)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	if err := output.WriteFile(target.Path, content); err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	fmt.Fprintf(a.Stdout, "结果已写入 %s\n", target.Path)
	return ExitOK
}

// normalizeFormatArg 处理 -f 参数, 空值时回退到配置值.
func normalizeFormatArg(arg string, cfg *config.Config) string {
	if strings.TrimSpace(arg) == "" {
		return cfg.Output.Format
	}
	if v, ok := config.NormalizeFormat(arg); ok {
		return v
	}
	return cfg.Output.Format
}

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/azazo1/mosi-asr/internal/audio"
	"github.com/azazo1/mosi-asr/internal/config"
	"github.com/azazo1/mosi-asr/internal/mossapi"
	"github.com/azazo1/mosi-asr/internal/output"
)

// transcribeFlags 是 transcribe 子命令的参数.
type transcribeFlags struct {
	model        string
	diarize      bool
	noDiarize    bool
	format       string
	outputPath   string
	stdout       bool
	stream       bool
	async        bool
	syncMode     bool
	jsonFormat   bool
	keyterms     stringList
	keytermText  string
	url          string
	fileID       string
	dir          string
	timestamps   bool
	noSpeakerPre bool
	mergeGap     float64
	timeout      int
}

// stringList 是支持重复出现与逗号分隔的字符串参数.
type stringList []string

// String 实现 flag.Value 接口.
func (s *stringList) String() string { return strings.Join(*s, ",") }

// Set 追加取值, 同时支持逗号分隔.
func (s *stringList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

// transcribePlan 汇总一次运行所需的目标与输入.
type transcribePlan struct {
	req mossapi.TranscribeRequest
	// inputs 是本地待转写文件; 为空表示使用远程输入.
	inputs []string
	// target 为 nil 表示每个文件各自推导输出位置.
	target *output.Target
}

// runTranscribe 执行转写.
func (a *App) runTranscribe(ctx context.Context, g globalOptions, args []string) int {
	fs := a.newFlagSet("transcribe")
	registerGlobal(fs, &g)

	var f transcribeFlags
	fs.StringVar(&f.model, "m", "", "使用的模型 ID, 默认按是否启用说话人识别自动选择")
	fs.StringVar(&f.model, "model", "", "使用的模型 ID, 默认按是否启用说话人识别自动选择")
	fs.BoolVar(&f.diarize, "d", false, "启用说话人识别 (多说话人转写)")
	fs.BoolVar(&f.diarize, "diarize", false, "启用说话人识别 (多说话人转写)")
	fs.BoolVar(&f.noDiarize, "no-diarize", false, "关闭配置里的默认说话人识别")
	fs.StringVar(&f.format, "f", "", "输出格式: "+config.FormatList())
	fs.StringVar(&f.format, "format", "", "输出格式: "+config.FormatList())
	fs.BoolVar(&f.jsonFormat, "json", false, "等价于 -f json")
	fs.StringVar(&f.outputPath, "o", "", "结果文件路径, 传 - 表示输出到标准输出")
	fs.StringVar(&f.outputPath, "output", "", "结果文件路径, 传 - 表示输出到标准输出")
	fs.BoolVar(&f.stdout, "stdout", false, "结果输出到标准输出")
	fs.StringVar(&f.dir, "dir", "", "结果输出目录, 默认与音频同目录")
	fs.BoolVar(&f.stream, "s", false, "流式输出, 边转写边显示 (需要多说话人模型)")
	fs.BoolVar(&f.stream, "stream", false, "流式输出, 边转写边显示 (需要多说话人模型)")
	fs.BoolVar(&f.async, "async", false, "使用异步任务, 适合长音频")
	fs.BoolVar(&f.syncMode, "sync", false, "强制使用同步请求")
	fs.Var(&f.keyterms, "k", "热词, 可重复使用, 也可逗号分隔 (最多 20 个)")
	fs.Var(&f.keyterms, "keyterm", "热词, 可重复使用, 也可逗号分隔 (最多 20 个)")
	fs.StringVar(&f.keytermText, "keyterms", "", "热词, 用逗号分隔")
	fs.StringVar(&f.url, "url", "", "转写公网音频地址 (与本地文件二选一)")
	fs.StringVar(&f.fileID, "file-id", "", "转写已上传文件的 file_id (与本地文件二选一)")
	fs.BoolVar(&f.timestamps, "timestamps", false, "文本结果中保留时间戳")
	fs.BoolVar(&f.noSpeakerPre, "no-speaker-prefix", false, "不在文本结果中加说话人前缀")
	fs.Float64Var(&f.mergeGap, "merge-gap", -1, "同一说话人相邻分段的合并间隔秒数, -1 表示用配置值")
	fs.IntVar(&f.timeout, "timeout", 0, "单次请求超时秒数, 0 表示用配置值")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.printTranscribeHelp()
			return ExitOK
		}
		return a.parseErr(err)
	}

	s, err := a.loadSettings(g)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}
	defer s.close()

	if err := a.applyTranscribeFlags(s.Config, &f); err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitUsage
	}

	plan, err := a.prepareTranscribe(s, &f, fs.Args())
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitUsage
	}

	client, err := a.newClient(s)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%v\n", err)
		return ExitError
	}

	inputs := plan.inputs
	if len(inputs) == 0 {
		// 远程输入没有本地文件, 用空路径占位.
		inputs = []string{""}
	}

	var failed int
	for i, src := range inputs {
		if len(inputs) > 1 {
			fmt.Fprintf(a.Stderr, "[%d/%d] %s\n", i+1, len(inputs), displaySource(src))
		}
		if err := a.transcribeOne(ctx, client, s, &f, plan, src); err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(a.Stderr, "已取消")
				return ExitError
			}
			fmt.Fprintf(a.Stderr, "转写失败: %v\n", err)
			failed++
		}
	}
	if failed > 0 {
		return ExitError
	}
	return ExitOK
}

// prepareTranscribe 校验参数并推导请求与输出位置.
func (a *App) prepareTranscribe(s *settings, f *transcribeFlags, positional []string) (*transcribePlan, error) {
	if f.format != "" {
		if _, ok := config.NormalizeFormat(f.format); !ok {
			return nil, fmt.Errorf("不支持的输出格式 %q, 可选: %s", f.format, config.FormatList())
		}
	}
	if f.stream && f.syncMode {
		return nil, errors.New("--stream 与 --sync 不能同时使用")
	}
	if f.stream && f.async {
		return nil, errors.New("--stream 与 --async 不能同时使用")
	}

	cfg := s.Config
	plan := &transcribePlan{}
	plan.req.Diarize = cfg.Transcribe.Diarize
	plan.req.Stream = cfg.Transcribe.Stream
	plan.req.Async = cfg.Transcribe.Async
	plan.req.Keyterms = collectKeyterms(cfg, f)
	if plan.req.Stream {
		plan.req.Async = false
	}

	url := strings.TrimSpace(f.url)
	fileID := strings.TrimSpace(f.fileID)
	remote := url != "" || fileID != ""

	// 流式与热词都要求多说话人模型, 用户没有显式指定模型时自动切换.
	if f.model == "" && (plan.req.Stream || len(plan.req.Keyterms) > 0) {
		plan.req.Model = cfg.Transcribe.DiarizeModel
	} else {
		plan.req.Model = effectiveModel(cfg, f, plan.req.Diarize)
	}

	if remote {
		if len(positional) > 0 {
			return nil, errors.New("使用 --url 或 --file-id 时不能再传入本地文件")
		}
		plan.req.URL = url
		plan.req.FileID = fileID
		target, err := output.ResolveTarget("", outArg(f), f.dir, effectiveFormat(cfg, f), cfg.Output.OverwriteFile)
		if err != nil {
			return nil, err
		}
		plan.target = target
		return plan, nil
	}

	if len(positional) == 0 {
		return nil, errors.New("请指定要转写的音频文件, 或使用 --url / --file-id")
	}
	inputs, err := audio.ExpandInputs(positional)
	if err != nil {
		var unknown *audio.UnknownExtensionError
		if !errors.As(err, &unknown) {
			return nil, err
		}
		fmt.Fprintf(a.Stderr, "提示: %v\n", err)
	}
	plan.inputs = inputs
	if len(inputs) == 1 {
		plan.req.FilePath = inputs[0]
	}

	target, err := resolveTargetForBatch(cfg, f, inputs)
	if err != nil {
		return nil, err
	}
	plan.target = target
	return plan, nil
}

// resolveTargetForBatch 推导输出位置: 单文件精确到文件, 多文件时逐文件推导.
func resolveTargetForBatch(cfg *config.Config, f *transcribeFlags, inputs []string) (*output.Target, error) {
	format := effectiveFormat(cfg, f)
	if len(inputs) == 1 {
		return output.ResolveTarget(inputs[0], outArg(f), f.dir, format, cfg.Output.OverwriteFile)
	}
	if path := strings.TrimSpace(f.outputPath); path != "" && path != "-" {
		return nil, errors.New("转写多个文件时不能指定单个输出文件, 请改用 --dir")
	}
	if f.stdout || f.outputPath == "-" {
		return &output.Target{Stdout: true}, nil
	}
	// nil 表示每个文件各自推导.
	return nil, nil
}

// transcribeOne 转写单个输入并按目标输出结果.
func (a *App) transcribeOne(ctx context.Context, client *mossapi.Client, s *settings, f *transcribeFlags,
	plan *transcribePlan, src string) error {
	cfg := s.Config
	log := s.Logger

	req := plan.req
	var size int64
	if src != "" {
		req.FilePath = src
		info, err := audio.Inspect(src)
		if err != nil {
			return err
		}
		size = info.Size
		if !info.KnownExtension {
			log.Warn("扩展名不在文档支持列表内, 仍会尝试上传",
				"source", displaySource(src), "extension", info.Extension,
				"supported", audio.SupportedExtensionsText())
		}
	}

	// 长音频自动切换异步, 避免同步请求长时间占用连接.
	if !req.Async && !req.Stream && !f.syncMode && shouldAutoAsync(cfg, size) {
		req.Async = true
		log.Info("音频较大, 自动使用异步任务", "source", displaySource(src),
			"size", humanSize(size), "threshold_mb", cfg.Transcribe.AutoAsyncOverMB)
	}

	mode := describeMode(req)
	log.Info("开始转写", "source", displaySource(src), "model", req.Model,
		"diarize", req.Diarize, "mode", mode)

	format := effectiveFormat(cfg, f)
	target := plan.target
	if target == nil {
		t, err := output.ResolveTarget(src, outArg(f), f.dir, format, cfg.Output.OverwriteFile)
		if err != nil {
			return err
		}
		target = t
	}

	req.UploadProgress = func(sent, total int64) {
		log.Info("上传中", "source", displaySource(src),
			"percent", fmt.Sprintf("%.0f%%", float64(sent)*100/float64(total)),
			"sent", humanSize(sent), "total", humanSize(total))
	}

	started := time.Now()
	tr, err := a.execute(ctx, client, s, req, target, format)
	if err != nil {
		return err
	}
	log.Info("转写完成", "source", displaySource(src), "elapsed", time.Since(started).Round(time.Millisecond).String())

	res := output.New(src, tr, effectiveMergeGap(cfg, f))
	opts := output.Options{
		Format:        format,
		SpeakerPrefix: speakerPrefix(cfg, f),
		Timestamps:    cfg.Transcribe.IncludeTimestamps,
	}

	if target.Stdout {
		if req.Stream && isPlainTextFormat(format) {
			// 流式模式已把增量文本写到标准输出, 这里只补一个换行.
			fmt.Fprintln(a.Stdout)
			return nil
		}
		return output.Render(a.Stdout, res, opts)
	}

	content, err := output.Marshal(res, opts)
	if err != nil {
		return err
	}
	if err := output.WriteFile(target.Path, content); err != nil {
		return err
	}
	log.Info("结果已写入", "path", target.Path, "format", format, "bytes", len(content))
	fmt.Fprintf(a.Stderr, "结果已写入 %s\n", target.Path)
	return nil
}

// execute 按请求参数选择同步, 流式或异步调用.
func (a *App) execute(ctx context.Context, client *mossapi.Client, s *settings,
	req mossapi.TranscribeRequest, target *output.Target, format string) (*mossapi.Transcription, error) {
	log := s.Logger

	if req.Stream {
		return a.executeStream(ctx, client, s, req, target, format)
	}

	if req.Async {
		task, err := client.CreateTask(ctx, req)
		if err != nil {
			return nil, err
		}
		log.Info("任务已创建", "task_id", task.TaskKey(), "status", task.Status)

		lastStatus := task.Status
		finished, err := client.WaitTask(ctx, task.TaskKey(),
			time.Duration(s.Config.Transcribe.PollIntervalSec)*time.Second,
			time.Duration(s.Config.Transcribe.PollTimeoutSec)*time.Second,
			func(t *mossapi.Task) {
				if t.Status != lastStatus {
					log.Info("任务状态变化", "task_id", t.TaskKey(), "status", t.Status)
					lastStatus = t.Status
				} else {
					log.Debug("任务状态", "task_id", t.TaskKey(), "status", t.Status)
				}
			})
		if err != nil {
			return nil, err
		}
		return finished.ToTranscription(), nil
	}

	return client.Transcribe(ctx, req)
}

// executeStream 执行流式转写.
func (a *App) executeStream(ctx context.Context, client *mossapi.Client, s *settings,
	req mossapi.TranscribeRequest, target *output.Target, format string) (*mossapi.Transcription, error) {
	log := s.Logger

	// 只有最终结果也是纯文本且写到标准输出时, 增量文本才跟着走标准输出;
	// 否则增量文本属于进度信息, 写到标准错误, 避免污染 json, srt 等结构化结果.
	echoToStdout := target.Stdout && isPlainTextFormat(format)
	echo := a.Stderr
	if echoToStdout {
		echo = a.Stdout
	}
	if !echoToStdout {
		log.Info("流式输出中, 最终结果会另外保存")
	}

	tr, err := client.TranscribeStream(ctx, req, mossapi.StreamResult{
		OnCreated: func(taskID string) {
			log.Info("流式任务已创建", "task_id", taskID)
		},
		OnDelta: func(delta string) {
			fmt.Fprint(echo, delta)
		},
	})
	if err != nil {
		return nil, err
	}
	if !echoToStdout {
		fmt.Fprintln(a.Stderr)
	}
	log.Info("流式转写结束", "segments", len(tr.Segments))
	return tr, nil
}

// newClient 按配置构造 API 客户端.
func (a *App) newClient(s *settings) (*mossapi.Client, error) {
	key, source := s.Config.ResolveAPIKey()
	if key == "" {
		return nil, fmt.Errorf("未找到 API Key, 请在 %s 里填写 api.api_key, "+
			"或设置环境变量 %s; 也可以先执行 mosi-asr config init",
			s.Path, s.Config.API.APIKeyEnv)
	}
	s.Logger.Debug("API Key 来源", "source", source)

	return mossapi.New(mossapi.Options{
		BaseURL:    s.Config.API.BaseURL,
		APIKey:     key,
		Timeout:    time.Duration(s.Config.API.TimeoutSeconds) * time.Second,
		MaxRetries: s.Config.API.MaxRetries,
		Logger:     s.Logger,
	}), nil
}

// applyTranscribeFlags 把命令行参数覆盖到配置上.
func (a *App) applyTranscribeFlags(cfg *config.Config, f *transcribeFlags) error {
	if f.diarize {
		cfg.Transcribe.Diarize = true
	}
	if f.noDiarize {
		cfg.Transcribe.Diarize = false
	}
	if f.stream {
		cfg.Transcribe.Stream = true
		cfg.Transcribe.Async = false
	}
	if f.async {
		cfg.Transcribe.Async = true
	}
	if f.syncMode {
		cfg.Transcribe.Async = false
	}
	if f.model != "" {
		cfg.Transcribe.Model = f.model
	}
	if f.timeout > 0 {
		cfg.API.TimeoutSeconds = f.timeout
	}
	if f.timestamps {
		cfg.Transcribe.IncludeTimestamps = true
	}
	if f.noSpeakerPre {
		cfg.Transcribe.SpeakerNamePrefix = false
	}
	if f.mergeGap >= 0 {
		cfg.Transcribe.MergeGapSeconds = f.mergeGap
	}
	if f.dir != "" {
		cfg.Output.Dir = f.dir
	}
	if f.jsonFormat {
		cfg.Output.Format = "json"
	} else if f.format != "" {
		v, ok := config.NormalizeFormat(f.format)
		if !ok {
			return fmt.Errorf("不支持的输出格式 %q, 可选: %s", f.format, config.FormatList())
		}
		cfg.Output.Format = v
	}
	return nil
}

// effectiveModel 决定本次请求使用的模型.
func effectiveModel(cfg *config.Config, f *transcribeFlags, diarize bool) string {
	if f.model != "" {
		return f.model
	}
	if diarize {
		return cfg.Transcribe.DiarizeModel
	}
	return cfg.Transcribe.Model
}

// effectiveFormat 决定输出格式, 显式 -o 的文件扩展名优先.
func effectiveFormat(cfg *config.Config, f *transcribeFlags) string {
	if f.jsonFormat {
		return "json"
	}
	if f.format != "" {
		if v, ok := config.NormalizeFormat(f.format); ok {
			return v
		}
	}
	if f.outputPath != "" && f.outputPath != "-" {
		if v, ok := config.NormalizeFormat(filepath.Ext(f.outputPath)); ok {
			return v
		}
	}
	return cfg.Output.Format
}

// effectiveMergeGap 返回本次使用的合并间隔.
func effectiveMergeGap(cfg *config.Config, f *transcribeFlags) float64 {
	if f.mergeGap >= 0 {
		return f.mergeGap
	}
	return cfg.Transcribe.MergeGapSeconds
}

// speakerPrefix 判断文本结果是否带说话人前缀.
func speakerPrefix(cfg *config.Config, f *transcribeFlags) bool {
	if f.noSpeakerPre {
		return false
	}
	return cfg.Transcribe.SpeakerNamePrefix
}

// collectKeyterms 合并配置与命令行中的热词, 保持顺序并去重.
func collectKeyterms(cfg *config.Config, f *transcribeFlags) []string {
	terms := make([]string, 0, len(cfg.Transcribe.Keyterms)+len(f.keyterms)+4)
	seen := map[string]bool{}
	appendTerm := func(t string) {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			return
		}
		seen[t] = true
		terms = append(terms, t)
	}
	for _, t := range cfg.Transcribe.Keyterms {
		appendTerm(t)
	}
	for _, t := range f.keyterms {
		appendTerm(t)
	}
	for _, t := range strings.Split(f.keytermText, ",") {
		appendTerm(t)
	}
	return terms
}

// describeMode 用一句话描述本次调用方式, 便于日志排查.
func describeMode(req mossapi.TranscribeRequest) string {
	switch {
	case req.Stream:
		return "流式"
	case req.Async:
		return "异步"
	default:
		return "同步"
	}
}

// outArg 返回统一的输出参数: --stdout 等价于 -o -.
func outArg(f *transcribeFlags) string {
	if f.stdout {
		return "-"
	}
	return f.outputPath
}

// isPlainTextFormat 判断格式是否属于可直接流式回显的纯文本.
func isPlainTextFormat(format string) bool {
	switch strings.ToLower(format) {
	case "txt", "text":
		return true
	default:
		return false
	}
}

// displaySource 返回用于展示的输入名称.
func displaySource(src string) string {
	if src == "" {
		return "(远程输入)"
	}
	return filepath.Base(src)
}

// humanSize 把字节数格式化成易读字符串.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

// shouldAutoAsync 判断是否因为文件过大而自动切换到异步任务.
func shouldAutoAsync(cfg *config.Config, sizeBytes int64) bool {
	if cfg.Transcribe.AutoAsyncOverMB <= 0 || sizeBytes <= 0 {
		return false
	}
	return sizeBytes > int64(cfg.Transcribe.AutoAsyncOverMB)<<20
}

package config

import (
	"fmt"
	"sort"
	"strings"
)

// ValidFormats 是 output.format 允许的取值.
// txt 与 text 的区别: text 直接输出接口返回的整段文本, txt 会做说话人分段整理.
var ValidFormats = map[string]string{
	"txt":  "整理后的纯文本, 启用说话人识别时按说话人分行",
	"text": "接口返回的原始整段文本",
	"srt":  "字幕文件, 带时间轴, 说话人写入字幕文本",
	"json": "接口返回的原始 JSON, 便于脚本处理",
	"md":   "Markdown, 按说话人分段并带时间范围",
}

// ValidLogLevels 是 log.level 允许的取值.
var ValidLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// FormatList 返回可读的格式列表, 例如 "json, md, srt, text, txt".
func FormatList() string {
	names := make([]string, 0, len(ValidFormats))
	for name := range ValidFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// FormatExtension 返回格式对应的文件扩展名.
func FormatExtension(format string) string {
	switch format {
	case "text":
		return "txt"
	case "json":
		return "json"
	case "srt":
		return "srt"
	case "md":
		return "md"
	default:
		return "txt"
	}
}

// NormalizeFormat 把用户输入或文件扩展名归一化成合法格式.
func NormalizeFormat(s string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, ".")))
	switch v {
	case "txt", "text", "srt", "json", "md":
		return v, true
	default:
		return "", false
	}
}

// Render 生成带注释的配置文件内容, 供 config init 使用.
func Render(cfg *Config) string {
	if cfg == nil {
		cfg = Default()
	}
	var b strings.Builder
	b.WriteString("# mosi-asr 配置文件\n")
	b.WriteString("# 文档: https://platform.mosi.cn/docs/scenarios/transcribe-diarization\n")
	b.WriteString("#\n")
	b.WriteString("# 命令行参数优先级高于本文件; 密钥也可以只放进环境变量, 避免明文落盘.\n")
	b.WriteString("# version 由程序维护, 升级时旧配置会自动迁移, 一般不需要手改.\n\n")

	fmt.Fprintf(&b, "version = %d\n\n", cfg.Version)

	b.WriteString("[api]\n")
	b.WriteString("# 服务地址, 一般不需要修改.\n")
	fmt.Fprintf(&b, "base_url = %q\n", cfg.API.BaseURL)
	b.WriteString("# 直接写在这里的 API Key; 留空则改用下面的环境变量.\n")
	fmt.Fprintf(&b, "api_key = %q\n", cfg.API.APIKey)
	b.WriteString("# 读取 API Key 的环境变量名.\n")
	fmt.Fprintf(&b, "api_key_env = %q\n", cfg.API.APIKeyEnv)
	b.WriteString("# 单次 HTTP 请求超时, 单位秒. 同步转写长音频时需要放宽.\n")
	fmt.Fprintf(&b, "timeout_seconds = %d\n", cfg.API.TimeoutSeconds)
	b.WriteString("# 遇到网络错误, 429 或 5xx 时的重试次数.\n")
	fmt.Fprintf(&b, "max_retries = %d\n\n", cfg.API.MaxRetries)

	b.WriteString("[transcribe]\n")
	b.WriteString("# 普通转写使用的模型.\n")
	fmt.Fprintf(&b, "model = %q\n", cfg.Transcribe.Model)
	b.WriteString("# 多说话人转写使用的模型, 也支持热词与流式输出.\n")
	fmt.Fprintf(&b, "diarize_model = %q\n", cfg.Transcribe.DiarizeModel)
	b.WriteString("# 默认是否启用说话人识别; 也可以用 -d 按次开启.\n")
	fmt.Fprintf(&b, "diarize = %t\n", cfg.Transcribe.Diarize)
	b.WriteString("# 默认是否流式输出; 流式要求多说话人模型, 且不能与 async 同用.\n")
	fmt.Fprintf(&b, "stream = %t\n", cfg.Transcribe.Stream)
	b.WriteString("# 默认是否走异步任务.\n")
	fmt.Fprintf(&b, "async = %t\n", cfg.Transcribe.Async)
	b.WriteString("# 超过该大小的音频自动改用异步任务, 单位 MB; 设为 0 表示不自动切换.\n")
	fmt.Fprintf(&b, "auto_async_over_mb = %d\n", cfg.Transcribe.AutoAsyncOverMB)
	b.WriteString("# 异步任务轮询间隔与最长等待时间, 单位秒.\n")
	fmt.Fprintf(&b, "poll_interval_seconds = %d\npoll_timeout_seconds = %d\n", cfg.Transcribe.PollIntervalSec, cfg.Transcribe.PollTimeoutSec)
	b.WriteString("# 热词, 最多 20 个, 每个不超过 30 个字符; 仅多说话人模型支持.\n")
	fmt.Fprintf(&b, "keyterms = %s\n", renderStringSlice(cfg.Transcribe.Keyterms))
	b.WriteString("# 说话人相同的相邻分段间隔小于该秒数时合并成一段.\n")
	fmt.Fprintf(&b, "merge_gap_seconds = %s\n", renderFloat(cfg.Transcribe.MergeGapSeconds))
	b.WriteString("# 是否在文本结果中保留时间戳.\n")
	fmt.Fprintf(&b, "include_timestamps = %t\n", cfg.Transcribe.IncludeTimestamps)
	b.WriteString("# 说话人文本是否带 \"S01: \" 前缀.\n")
	fmt.Fprintf(&b, "speaker_name_prefix = %t\n\n", cfg.Transcribe.SpeakerNamePrefix)

	b.WriteString("[output]\n")
	b.WriteString("# 默认输出格式: " + FormatList() + "\n")
	fmt.Fprintf(&b, "format = %q\n", cfg.Output.Format)
	b.WriteString("# 结果输出目录, 留空表示与音频文件放在同一目录; 支持 ~ 展开.\n")
	fmt.Fprintf(&b, "dir = %q\n", cfg.Output.Dir)
	b.WriteString("# 结果文件已存在时是否覆盖, false 则自动追加序号.\n")
	fmt.Fprintf(&b, "overwrite = %t\n\n", cfg.Output.OverwriteFile)

	b.WriteString("[log]\n")
	b.WriteString("# 日志级别: debug, info, warn, error. 日志始终写到 stderr.\n")
	fmt.Fprintf(&b, "level = %q\n", cfg.Log.Level)
	b.WriteString("# 额外写入的日志文件, 留空表示不落盘.\n")
	fmt.Fprintf(&b, "file = %q\n", cfg.Log.File)
	return b.String()
}

func renderStringSlice(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(items))
	for _, it := range items {
		quoted = append(quoted, fmt.Sprintf("%q", it))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func renderFloat(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

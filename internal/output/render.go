package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Options 控制结果的呈现方式.
type Options struct {
	// Format 取值 txt, text, srt, json, md.
	Format string
	// SpeakerPrefix 为 true 时在文本中加 "S01: " 前缀.
	SpeakerPrefix bool
	// Timestamps 为 true 时在文本中加时间范围.
	Timestamps bool
}

// Render 按指定格式把结果写入 w.
func Render(w io.Writer, res *Result, opts Options) error {
	if res == nil {
		return fmt.Errorf("没有可输出的结果")
	}
	switch normFormat(opts.Format) {
	case "txt":
		return renderText(w, res, opts)
	case "text":
		_, err := fmt.Fprintln(w, res.Text)
		return err
	case "srt":
		return renderSRT(w, res, opts)
	case "json":
		return renderJSON(w, res)
	case "md":
		return renderMarkdown(w, res, opts)
	default:
		return fmt.Errorf("不支持的输出格式 %q", opts.Format)
	}
}

func normFormat(f string) string {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "text", "plain":
		return "text"
	case "srt", "subtitle", "sub":
		return "srt"
	case "json":
		return "json"
	case "md", "markdown":
		return "md"
	default:
		return "txt"
	}
}

// renderText 输出整理后的纯文本.
func renderText(w io.Writer, res *Result, opts Options) error {
	utterances := res.Utterances
	if len(utterances) == 0 {
		_, err := fmt.Fprintln(w, res.Text)
		return err
	}
	for _, u := range utterances {
		var b strings.Builder
		if opts.Timestamps {
			fmt.Fprintf(&b, "[%s - %s] ", FormatClock(u.Start), FormatClock(u.End))
		}
		if u.Speaker != "" && opts.SpeakerPrefix {
			fmt.Fprintf(&b, "%s: ", u.Speaker)
		}
		b.WriteString(u.Text)
		if _, err := fmt.Fprintln(w, b.String()); err != nil {
			return err
		}
	}
	return nil
}

// renderSRT 输出字幕文件.
func renderSRT(w io.Writer, res *Result, opts Options) error {
	cues := res.Utterances
	if len(cues) == 0 {
		if strings.TrimSpace(res.Text) == "" {
			return fmt.Errorf("结果为空, 无法生成字幕")
		}
		// 没有分段信息时退化成单条字幕.
		cues = []Utterance{{Start: 0, End: res.Duration, Text: res.Text}}
	}
	for i, u := range cues {
		end := u.End
		if end <= u.Start {
			end = u.Start + 2
		}
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n", i+1, FormatSRTTime(u.Start), FormatSRTTime(end)); err != nil {
			return err
		}
		text := u.Text
		if u.Speaker != "" && opts.SpeakerPrefix {
			text = u.Speaker + ": " + text
		}
		if _, err := fmt.Fprintf(w, "%s\n\n", text); err != nil {
			return err
		}
	}
	return nil
}

// renderJSON 透传服务端原始响应, 方便脚本继续处理.
func renderJSON(w io.Writer, res *Result) error {
	payload := res.Raw
	if len(payload) == 0 {
		fallback := map[string]any{"task": "transcribe", "text": res.Text}
		if len(res.Utterances) > 0 {
			segs := make([]map[string]any, 0, len(res.Utterances))
			for _, u := range res.Utterances {
				segs = append(segs, map[string]any{
					"start":   u.Start,
					"end":     u.End,
					"text":    u.Text,
					"speaker": u.Speaker,
				})
			}
			fallback["segments"] = segs
		}
		encoded, err := json.Marshal(fallback)
		if err != nil {
			return err
		}
		payload = encoded
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, payload, "", "  "); err != nil {
		// 不是合法 JSON 时原样输出, 至少不丢内容.
		_, werr := fmt.Fprintln(w, string(payload))
		return werr
	}
	_, err := fmt.Fprintln(w, buf.String())
	return err
}

// renderMarkdown 输出便于阅读与归档的 Markdown.
func renderMarkdown(w io.Writer, res *Result, opts Options) error {
	title := "音频转写"
	if res.Source != "" {
		title = strings.TrimSuffix(filepath.Base(res.Source), filepath.Ext(res.Source))
	}
	if _, err := fmt.Fprintf(w, "# %s\n\n", title); err != nil {
		return err
	}
	if res.Source != "" {
		if _, err := fmt.Fprintf(w, "- 源文件: `%s`\n", res.Source); err != nil {
			return err
		}
	}
	if res.Model != "" {
		if _, err := fmt.Fprintf(w, "- 模型: %s\n", res.Model); err != nil {
			return err
		}
	}
	if res.Duration > 0 {
		if _, err := fmt.Fprintf(w, "- 时长: %s\n", FormatClock(res.Duration)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	if len(res.Utterances) == 0 {
		_, err := fmt.Fprintf(w, "%s\n", res.Text)
		return err
	}
	hasSpeakers := res.HasSpeakers()
	for _, u := range res.Utterances {
		var meta []string
		if opts.SpeakerPrefix && hasSpeakers && u.Speaker != "" {
			meta = append(meta, "**"+u.Speaker+"**")
		}
		if opts.Timestamps {
			meta = append(meta, fmt.Sprintf("(%s - %s)", FormatClock(u.Start), FormatClock(u.End)))
		}
		if len(meta) > 0 {
			if _, err := fmt.Fprintf(w, "%s\n", strings.Join(meta, " ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s\n\n", u.Text); err != nil {
			return err
		}
	}
	return nil
}

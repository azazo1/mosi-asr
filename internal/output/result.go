// Package output 把转写结果整理成适合阅读与后续处理的文本形式.
//
// 支持 txt, text, srt, json, md 五种格式, 并负责结果文件路径的推导.
package output

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/azazo1/mosi-asr/internal/mossapi"
)

// Utterance 是一段按说话人整理后的发言.
type Utterance struct {
	Speaker string
	Start   float64
	End     float64
	Text    string
}

// Result 是最终呈现用的结果.
type Result struct {
	// Source 是输入音频路径, 使用 URL 或 file_id 时为空.
	Source string
	Model  string
	TaskID string
	// Duration 是音频时长, 服务端未返回时为 0.
	Duration float64
	// Text 是完整转写文本.
	Text string
	// Utterances 是按说话人整理后的分段, 未启用说话人识别时为空.
	Utterances []Utterance
	// Usage 是用量统计, 服务端未返回时为 nil.
	Usage *mossapi.Usage
	// Raw 是服务端原始响应体, json 格式直接透传.
	Raw json.RawMessage

	FinishedAt time.Time
}

// HasSpeakers 表示结果是否包含说话人信息.
func (r *Result) HasSpeakers() bool {
	if r == nil {
		return false
	}
	for _, u := range r.Utterances {
		if strings.TrimSpace(u.Speaker) != "" {
			return true
		}
	}
	return false
}

// New 把接口结果整理成呈现结果.
//
// mergeGap 表示同一说话人相邻分段间隔小于该秒数时合并成一段, 传 0 表示不合并.
func New(source string, tr *mossapi.Transcription, mergeGap float64) *Result {
	res := &Result{Source: source, FinishedAt: time.Now()}
	if tr == nil {
		return res
	}
	res.Text = strings.TrimSpace(tr.Text)
	res.Model = tr.Model
	res.TaskID = tr.TaskID
	res.Duration = tr.Duration
	res.Usage = tr.Usage
	res.Raw = tr.Raw
	res.Utterances = MergeSegments(tr.Segments, mergeGap)
	return res
}

// MergeSegments 把分段结果合并成发言列表.
func MergeSegments(segments []mossapi.Segment, mergeGap float64) []Utterance {
	var out []Utterance
	for _, seg := range segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			sameSpeaker := last.Speaker == seg.Speaker
			gap := seg.Start - last.End
			if sameSpeaker && (mergeGap <= 0 || gap <= mergeGap) {
				last.Text = joinText(last.Text, text)
				if seg.End > last.End {
					last.End = seg.End
				}
				continue
			}
		}
		out = append(out, Utterance{
			Speaker: seg.Speaker,
			Start:   seg.Start,
			End:     seg.End,
			Text:    text,
		})
	}
	return out
}

// joinText 拼接同一说话人的相邻文本.
// 中英文混排时按需补空格, 避免中文被空格割裂, 也避免英文单词粘连.
func joinText(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	lastRune := lastRuneOf(a)
	firstRune := firstRuneOf(b)
	if isASCIIWordRune(lastRune) && isASCIIWordRune(firstRune) {
		return a + " " + b
	}
	return a + b
}

func lastRuneOf(s string) rune {
	rs := []rune(s)
	if len(rs) == 0 {
		return 0
	}
	return rs[len(rs)-1]
}

func firstRuneOf(s string) rune {
	rs := []rune(s)
	if len(rs) == 0 {
		return 0
	}
	return rs[0]
}

func isASCIIWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return false
	}
}

// FormatClock 把秒数格式化成 MM:SS.mmm 或 HH:MM:SS.mmm.
func FormatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	ms := int((seconds - float64(total)) * 1000)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
	}
	return fmt.Sprintf("%02d:%02d.%03d", m, s, ms)
}

// FormatSRTTime 把秒数格式化成 SRT 要求的 HH:MM:SS,mmm.
func FormatSRTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	ms := int((seconds - float64(total)) * 1000)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

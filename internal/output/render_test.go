package output

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azazo1/mosi-asr/internal/mossapi"
)

func sampleTranscription() *mossapi.Transcription {
	raw, _ := json.Marshal(map[string]any{
		"task":     "transcribe",
		"duration": 12.5,
		"text":     "大家好今天开会你好",
		"segments": []map[string]any{
			{"start": 0.0, "end": 2.4, "text": "大家好", "speaker": "S01"},
			{"start": 2.5, "end": 4.0, "text": "今天开会", "speaker": "S01"},
			{"start": 8.0, "end": 12.5, "text": "你好", "speaker": "S02"},
		},
	})
	return &mossapi.Transcription{
		Task:     "transcribe",
		Duration: 12.5,
		Text:     "大家好今天开会你好",
		Segments: []mossapi.Segment{
			{Start: 0, End: 2.4, Text: "大家好", Speaker: "S01"},
			{Start: 2.5, End: 4.0, Text: "今天开会", Speaker: "S01"},
			{Start: 8.0, End: 12.5, Text: "你好", Speaker: "S02"},
		},
		Raw: raw,
	}
}

func TestMergeSegmentsRespectsGapAndSpeaker(t *testing.T) {
	utts := MergeSegments(sampleTranscription().Segments, 1.5)
	if len(utts) != 2 {
		t.Fatalf("合并结果数量异常: %+v", utts)
	}
	if utts[0].Speaker != "S01" || utts[0].Text != "大家好今天开会" {
		t.Errorf("同说话人相邻分段未合并: %+v", utts[0])
	}
	if utts[0].End != 4.0 {
		t.Errorf("合并后的结束时间异常: %v", utts[0].End)
	}
	if utts[1].Speaker != "S02" {
		t.Errorf("不同说话人被错误合并: %+v", utts[1])
	}

	// 间隔超过阈值时不应合并.
	utts = MergeSegments(sampleTranscription().Segments, 0.05)
	if len(utts) != 3 {
		t.Fatalf("间隔较大时不应合并: %+v", utts)
	}
}

func TestJoinTextBetweenASCIIWords(t *testing.T) {
	segs := []mossapi.Segment{
		{Text: "hello", Speaker: "S01", Start: 0, End: 1},
		{Text: "world", Speaker: "S01", Start: 1, End: 2},
	}
	utts := MergeSegments(segs, 1)
	if utts[0].Text != "hello world" {
		t.Fatalf("英文分词未补空格: %q", utts[0].Text)
	}
}

func TestRenderTextWithSpeakerAndTimestamps(t *testing.T) {
	res := New("/tmp/会议.mp3", sampleTranscription(), 1.5)
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Format: "txt", SpeakerPrefix: true, Timestamps: true}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "S01: 大家好今天开会") {
		t.Errorf("缺少说话人前缀: %s", out)
	}
	if !strings.Contains(out, "[00:08.000 - 00:12.500] S02: 你好") {
		t.Errorf("时间戳格式异常: %s", out)
	}
}

func TestRenderTextWithoutSegments(t *testing.T) {
	tr := &mossapi.Transcription{Text: "只有整段文本"}
	res := New("a.mp3", tr, 1)
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Format: "txt"}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "只有整段文本" {
		t.Fatalf("整段文本输出异常: %q", buf.String())
	}
}

func TestRenderSRTTimecodes(t *testing.T) {
	res := New("a.mp3", sampleTranscription(), 1.5)
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Format: "srt", SpeakerPrefix: true}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "1\n00:00:00,000 --> 00:00:04,000\nS01: 大家好今天开会") {
		t.Errorf("字幕时间轴异常: %s", out)
	}
	if !strings.Contains(out, "2\n00:00:08,000 --> 00:00:12,500\nS02: 你好") {
		t.Errorf("第二条字幕异常: %s", out)
	}
}

func TestRenderSRTWithoutSegmentsFallsBack(t *testing.T) {
	res := New("a.mp3", &mossapi.Transcription{Text: "整段文本", Duration: 30}, 1)
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Format: "srt"}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(buf.String(), "00:00:00,000 --> 00:00:30,000") {
		t.Fatalf("退化字幕异常: %s", buf.String())
	}
}

func TestRenderJSONKeepsRawResponse(t *testing.T) {
	res := New("a.mp3", sampleTranscription(), 1.5)
	var buf bytes.Buffer
	if err := Render(&buf, res, Options{Format: "json"}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if decoded["duration"] != 12.5 {
		t.Errorf("原始字段丢失: %v", decoded)
	}
}

func TestResolveTarget(t *testing.T) {
	dir := t.TempDir()
	target, err := ResolveTarget(filepath.Join(dir, "会议.m4a"), "", "", "srt", true)
	if err != nil {
		t.Fatalf("推导失败: %v", err)
	}
	if target.Path != filepath.Join(dir, "会议.srt") {
		t.Errorf("默认输出路径异常: %s", target.Path)
	}

	target, err = ResolveTarget(filepath.Join(dir, "会议.m4a"), "", filepath.Join(dir, "out"), "md", true)
	if err != nil {
		t.Fatalf("推导失败: %v", err)
	}
	if target.Path != filepath.Join(dir, "out", "会议.md") {
		t.Errorf("指定目录时输出路径异常: %s", target.Path)
	}

	target, err = ResolveTarget("audio.mp3", "-", "", "txt", true)
	if err != nil || !target.Stdout {
		t.Fatalf("标准输出目标异常: %+v %v", target, err)
	}
}

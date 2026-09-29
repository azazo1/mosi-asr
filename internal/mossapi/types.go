// Package mossapi 封装 Moss API 的音频转写接口.
//
// 覆盖三种调用方式:
//   - 同步: multipart 或 JSON 请求, 直接返回转写结果;
//   - 流式: 追加 stream=true, 通过 SSE 持续接收增量文本;
//   - 异步: 追加 async=true, 返回 task_id, 再用查询任务接口取结果.
//
// 接口文档: https://platform.mosi.cn/docs/scenarios/transcribe-diarization
package mossapi

import (
	"encoding/json"
	"fmt"
)

// 支持的模型 ID.
const (
	// ModelTranscribe 是普通转写的稳定模型.
	ModelTranscribe = "moss-transcribe-1.0"
	// ModelDiarizePro 支持多说话人分离, 热词与 SSE 流式输出.
	ModelDiarizePro = "moss-transcribe-diarize-pro"
)

// response_format 取值.
const (
	FormatJSON         = "json"
	FormatText         = "text"
	FormatDiarizedJSON = "diarized_json"
	FormatVerboseJSON  = "verbose_json"
)

// 异步任务状态.
const (
	StatusPending    = "PENDING"
	StatusProcessing = "PROCESSING"
	StatusSuccess    = "SUCCESS"
	StatusFailed     = "FAILED"
)

// SSE 事件类型.
const (
	EventTaskCreated = "task.created"
	EventTextDelta   = "transcript.text.delta"
	EventSegmentDone = "transcript.segment.done"
	EventTextDone    = "transcript.text.done"
	EventError       = "error"
)

// MaxUploadBytes 是服务端允许的单文件大小上限.
const MaxUploadBytes = 512 << 20

// 热词限制, 与服务端约定保持一致.
const (
	// MaxKeyterms 是热词数量上限.
	MaxKeyterms = 20
	// MaxKeytermRunes 是单个热词的长度上限, 按字符数计算.
	MaxKeytermRunes = 30
)

// TranscribeRequest 描述一次转写请求.
// FilePath, FileID, URL 三者只能设置一个.
type TranscribeRequest struct {
	// Model 为空时由调用方按是否启用说话人识别选择.
	Model string
	// FilePath 是本地音视频文件路径, 走 multipart 上传.
	FilePath string
	// FileID 是 POST /v1/files 返回的文件 ID.
	FileID string
	// URL 是服务端可访问的公网音频地址.
	URL string

	Diarize        bool
	Keyterms       []string
	ResponseFormat string
	Async          bool
	Stream         bool
	WebhookURL     string

	// UploadProgress 在上传过程中被周期性调用, 用于输出进度, 不需要时留空.
	// 它只影响本地回调, 不会作为参数发给服务端.
	UploadProgress func(sent, total int64)
}

// Segment 是一段带时间轴的转写结果.
type Segment struct {
	Type    string  `json:"type,omitempty"`
	ID      string  `json:"id,omitempty"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker,omitempty"`
}

// Usage 是转写的用量统计.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Transcription 是同步与流式转写的最终结果.
type Transcription struct {
	Task     string    `json:"task,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text"`
	Segments []Segment `json:"segments,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`

	// Raw 保存服务端原始响应体, json 输出格式直接透传, 避免字段丢失.
	Raw json.RawMessage `json:"-"`
	// Model 记录本次实际使用的模型, 日志与展示用.
	Model string `json:"-"`
	// TaskID 在流式或异步调用时记录任务 ID.
	TaskID string `json:"-"`
}

// HasSegments 表示结果是否包含说话人分段.
func (t *Transcription) HasSegments() bool {
	return t != nil && len(t.Segments) > 0
}

// Task 是异步任务对象, 成功时结果字段位于顶层.
type Task struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"task_id"`
	Object         string          `json:"object"`
	Status         string          `json:"status"`
	ModelID        string          `json:"model_id"`
	RetryAfter     int             `json:"retry_after"`
	CreatedAt      int64           `json:"created_at"`
	UpdatedAt      int64           `json:"updated_at"`
	Error          *TaskError      `json:"error"`
	Text           string          `json:"text"`
	Segments       []Segment       `json:"segments"`
	Usage          *Usage          `json:"usage"`
	URL            string          `json:"url"`
	ResponseFormat string          `json:"response_format"`
	ContentType    string          `json:"content_type"`
	Raw            json.RawMessage `json:"-"`
}

// TaskKey 返回用于查询与展示的任务 ID, id 与 task_id 通常相同.
func (t *Task) TaskKey() string {
	if t == nil {
		return ""
	}
	if t.TaskID != "" {
		return t.TaskID
	}
	return t.ID
}

// ToTranscription 把完成的任务转成统一的转写结果.
func (t *Task) ToTranscription() *Transcription {
	if t == nil {
		return nil
	}
	return &Transcription{
		Text:     t.Text,
		Segments: t.Segments,
		Usage:    t.Usage,
		Raw:      t.Raw,
		Model:    t.ModelID,
		TaskID:   t.TaskKey(),
	}
}

// TaskError 是异步任务失败信息.
type TaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Error 实现 error 接口.
func (e *TaskError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	default:
		return "任务失败"
	}
}

// StreamEvent 是 SSE 事件, 不同事件的字段按需填充.
type StreamEvent struct {
	Type         string  `json:"type"`
	TaskID       string  `json:"task_id"`
	Object       string  `json:"object"`
	Status       string  `json:"status"`
	Model        string  `json:"model"`
	Delta        string  `json:"delta"`
	ContentIndex int     `json:"content_index"`
	Speaker      string  `json:"speaker"`
	Start        float64 `json:"start"`
	End          float64 `json:"end"`
	Text         string  `json:"text"`
	Usage        *Usage  `json:"usage"`
	Raw          string  `json:"-"`
}

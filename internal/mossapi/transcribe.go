package mossapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Transcribe 发起同步转写并返回最终结果.
func (c *Client) Transcribe(ctx context.Context, req TranscribeRequest) (*Transcription, error) {
	req.Async = false
	req.Stream = false
	req = req.withDefaults()

	resp, err := c.do(ctx, "转写", func(ctx context.Context) (*http.Request, error) {
		return c.buildTranscribeRequest(ctx, req, false)
	}, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取转写结果失败: %w", err)
	}

	// response_format=text 时响应体就是纯文本, 不是 JSON 对象.
	if req.ResponseFormat == FormatText {
		return &Transcription{Text: strings.TrimSpace(string(body)), Model: req.Model}, nil
	}

	out := &Transcription{}
	if err := json.Unmarshal(body, out); err != nil {
		// 兼容服务端未按 JSON 返回的情况, 尽量保留可用内容.
		text := strings.TrimSpace(string(body))
		if text == "" {
			return nil, fmt.Errorf("解析转写结果失败: %w", err)
		}
		return &Transcription{Text: text, Model: req.Model}, nil
	}
	out.Raw = json.RawMessage(body)
	out.Model = req.Model
	return out, nil
}

// CreateTask 发起异步转写, 返回 task_id.
func (c *Client) CreateTask(ctx context.Context, req TranscribeRequest) (*Task, error) {
	req.Async = true
	req.Stream = false
	req = req.withDefaults()

	resp, err := c.do(ctx, "创建转写任务", func(ctx context.Context) (*http.Request, error) {
		return c.buildTranscribeRequest(ctx, req, false)
	}, false)
	if err != nil {
		return nil, err
	}
	task := &Task{}
	body, err := readJSON(resp, task)
	if err != nil {
		return nil, err
	}
	task.Raw = json.RawMessage(body)
	c.log.Debug("任务已创建", "task_id", task.TaskKey(), "status", task.Status)
	return task, nil
}

// StreamResult 是流式转写的回调集合, 未设置的回调会被忽略.
type StreamResult struct {
	// OnCreated 在任务创建事件到达时调用.
	OnCreated func(taskID string)
	// OnDelta 在增量文本到达时调用, 参数是本次新增的片段.
	OnDelta func(delta string)
	// OnSegment 在分段完成事件到达时调用.
	OnSegment func(seg Segment)
}

// TranscribeStream 以 SSE 方式转写, 边接收边回调, 最后返回汇总结果.
func (c *Client) TranscribeStream(ctx context.Context, req TranscribeRequest, handlers StreamResult) (*Transcription, error) {
	req.Stream = true
	req.Async = false
	req = req.withDefaults()
	// 流式响应固定为 SSE, 显式指定 verbose_json 以便拿到分段与用量.
	if req.ResponseFormat == FormatJSON || req.ResponseFormat == FormatDiarizedJSON {
		req.ResponseFormat = FormatVerboseJSON
	}

	resp, err := c.do(ctx, "流式转写", func(ctx context.Context) (*http.Request, error) {
		return c.buildTranscribeRequest(ctx, req, true)
	}, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	final := &Transcription{Model: req.Model}
	var deltaBuf strings.Builder
	var segments []Segment

	err = parseSSE(resp.Body, func(ev StreamEvent) error {
		switch ev.Type {
		case EventTaskCreated:
			final.TaskID = ev.TaskID
			c.log.Debug("流式任务已创建", "task_id", ev.TaskID, "model", ev.Model)
			if handlers.OnCreated != nil {
				handlers.OnCreated(ev.TaskID)
			}
		case EventTextDelta:
			deltaBuf.WriteString(ev.Delta)
			if handlers.OnDelta != nil && ev.Delta != "" {
				handlers.OnDelta(ev.Delta)
			}
		case EventSegmentDone:
			seg := Segment{Type: ev.Type, Start: ev.Start, End: ev.End, Text: ev.Text, Speaker: ev.Speaker}
			segments = append(segments, seg)
			if handlers.OnSegment != nil {
				handlers.OnSegment(seg)
			}
		case EventTextDone:
			final.Text = ev.Text
			final.Usage = ev.Usage
		case EventError:
			return fmt.Errorf("流式转写出错: %s", strings.TrimSpace(ev.Text))
		default:
			c.log.Debug("忽略未知的流式事件", "type", ev.Type, "raw", truncate(ev.Raw, 200))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if final.Text == "" {
		final.Text = deltaBuf.String()
	}
	final.Segments = segments
	if len(segments) > 0 {
		final.Raw = buildRawFromStream(final)
	}
	return final, nil
}

// buildRawFromStream 把流式结果还原成与同步接口一致的 JSON, 供 json 输出格式复用.
func buildRawFromStream(t *Transcription) json.RawMessage {
	payload := map[string]any{
		"task":     "transcribe",
		"text":     t.Text,
		"segments": t.Segments,
	}
	if t.Usage != nil {
		payload["usage"] = t.Usage
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return b
}

// withDefaults 补齐模型与输出格式.
// 只在使用方没有显式指定模型时才自动选择, 避免覆盖用户的明确选择.
func (r TranscribeRequest) withDefaults() TranscribeRequest {
	if r.Model == "" {
		switch {
		case r.Stream, r.Diarize, len(r.Keyterms) > 0:
			// 流式输出与热词都只支持多说话人模型.
			r.Model = ModelDiarizePro
		default:
			r.Model = ModelTranscribe
		}
	}
	if r.Diarize && r.Model == ModelTranscribe {
		// 普通模型不支持说话人分离, 自动切换到多说话人模型.
		r.Model = ModelDiarizePro
	}
	if r.ResponseFormat == "" {
		r.ResponseFormat = FormatJSON
	}
	if r.Stream {
		r.ResponseFormat = FormatVerboseJSON
	}
	return r
}

// validate 校验输入源与参数组合.
func (r TranscribeRequest) validate() error {
	sources := 0
	for _, v := range []string{r.FilePath, r.FileID, r.URL} {
		if strings.TrimSpace(v) != "" {
			sources++
		}
	}
	switch {
	case sources == 0:
		return errors.New("必须提供音频文件, file_id 或 url 之一")
	case sources > 1:
		return errors.New("音频文件, file_id, url 三者只能填一个")
	}
	if r.Stream && r.Async {
		return errors.New("stream 与 async 不能同时使用")
	}
	if r.Stream && r.Model != ModelDiarizePro {
		return fmt.Errorf("流式输出只支持 %s 模型", ModelDiarizePro)
	}
	if r.Stream && r.ResponseFormat == FormatText {
		return errors.New("流式输出不支持 response_format=text")
	}
	if len(r.Keyterms) > 0 && r.Model != ModelDiarizePro {
		return fmt.Errorf("热词只支持 %s 模型", ModelDiarizePro)
	}
	if len(r.Keyterms) > MaxKeyterms {
		return fmt.Errorf("热词最多 %d 个", MaxKeyterms)
	}
	for _, k := range r.Keyterms {
		if len([]rune(k)) > MaxKeytermRunes {
			return fmt.Errorf("热词 %q 超过 %d 个字符", k, MaxKeytermRunes)
		}
	}
	if r.FilePath != "" {
		if err := checkLocalFile(r.FilePath); err != nil {
			return err
		}
	}
	return nil
}

// checkLocalFile 在上传前确认文件存在, 可读且未超过大小上限.
func checkLocalFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("无法访问音频文件 %s: %w", path, err)
	}
	if st.IsDir() {
		return fmt.Errorf("%s 是目录, 不是音频文件", path)
	}
	if st.Size() == 0 {
		return fmt.Errorf("音频文件 %s 是空文件", path)
	}
	if st.Size() > MaxUploadBytes {
		return fmt.Errorf("音频文件 %.1f MB 超过服务端 %d MB 上限", float64(st.Size())/(1<<20), MaxUploadBytes>>20)
	}
	return nil
}

// buildTranscribeRequest 构造一次请求, 每次调用都会重新打开文件.
func (c *Client) buildTranscribeRequest(ctx context.Context, req TranscribeRequest, stream bool) (*http.Request, error) {
	if err := req.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", errRequestBody, err)
	}

	if req.FilePath != "" {
		body, err := newMultipartBody(requestFields(req), req.FilePath, 0, req.UploadProgress)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/audio/transcriptions"), body.reader)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errRequestBody, err)
		}
		httpReq.Header.Set("Content-Type", body.contentType)
		// 显式给出长度, 避免退化成 chunked 上传.
		httpReq.ContentLength = body.length
		if stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		}
		return httpReq, nil
	}

	payload := map[string]any{
		"model":           req.Model,
		"response_format": req.ResponseFormat,
	}
	if req.Diarize {
		payload["diarize"] = true
	}
	if len(req.Keyterms) > 0 {
		payload["keyterms"] = req.Keyterms
	}
	switch {
	case req.FileID != "":
		payload["file_id"] = req.FileID
	case req.URL != "":
		payload["url"] = req.URL
	}
	if req.Async {
		payload["async"] = true
	}
	if req.Stream {
		payload["stream"] = true
	}
	if req.WebhookURL != "" {
		payload["webhook_url"] = req.WebhookURL
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRequestBody, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/audio/transcriptions"), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRequestBody, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	c.log.Debug("请求体", "body", string(raw))
	return httpReq, nil
}

// parseSSE 逐个读取 SSE 事件, 忽略注释与心跳行.
func parseSSE(r io.Reader, handle func(StreamEvent) error) error {
	reader := newSSEReader(r)
	for {
		event, err := reader.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if event == nil {
			continue
		}
		if err := handle(*event); err != nil {
			return err
		}
	}
}

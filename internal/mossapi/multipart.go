package mossapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// formField 是 multipart 表单字段.
type formField struct {
	name  string
	value string
}

// requestFields 生成 multipart 表单字段, 顺序与接口文档示例保持一致.
func requestFields(req TranscribeRequest) []formField {
	fields := []formField{
		{name: "model", value: req.Model},
		{name: "response_format", value: req.ResponseFormat},
	}
	if req.Diarize {
		fields = append(fields, formField{name: "diarize", value: "true"})
	}
	if len(req.Keyterms) > 0 {
		// 文档要求 multipart 下热词以 JSON 字符串数组传入.
		if raw, err := json.Marshal(req.Keyterms); err == nil {
			fields = append(fields, formField{name: "keyterms", value: string(raw)})
		}
	}
	if req.Async {
		fields = append(fields, formField{name: "async", value: "true"})
	}
	if req.Stream {
		fields = append(fields, formField{name: "stream", value: "true"})
	}
	if req.WebhookURL != "" {
		fields = append(fields, formField{name: "webhook_url", value: req.WebhookURL})
	}
	return fields
}

// countingWriter 只统计写入字节数, 不保留内容.
type countingWriter struct {
	n int64
}

// Write 实现 io.Writer.
func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// multipartBody 描述一次 multipart 请求体.
type multipartBody struct {
	reader      io.ReadCloser
	contentType string
	length      int64
}

// newMultipartBody 以流式方式构造 multipart 请求体, 避免把大文件整体读进内存.
//
// 返回的 length 是请求体的精确字节数, 调用方需要把它写进 Content-Length,
// 否则 Go 会退化成 chunked 传输, 部分服务端与网关对 chunked 上传支持不佳.
//
// progress 非空时按固定间隔回调上传进度, 主要用于长音频上传时的可见性.
func newMultipartBody(fields []formField, filePath string, size int64,
	progress func(sent, total int64)) (*multipartBody, error) {
	st, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法访问音频文件 %s: %v", errRequestBody, filePath, err)
	}
	if size <= 0 {
		size = st.Size()
	}
	filename := filepath.Base(filePath)

	// 先空跑一次, 求出 boundary 与文件内容之前/之后的固定开销.
	boundary := multipart.NewWriter(io.Discard).Boundary()
	length, err := multipartLength(boundary, fields, filename, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRequestBody, err)
	}

	pr, pw := io.Pipe()
	go func() {
		if err := writeMultipart(pw, boundary, fields, filePath, filename); err != nil {
			pw.CloseWithError(fmt.Errorf("%w: %v", errRequestBody, err))
			return
		}
		pw.Close()
	}()

	var reader io.ReadCloser = pr
	if progress != nil {
		reader = newProgressReader(pr, size, progress)
	}
	return &multipartBody{
		reader:      reader,
		contentType: "multipart/form-data; boundary=" + boundary,
		length:      length,
	}, nil
}

// writeMultipart 把字段与文件依次写入 w, 顺序必须与 multipartLength 的试算一致.
func writeMultipart(w io.Writer, boundary string, fields []formField, filePath, filename string) error {
	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(boundary); err != nil {
		return err
	}
	for _, f := range fields {
		if err := mw.WriteField(f.name, f.value); err != nil {
			return fmt.Errorf("写入表单字段 %s 失败: %w", f.name, err)
		}
	}

	fh, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开音频文件失败: %w", err)
	}
	defer fh.Close()

	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, fh); err != nil {
		return fmt.Errorf("读取音频文件失败: %w", err)
	}
	return mw.Close()
}

// multipartLength 试算请求体总长度.
//
// 做法是用同样的字段与文件名走一遍写入流程, 但文件内容不写, 只统计字节数;
// 文件部分是按原样写入的, 因此总长度等于 "固定开销 + 文件大小".
func multipartLength(boundary string, fields []formField, filename string, fileSize int64) (int64, error) {
	counter := &countingWriter{}
	mw := multipart.NewWriter(counter)
	if err := mw.SetBoundary(boundary); err != nil {
		return 0, err
	}
	for _, f := range fields {
		if err := mw.WriteField(f.name, f.value); err != nil {
			return 0, err
		}
	}
	if _, err := mw.CreateFormFile("file", filename); err != nil {
		return 0, err
	}
	if err := mw.Close(); err != nil {
		return 0, err
	}
	return counter.n + fileSize, nil
}

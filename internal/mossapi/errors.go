package mossapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError 是服务端返回的错误, 附带状态码与错误码.
type APIError struct {
	StatusCode int
	Code       string
	Type       string
	Message    string
	RequestID  string
	// Body 是原始响应体, 便于排查未识别的错误结构.
	Body string
}

// Error 实现 error 接口, 并给出针对性的处理建议.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "接口返回 HTTP %d", e.StatusCode)
	switch {
	case e.Code != "" && e.Message != "":
		fmt.Fprintf(&b, " (%s): %s", e.Code, e.Message)
	case e.Code != "":
		fmt.Fprintf(&b, " (%s)", e.Code)
	case e.Message != "":
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if hint := HintForCode(e.Code, e.StatusCode); hint != "" {
		b.WriteString(", ")
		b.WriteString(hint)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " [request_id=%s]", e.RequestID)
	}
	return b.String()
}

// HintForCode 针对常见错误码给出中文处理建议.
func HintForCode(code string, status int) string {
	switch code {
	case "authentication_error", "permission_error":
		return "请检查 API Key 是否有效, 可用 mosi-asr config show 查看当前来源"
	case "billing_error", "insufficient_credits":
		return "账户余额不足, 请到 Moss 平台充值"
	case "rate_limit_error", "rate_limit_exceeded":
		return "请求过于频繁, 稍后重试或降低并发"
	case "concurrency_limit_exceeded":
		return "并发的异步任务过多, 等待已有任务结束后重试"
	case "invalid_input_source":
		return "四种输入源只能选一个, 且不支持 Base64 内联音频"
	case "unsupported_stream":
		return "流式输出只支持 moss-transcribe-diarize-pro 模型"
	case "unsupported_with_async":
		return "stream 与 async 不能同时使用, 二者选一"
	case "invalid_keyterms":
		return fmt.Sprintf("热词最多 %d 个, 每个最多 %d 个字符", MaxKeyterms, MaxKeytermRunes)
	case "task_not_found":
		return "任务不存在或无权限, 请确认 task_id 是否正确"
	case "url_not_allowed", "invalid_url":
		return "音频 URL 必须是服务端可访问的公网地址"
	case "safety_guardrail_blocked":
		return "内容被安全策略拦截"
	}
	switch status {
	case http.StatusTooManyRequests:
		return "请求过于频繁, 稍后重试"
	case http.StatusPaymentRequired:
		return "账户余额不足"
	case http.StatusUnauthorized:
		return "鉴权失败, 请检查 API Key"
	case http.StatusForbidden:
		return "无权访问, 请检查 API Key 与账号权限"
	}
	return ""
}

// IsRetryable 判断错误是否值得重试.
func IsRetryable(err error) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// errorEnvelope 兼容服务端可能出现的几种错误结构.
type errorEnvelope struct {
	Error     json.RawMessage `json:"error"`
	Code      string          `json:"code"`
	Type      string          `json:"type"`
	Msg       string          `json:"message"`
	Detail    json.RawMessage `json:"detail"`
	RequestID string          `json:"request_id"`
}

type errorBody struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// parseAPIError 从响应体解析错误信息, 解析不出结构时保留原始文本.
func parseAPIError(status int, body []byte, header http.Header) *APIError {
	err := &APIError{
		StatusCode: status,
		Body:       truncate(string(body), 600),
	}
	if header != nil {
		err.RequestID = firstNonEmpty(header.Get("x-request-id"), header.Get("request-id"))
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		err.Message = http.StatusText(status)
		return err
	}
	if !strings.HasPrefix(trimmed, "{") {
		err.Message = truncate(trimmed, 300)
		return err
	}

	var env errorEnvelope
	if jerr := json.Unmarshal(body, &env); jerr != nil {
		err.Message = truncate(trimmed, 300)
		return err
	}
	if env.RequestID != "" {
		err.RequestID = env.RequestID
	}

	if len(env.Error) > 0 {
		// error 可能是对象, 也可能是字符串.
		var obj errorBody
		if jerr := json.Unmarshal(env.Error, &obj); jerr == nil && (obj.Code != "" || obj.Message != "" || obj.Type != "") {
			err.Code = obj.Code
			err.Type = obj.Type
			err.Message = obj.Message
			return err
		}
		var s string
		if jerr := json.Unmarshal(env.Error, &s); jerr == nil {
			err.Message = s
			return err
		}
	}
	err.Code = firstNonEmpty(env.Code, codeFromDetail(env.Detail))
	err.Type = env.Type
	err.Message = env.Msg
	if err.Message == "" {
		err.Message = codeFromDetail(env.Detail)
	}
	if err.Message == "" && err.Code == "" {
		err.Message = truncate(trimmed, 300)
	}
	return err
}

func codeFromDetail(detail json.RawMessage) string {
	if len(detail) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(detail, &s); err == nil {
		return s
	}
	var obj struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(detail, &obj); err == nil {
		return firstNonEmpty(obj.Code, obj.Message)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}

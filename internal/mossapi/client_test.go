package mossapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/mosi-asr/internal/logger"
)

// newTestClient 构造指向测试服务器的客户端.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := New(Options{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Timeout: 5 * time.Second,
		Logger:  logger.Discard(),
	})
	return client, srv
}

// writeTempAudio 生成一个占位音频文件.
func writeTempAudio(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.mp3")
	if err := os.WriteFile(path, []byte("fake-audio-bytes"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	return path
}

func TestTranscribeSyncParsesSegments(t *testing.T) {
	var gotAuth string
	var gotBody string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
		}
		gotBody = fmt.Sprintf("model=%s diarize=%s format=%s",
			r.FormValue("model"), r.FormValue("diarize"), r.FormValue("response_format"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"task":"transcribe","duration":5.18,"text":"你好世界",
			"segments":[{"start":0.1,"end":2.0,"text":"你好","speaker":"S01"},
			            {"start":2.0,"end":5.18,"text":"世界","speaker":"S01"}]}`)
	})

	tr, err := client.Transcribe(context.Background(), TranscribeRequest{
		FilePath: writeTempAudio(t),
		Diarize:  true,
	})
	if err != nil {
		t.Fatalf("转写失败: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization 头不正确: %q", gotAuth)
	}
	if !strings.Contains(gotBody, "model=moss-transcribe-diarize-pro") {
		t.Errorf("启用说话人识别时未切换到多说话人模型: %s", gotBody)
	}
	if !strings.Contains(gotBody, "diarize=true") {
		t.Errorf("未传递 diarize 参数: %s", gotBody)
	}
	if tr.Text != "你好世界" || len(tr.Segments) != 2 {
		t.Fatalf("转写结果解析异常: %+v", tr)
	}
	if len(tr.Raw) == 0 {
		t.Error("未保留原始响应体")
	}
}

func TestTranscribePlainTextResponse(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "纯文本结果")
	})

	tr, err := client.Transcribe(context.Background(), TranscribeRequest{
		FilePath:       writeTempAudio(t),
		ResponseFormat: FormatText,
	})
	if err != nil {
		t.Fatalf("转写失败: %v", err)
	}
	if tr.Text != "纯文本结果" {
		t.Fatalf("纯文本结果未正确解析: %q", tr.Text)
	}
}

func TestTranscribeAsyncTaskFlow(t *testing.T) {
	polls := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/audio/transcriptions"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("解析 multipart 失败: %v", err)
			}
			if r.FormValue("async") != "true" {
				t.Errorf("异步请求未带 async=true")
			}
			fmt.Fprint(w, `{"id":"task-1","task_id":"task-1","object":"audio.transcription","status":"PENDING","retry_after":1}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/v1/audio/tasks/task-1"):
			polls++
			if polls < 2 {
				fmt.Fprint(w, `{"id":"task-1","status":"PROCESSING"}`)
				return
			}
			fmt.Fprint(w, `{"id":"task-1","status":"SUCCESS","object":"audio.transcription",
				"text":"完成了","segments":[{"start":0,"end":1,"text":"完成了","speaker":"S02"}]}`)
		default:
			t.Errorf("未预期的请求: %s %s", r.Method, r.URL.Path)
		}
	})

	task, err := client.CreateTask(context.Background(), TranscribeRequest{FilePath: writeTempAudio(t)})
	if err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}
	if task.TaskKey() != "task-1" {
		t.Fatalf("任务 ID 解析异常: %+v", task)
	}

	done, err := client.WaitTask(context.Background(), task.TaskKey(), 10*time.Millisecond, 3*time.Second, nil)
	if err != nil {
		t.Fatalf("等待任务失败: %v", err)
	}
	if done.Status != StatusSuccess || done.Text != "完成了" {
		t.Fatalf("任务结果异常: %+v", done)
	}
	if polls < 2 {
		t.Errorf("轮询次数异常: %d", polls)
	}
}

func TestWaitTaskFailed(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"task-2","status":"FAILED","error":{"code":"upstream_error","message":"上游超时"}}`)
	})
	_, err := client.WaitTask(context.Background(), "task-2", time.Millisecond, time.Second, nil)
	if err == nil || !strings.Contains(err.Error(), "上游超时") {
		t.Fatalf("任务失败信息未透出: %v", err)
	}
}

func TestTranscribeStreamCollectsEvents(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
		}
		if r.FormValue("stream") != "true" {
			t.Errorf("流式请求未带 stream=true")
		}
		if r.FormValue("response_format") != FormatVerboseJSON {
			t.Errorf("流式请求的 response_format 不正确: %s", r.FormValue("response_format"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": 心跳\n\n")
		fmt.Fprint(w, "data: {\"type\":\"task.created\",\"task_id\":\"t-9\",\"model\":\"moss-transcribe-diarize-pro\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"你\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"好\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.segment.done\",\"start\":0.2,\"end\":1.8,\"text\":\"你好\",\"speaker\":\"S01\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.done\",\"text\":\"你好\",\"usage\":{\"total_tokens\":12}}\n\n")
	})

	var deltas []string
	tr, err := client.TranscribeStream(context.Background(), TranscribeRequest{FilePath: writeTempAudio(t)},
		StreamResult{OnDelta: func(d string) { deltas = append(deltas, d) }})
	if err != nil {
		t.Fatalf("流式转写失败: %v", err)
	}
	if tr.Text != "你好" || tr.TaskID != "t-9" {
		t.Fatalf("流式结果异常: %+v", tr)
	}
	if strings.Join(deltas, "") != "你好" {
		t.Fatalf("增量文本异常: %v", deltas)
	}
	if len(tr.Segments) != 1 || tr.Segments[0].Speaker != "S01" {
		t.Fatalf("分段解析异常: %+v", tr.Segments)
	}
	if tr.Usage == nil || tr.Usage.TotalTokens != 12 {
		t.Fatalf("用量解析异常: %+v", tr.Usage)
	}
}

func TestAPIErrorParsed(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req-abc")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"authentication_error","message":"invalid key","type":"auth"}}`)
	})

	_, err := client.Transcribe(context.Background(), TranscribeRequest{FilePath: writeTempAudio(t)})
	if err == nil {
		t.Fatal("预期返回错误")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("错误类型异常: %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Code != "authentication_error" {
		t.Fatalf("错误信息解析异常: %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "API Key") {
		t.Errorf("错误提示缺少处理建议: %s", apiErr.Error())
	}
	if apiErr.RequestID != "req-abc" {
		t.Errorf("request id 未解析: %q", apiErr.RequestID)
	}
}

func TestRetryOnServerError(t *testing.T) {
	attempts := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"code":"service_unavailable","message":"稍后重试"}}`)
			return
		}
		fmt.Fprint(w, `{"text":"重试成功"}`)
	})
	client.maxRetries = 1

	tr, err := client.Transcribe(context.Background(), TranscribeRequest{FilePath: writeTempAudio(t)})
	if err != nil {
		t.Fatalf("重试后仍失败: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("重试次数异常: %d", attempts)
	}
	if tr.Text != "重试成功" {
		t.Fatalf("结果异常: %+v", tr)
	}
}

func TestValidateRejectsBadCombination(t *testing.T) {
	audio := writeTempAudio(t)
	cases := []TranscribeRequest{
		{},
		{FilePath: audio, URL: "https://example.com/a.mp3"},
		{FilePath: audio, Stream: true, Async: true},
		{FilePath: audio, Stream: true, Model: ModelTranscribe},
		{FilePath: audio, Keyterms: []string{"MOSS"}, Model: ModelTranscribe},
	}
	for i, req := range cases {
		if err := req.withDefaults().validate(); err == nil {
			t.Errorf("第 %d 个用例应当报错", i+1)
		}
	}
}

func TestWithDefaultsPicksModel(t *testing.T) {
	cases := []struct {
		name string
		req  TranscribeRequest
		want string
	}{
		{"默认用普通模型", TranscribeRequest{}, ModelTranscribe},
		{"说话人识别用多说话人模型", TranscribeRequest{Diarize: true}, ModelDiarizePro},
		{"流式用多说话人模型", TranscribeRequest{Stream: true}, ModelDiarizePro},
		{"热词用多说话人模型", TranscribeRequest{Keyterms: []string{"MOSS"}}, ModelDiarizePro},
		{"显式指定优先", TranscribeRequest{Model: "custom-model"}, "custom-model"},
	}
	for _, c := range cases {
		if got := c.req.withDefaults().Model; got != c.want {
			t.Errorf("%s: 期望 %s, 实际 %s", c.name, c.want, got)
		}
	}
}

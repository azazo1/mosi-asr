package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv 准备一个指向本地模拟服务的命令行环境.
type testEnv struct {
	app     *App
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	cfgPath string
	workDir string
}

// newTestEnv 创建临时工作目录与配置文件.
func newTestEnv(t *testing.T, baseURL string) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	cfg := fmt.Sprintf(`version = 1

[api]
base_url = %q
api_key = "test-key"
timeout_seconds = 10
max_retries = 0

[transcribe]
poll_interval_seconds = 1
poll_timeout_seconds = 10

[log]
level = "error"
`, baseURL)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	app := New("test")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Stdout = stdout
	app.Stderr = stderr
	return &testEnv{app: app, stdout: stdout, stderr: stderr, cfgPath: cfgPath, workDir: dir}
}

// audioFile 生成一个占位音频文件.
func (e *testEnv) audioFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(e.workDir, name)
	if err := os.WriteFile(path, []byte("fake-audio"), 0o600); err != nil {
		t.Fatalf("写入音频失败: %v", err)
	}
	return path
}

func TestTranscribeWritesSRT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("请求路径异常: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
		}
		if r.FormValue("diarize") != "true" {
			t.Errorf("未启用说话人识别: %s", r.FormValue("diarize"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"task":"transcribe","duration":6,"text":"甲说了话乙回了话",
			"segments":[{"start":0,"end":3,"text":"甲说了话","speaker":"S01"},
			            {"start":3,"end":6,"text":"乙回了话","speaker":"S02"}]}`)
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	audio := env.audioFile(t, "会议.mp3")
	// 参数写在文件名之后, 验证参数重排.
	code := env.app.Run([]string{"-d", "--config", env.cfgPath, audio, "-f", "srt"})
	if code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}

	out := filepath.Join(env.workDir, "会议.srt")
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("未生成字幕文件: %v", err)
	}
	if !strings.Contains(string(content), "00:00:00,000 --> 00:00:03,000") {
		t.Errorf("字幕内容异常: %s", content)
	}
	if !strings.Contains(string(content), "S02: 乙回了话") {
		t.Errorf("说话人信息丢失: %s", content)
	}
}

func TestTranscribeToStdoutWithTimestamps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"普通文本结果"}`)
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	audio := env.audioFile(t, "语音.mp3")
	code := env.app.Run([]string{"--config", env.cfgPath, audio, "-o", "-"})
	if code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}
	if strings.TrimSpace(env.stdout.String()) != "普通文本结果" {
		t.Fatalf("标准输出内容异常: %q", env.stdout.String())
	}
}

func TestTranscribeAsyncMode(t *testing.T) {
	var sawAsync bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("解析 multipart 失败: %v", err)
			}
			sawAsync = r.FormValue("async") == "true"
			fmt.Fprint(w, `{"id":"t1","task_id":"t1","status":"PENDING"}`)
		case strings.Contains(r.URL.Path, "/v1/audio/tasks/"):
			fmt.Fprint(w, `{"id":"t1","status":"SUCCESS","text":"异步结果"}`)
		}
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	audio := env.audioFile(t, "长录音.mp3")
	code := env.app.Run([]string{"--config", env.cfgPath, "--async", audio, "-o", "-"})
	if code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}
	if !sawAsync {
		t.Error("未使用异步任务")
	}
	if !strings.Contains(env.stdout.String(), "异步结果") {
		t.Fatalf("异步结果未输出: %q", env.stdout.String())
	}
}

func TestTranscribeStreamEchoesDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"task.created\",\"task_id\":\"s1\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"流式\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"结果\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.done\",\"text\":\"流式结果\"}\n\n")
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	audio := env.audioFile(t, "直播.m4a")
	code := env.app.Run([]string{"--config", env.cfgPath, "-s", audio, "-o", "-"})
	if code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}
	if !strings.Contains(env.stdout.String(), "流式结果") {
		t.Fatalf("流式文本未输出: %q", env.stdout.String())
	}
}

func TestTaskCommandQueriesResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/audio/tasks/task-7") {
			t.Errorf("请求路径异常: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"task-7","status":"SUCCESS","text":"任务结果"}`)
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	code := env.app.Run([]string{"task", "--config", env.cfgPath, "task-7", "-o", "-"})
	if code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}
	if !strings.Contains(env.stdout.String(), "任务结果") {
		t.Fatalf("任务结果未输出: %q", env.stdout.String())
	}
}

func TestConfigInitAndShow(t *testing.T) {
	env := newTestEnv(t, "https://api.example.com")
	initPath := filepath.Join(env.workDir, "sub", "config.toml")

	if code := env.app.Run([]string{"config", "init", "--config", initPath}); code != ExitOK {
		t.Fatalf("初始化失败: %d, stderr=%s", code, env.stderr.String())
	}
	content, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("配置文件未生成: %v", err)
	}
	if !strings.Contains(string(content), "version = 1") {
		t.Errorf("配置缺少版本号: %s", content)
	}

	// 再次初始化应提示已存在.
	if code := env.app.Run([]string{"config", "init", "--config", initPath}); code == ExitOK {
		t.Error("重复初始化应当失败")
	}

	env.stdout.Reset()
	if code := env.app.Run([]string{"config", "show", "--config", env.cfgPath}); code != ExitOK {
		t.Fatalf("展示配置失败: %d", code)
	}
	shown := env.stdout.String()
	if !strings.Contains(shown, "test-key") && !strings.Contains(shown, "****") {
		t.Errorf("密钥展示异常: %s", shown)
	}
	if strings.Contains(shown, "api_key = \"test-key\"") {
		t.Error("完整密钥不应明文展示")
	}
}

func TestMissingAPIKeyGivesHint(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("version = 1\n\n[api]\nbase_url = \"https://api.example.com\"\n"), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	t.Setenv("MOSS_API_KEY", "")

	app := New("test")
	app.Stdout = &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Stderr = stderr
	audio := filepath.Join(dir, "a.mp3")
	if err := os.WriteFile(audio, []byte("x"), 0o600); err != nil {
		t.Fatalf("写入音频失败: %v", err)
	}

	if code := app.Run([]string{"--config", cfgPath, audio}); code != ExitError {
		t.Fatalf("缺少密钥时退出码异常: %d", code)
	}
	if !strings.Contains(stderr.String(), "API Key") {
		t.Fatalf("缺少提示信息: %s", stderr.String())
	}
}

func TestExpandInputsMissingFile(t *testing.T) {
	env := newTestEnv(t, "https://api.example.com")
	code := env.app.Run([]string{"--config", env.cfgPath, filepath.Join(env.workDir, "不存在.mp3")})
	if code != ExitUsage {
		t.Fatalf("文件不存在时退出码异常: %d", code)
	}
}

func TestGlobalFlagsBeforeAndAfterSubcommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"task-9","status":"SUCCESS","text":"结果文本"}`)
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	cases := [][]string{
		{"task", "--config", env.cfgPath, "task-9", "-o", "-"},
		{"--config", env.cfgPath, "task", "task-9", "-o", "-"},
		{"task", "task-9", "--config", env.cfgPath, "-o", "-"},
	}
	for i, args := range cases {
		env.stdout.Reset()
		if code := env.app.Run(args); code != ExitOK {
			t.Fatalf("第 %d 种参数顺序失败: %d, stderr=%s", i+1, code, env.stderr.String())
		}
		if !strings.Contains(env.stdout.String(), "结果文本") {
			t.Errorf("第 %d 种参数顺序未输出结果: %q", i+1, env.stdout.String())
		}
	}
}

func TestStreamWithStructuredOutputKeepsStdoutClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"增量\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"transcript.text.done\",\"text\":\"增量文本\"}\n\n")
	}))
	defer srv.Close()

	env := newTestEnv(t, srv.URL)
	audio := env.audioFile(t, "直播.m4a")
	if code := env.app.Run([]string{"--config", env.cfgPath, "-s", "-f", "json", audio, "-o", "-"}); code != ExitOK {
		t.Fatalf("退出码异常: %d, stderr=%s", code, env.stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(env.stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("标准输出不是纯 JSON, 增量文本污染了结果: %v, 内容=%q", err, env.stdout.String())
	}
	if decoded["text"] != "增量文本" {
		t.Errorf("JSON 结果异常: %v", decoded)
	}
	// 增量文本应当作为进度出现在标准错误里.
	if !strings.Contains(env.stderr.String(), "增量") {
		t.Errorf("增量文本未输出到标准错误: %s", env.stderr.String())
	}
}

package audio

import (
	"os"
	"path/filepath"
	"testing"
)

// prepareDir 造一个包含音频与非音频文件的目录.
func prepareDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.m4a", "nested.mp4", "笔记.txt", ".hidden.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("准备文件失败: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	return dir
}

func TestExpandInputsDirectory(t *testing.T) {
	dir := prepareDir(t)
	files, err := ExpandInputs([]string{dir})
	if err != nil {
		t.Fatalf("展开目录失败: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("目录展开结果数量异常: %v", files)
	}
	for _, f := range files {
		if filepath.Base(f) == "笔记.txt" || filepath.Base(f) == ".hidden.mp3" {
			t.Errorf("不应包含非音频或隐藏文件: %s", f)
		}
	}
}

func TestExpandInputsGlobAndDedup(t *testing.T) {
	dir := prepareDir(t)
	pattern := filepath.Join(dir, "*.mp3")
	files, err := ExpandInputs([]string{pattern, filepath.Join(dir, "a.mp3")})
	if err != nil {
		t.Fatalf("展开通配符失败: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "a.mp3" {
		t.Fatalf("通配符与去重结果异常: %v", files)
	}

	if _, err := ExpandInputs([]string{filepath.Join(dir, "*.wav")}); err == nil {
		t.Error("通配符无匹配时应当报错")
	}
}

func TestExpandInputsUnknownExtension(t *testing.T) {
	dir := prepareDir(t)
	path := filepath.Join(dir, "录音.aiff")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	files, err := ExpandInputs([]string{path})
	var unknown *UnknownExtensionError
	if !asUnknown(err, &unknown) {
		t.Fatalf("应当返回扩展名未知的提示: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("扩展名未知时仍应继续尝试: %v", files)
	}
}

func TestInspectRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mp3")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}
	if _, err := Inspect(path); err == nil {
		t.Error("空文件应当报错")
	}
	if _, err := Inspect(dir); err == nil {
		t.Error("目录应当报错")
	}
}

// asUnknown 是 errors.As 的简化包装, 避免测试文件引入无关依赖.
func asUnknown(err error, target **UnknownExtensionError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*UnknownExtensionError); ok {
		*target = e
		return true
	}
	return false
}

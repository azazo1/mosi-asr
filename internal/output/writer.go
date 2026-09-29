package output

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/azazo1/mosi-asr/internal/config"
)

// Target 描述结果的输出去向.
type Target struct {
	// Path 是结果文件路径, 为空表示直接写到标准输出.
	Path string
	// Stdout 为 true 时结果写到标准输出.
	Stdout bool
}

// ResolveTarget 推导结果的输出位置.
//
// 优先级: outArg > (dir 或音频所在目录) 下的同名文件.
// outArg 为 "-" 时输出到标准输出. overwrite 为 false 且目标已存在时会追加序号.
func ResolveTarget(src, outArg, dir, format string, overwrite bool) (*Target, error) {
	if strings.TrimSpace(outArg) == "-" {
		return &Target{Stdout: true}, nil
	}
	if p := strings.TrimSpace(outArg); p != "" {
		path := config.ExpandHome(p)
		if !overwrite {
			path = uniquePath(path)
		}
		return &Target{Path: path}, nil
	}

	ext := config.FormatExtension(format)
	base := "transcript"
	if src != "" {
		base = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	targetDir := strings.TrimSpace(dir)
	if targetDir == "" {
		if src != "" {
			targetDir = filepath.Dir(src)
		} else {
			targetDir = "."
		}
	}
	path := filepath.Join(config.ExpandHome(targetDir), base+"."+ext)
	if !overwrite {
		path = uniquePath(path)
	}
	return &Target{Path: path}, nil
}

// uniquePath 在目标已存在时追加序号, 避免覆盖用户已有结果.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return path
}

// Marshal 把结果渲染成字节切片, 便于先写文件或先做长度检查.
func Marshal(res *Result, opts Options) ([]byte, error) {
	var buf bytes.Buffer
	if err := Render(&buf, res, opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteFile 把内容写入结果文件, 自动创建父目录.
func WriteFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("写入结果文件 %s 失败: %w", path, err)
	}
	return nil
}

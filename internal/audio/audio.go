// Package audio 负责音频输入的整理与预检.
//
// 包括支持的格式判定, 文件基本信息读取, 以及把命令行参数展开成实际待转写的
// 文件列表 (支持通配符与目录).
package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// supportedExtensions 是服务端明确支持的音视频扩展名, 与接口文档一致.
var supportedExtensions = []string{
	"aac", "amr", "flac", "m4a", "alac", "mov", "mp3", "mp4",
	"mpg", "ogg", "opus", "wav", "webm", "wma",
}

// Info 描述一个待转写文件.
type Info struct {
	Path      string
	Size      int64
	Extension string
	Modified  time.Time
	// KnownExtension 表示扩展名在服务端支持列表内.
	KnownExtension bool
}

// SupportedExtensions 返回支持的扩展名列表, 小写且已排序.
func SupportedExtensions() []string {
	out := make([]string, len(supportedExtensions))
	copy(out, supportedExtensions)
	sort.Strings(out)
	return out
}

// SupportedExtensionsText 返回逗号分隔的扩展名, 用于提示信息.
func SupportedExtensionsText() string {
	return strings.Join(SupportedExtensions(), ", ")
}

// IsSupportedExt 判断扩展名是否在支持列表内.
func IsSupportedExt(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	for _, s := range supportedExtensions {
		if s == ext {
			return true
		}
	}
	return false
}

// Inspect 读取文件基本信息并做基本检查.
func Inspect(path string) (*Info, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %s: %w", path, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s 是目录, 请直接传入具体文件或使用目录展开", path)
	}
	if st.Size() == 0 {
		return nil, fmt.Errorf("%s 是空文件", path)
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return &Info{
		Path:           path,
		Size:           st.Size(),
		Extension:      ext,
		Modified:       st.ModTime(),
		KnownExtension: IsSupportedExt(path),
	}, nil
}

// ExpandInputs 把命令行参数展开成待转写文件列表.
//
// 规则:
//   - 通配符按 shell 未展开的情况处理, 自己展开;
//   - 目录取其下扩展名受支持的文件, 不递归;
//   - 其余按文件处理, 重复路径只保留一次.
func ExpandInputs(args []string) ([]string, error) {
	var (
		files   []string
		seen    = map[string]bool{}
		unknown []string
	)

	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		files = append(files, abs)
	}

	for _, arg := range args {
		if arg == "" {
			continue
		}
		if strings.ContainsAny(arg, "*?[") {
			matches, err := filepath.Glob(arg)
			if err != nil {
				return nil, fmt.Errorf("通配符 %s 不合法: %w", arg, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("通配符 %s 没有匹配到文件", arg)
			}
			for _, m := range matches {
				// Go 的通配符会匹配隐藏文件, 这里保持与 shell 一致的直觉.
				if isHidden(m) {
					continue
				}
				add(m)
			}
			continue
		}

		st, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("无法访问 %s: %w", arg, err)
		}
		if !st.IsDir() {
			if !IsSupportedExt(arg) {
				unknown = append(unknown, arg)
			}
			add(arg)
			continue
		}

		entries, err := os.ReadDir(arg)
		if err != nil {
			return nil, fmt.Errorf("读取目录 %s 失败: %w", arg, err)
		}
		found := 0
		for _, e := range entries {
			if e.IsDir() || isHidden(e.Name()) {
				continue
			}
			if !IsSupportedExt(e.Name()) {
				continue
			}
			add(filepath.Join(arg, e.Name()))
			found++
		}
		if found == 0 {
			return nil, fmt.Errorf("目录 %s 下没有找到受支持的音频文件, 支持: %s", arg, SupportedExtensionsText())
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("没有可转写的文件")
	}
	if len(unknown) > 0 {
		// 扩展名未知不阻止执行, 由服务端最终判定, 调用方可以把提示输出给用户.
		return files, &UnknownExtensionError{Paths: unknown}
	}
	return files, nil
}

// isHidden 判断文件名是否为隐藏文件.
func isHidden(name string) bool {
	return strings.HasPrefix(filepath.Base(name), ".")
}

// UnknownExtensionError 表示部分输入文件的扩展名不在支持列表内.
type UnknownExtensionError struct {
	Paths []string
}

// Error 实现 error 接口.
func (e *UnknownExtensionError) Error() string {
	return fmt.Sprintf("以下文件的扩展名不在文档支持列表内, 仍会尝试上传: %s (支持: %s)",
		strings.Join(e.Paths, ", "), SupportedExtensionsText())
}

package cli

import (
	"flag"
	"strings"
)

// boolFlag 用于识别布尔型参数, 布尔参数不会消费后面的取值.
type boolFlag interface {
	IsBoolFlag() bool
}

// reorderArgs 把参数重新排布成 "先参数后位置参数".
//
// Go 标准库的 flag 遇到第一个非参数就会停止解析, 而日常使用中
// "mosi-asr 录音.m4a -d" 这种写法更自然, 因此在解析前统一重排.
// "--" 之后的内容一律视为位置参数, 不再重排.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var (
		flags       []string
		positionals []string
		terminated  bool
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if terminated {
			positionals = append(positionals, arg)
			continue
		}
		if arg == "--" {
			terminated = true
			continue
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		name, hasInlineValue := flagName(arg)
		if hasInlineValue || isBoolArg(fs, name) {
			continue
		}
		// 非布尔参数需要把后面的取值一起搬过来.
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	out := make([]string, 0, len(flags)+len(positionals))
	out = append(out, flags...)
	out = append(out, positionals...)
	return out
}

// flagName 去掉前导横线并拆分 "--name=value" 形式.
func flagName(arg string) (name string, hasInlineValue bool) {
	trimmed := strings.TrimLeft(arg, "-")
	if idx := strings.Index(trimmed, "="); idx >= 0 {
		return trimmed[:idx], true
	}
	return trimmed, false
}

// isBoolArg 判断参数是否为布尔型.
func isBoolArg(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		// 未知参数不消费后面的取值, 交给标准库报错.
		return true
	}
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

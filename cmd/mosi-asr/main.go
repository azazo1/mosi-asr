// Command mosi-asr 把音视频转成文字, 后端使用 Moss API.
//
// 版本号由发布构建通过 -ldflags 注入 internal/buildinfo, 这里不保存任何
// 版本状态, git tag 是唯一的版本来源.
package main

import (
	"os"

	"github.com/azazo1/mosi-asr/internal/buildinfo"
	"github.com/azazo1/mosi-asr/internal/cli"
)

func main() {
	app := cli.New(buildinfo.Version())
	os.Exit(app.Run(os.Args[1:]))
}

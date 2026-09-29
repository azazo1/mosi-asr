// Command mosi-asr 把音视频转成文字, 后端使用 Moss API.
package main

import (
	"os"

	"github.com/azazo1/mosi-asr/internal/cli"
)

// version 由构建时通过 -ldflags 注入, 未注入时表示开发版本.
var version = "dev"

func main() {
	app := cli.New(version)
	os.Exit(app.Run(os.Args[1:]))
}

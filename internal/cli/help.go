package cli

import "fmt"

// printHelp 打印顶层帮助.
func (a *App) printHelp() {
	fmt.Fprintf(a.Stdout, `mosi-asr %s - 用 Moss API 把音视频转成文字

用法:
  mosi-asr [参数] <音频文件...>          转写音频 (默认子命令)
  mosi-asr config <子命令>               查看或初始化配置
  mosi-asr task <task_id> [参数]         查询异步任务结果
  mosi-asr version                       显示版本

配置:
  默认读取 ~/.config/mosi-asr/config.toml, 可用 --config 指定路径,
  也可用环境变量 MOSS_ASR_CONFIG 覆盖. API Key 也可以放在环境变量里,
  默认变量名为 MOSS_API_KEY, 可用 api.api_key_env 改成其它名字.

示例:
  mosi-asr 会议录音.m4a                 转写并在同目录生成 txt
  mosi-asr -d 访谈.mp3                  启用说话人识别
  mosi-asr -d -s 直播回放.m4a           边转写边显示文本
  mosi-asr -f srt -o 字幕.srt 课程.mp4  生成字幕文件
  mosi-asr --dir ./out 录音/*.m4a       批量转写到指定目录
  mosi-asr -o - 语音.m4a                结果直接打印到终端
  mosi-asr --url https://example.com/a.mp3
  mosi-asr task task-123 --wait -f srt

转写参数可用 mosi-asr transcribe --help 查看, 配置参数见 mosi-asr config --help.

`, a.Version)
}

// printTranscribeHelp 打印转写子命令的完整参数说明.
func (a *App) printTranscribeHelp() {
	fmt.Fprintf(a.Stdout, `用法: mosi-asr [参数] <音频文件...>

转写参数:
  -m, --model <id>         指定模型, 默认按是否需要说话人识别自动选择
  -d, --diarize            启用说话人识别 (多说话人转写)
      --no-diarize         关闭配置中的默认说话人识别
  -f, --format <fmt>       输出格式: txt, text, srt, json, md
      --json               等价于 -f json
  -o, --output <path>      结果文件路径, 用 - 表示打印到标准输出
      --stdout             结果打印到标准输出
      --dir <dir>          结果输出目录, 默认与音频同目录
  -s, --stream             流式输出, 边转写边显示
      --async              使用异步任务, 适合长音频
      --sync               强制同步请求
  -k, --keyterm <词>       热词, 可重复, 也可逗号分隔 (最多 20 个)
      --keyterms <词,词>   热词, 逗号分隔
      --url <地址>         转写公网音频地址, 与本地文件二选一
      --file-id <id>       转写已上传文件的 file_id
      --timestamps         文本中保留时间戳
      --no-speaker-prefix  文本中不加说话人前缀
      --merge-gap <秒>     同一说话人相邻分段合并间隔
      --timeout <秒>       单次请求超时

全局参数:
      --config <path>      指定配置文件
  -v, --verbose            输出调试日志
  -q, --quiet              只输出错误

说明:
  文件大小超过 transcribe.auto_async_over_mb 时会自动改用异步任务.
  流式输出与热词需要 moss-transcribe-diarize-pro 模型, 指定普通模型会失败.
  使用 --stream 时, 若最终结果写到文件, 增量文本显示在终端上作为进度.

`)
}

// printConfigHelp 打印配置子命令帮助.
func (a *App) printConfigHelp() {
	fmt.Fprintf(a.Stdout, `用法: mosi-asr config <子命令>

子命令:
  init [--force]   生成配置文件模板, --force 覆盖已有文件
  path             打印当前使用的配置文件路径
  show             打印当前生效的配置, 密钥会被遮蔽

`)
}

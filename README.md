# mosi-asr

把音视频转成文字的日常命令行工具, 后端使用 [Moss API](https://platform.mosi.cn/docs/scenarios/transcribe-diarization)
的语音识别接口. 支持多说话人分离, 流式输出, 异步长音频任务, 以及 txt / srt / json / md 多种结果格式.

## 安装

从 [Releases](https://github.com/azazo1/mosi-asr/releases) 下载对应平台的归档, 解压后放进 PATH:

```shell
tar -xzf mosi-asr-0.1.0-macos-aarch64.tar.gz
install -m 755 mosi-asr /usr/local/bin/mosi-asr
```

也可以直接用 Go 安装:

```shell
go install github.com/azazo1/mosi-asr/cmd/mosi-asr@latest
```

## 快速开始

```shell
# 1. 生成配置文件, 位置固定在 ~/.config/mosi-asr/config.toml
mosi-asr config init

# 2. 填入 API Key (二选一)
#    写进配置文件: api_key = "你的 Key"
#    或者用环境变量: export MOSS_API_KEY=你的Key

# 3. 转写
mosi-asr 会议录音.m4a        # 结果写到 会议录音.txt
```

源码目录里也可以用 `just init` / `just build` 完成同样的步骤.

## 常用用法

```shell
mosi-asr -d 访谈.mp3                    # 启用说话人识别, 按 "S01: ..." 分行
mosi-asr -d -s 直播回放.m4a             # 流式输出, 边转写边显示
mosi-asr -f srt -o 字幕.srt 课程.mp4    # 生成字幕文件
mosi-asr --dir ./out 录音/*.m4a         # 批量转写, 结果统一放到 ./out
mosi-asr -o - 语音.m4a                  # 结果直接打印到终端, 便于管道
mosi-asr --url https://example.com/a.mp3  # 转写公网音频
mosi-asr -k 矩池云,SGLang 技术分享.m4a    # 用热词提升专有名词识别
mosi-asr task task-123 --wait -f srt    # 查询异步任务并导出结果
```

## 配置

默认读取 `~/.config/mosi-asr/config.toml`, 可以用 `--config` 或环境变量 `MOSS_ASR_CONFIG` 指定其它路径.
配置里带有版本号, 旧配置升级时会自动迁移. 完整字段与注释见 [examples/config.toml.example](examples/config.toml.example).

几个日常最常调整的字段:

| 字段 | 作用 |
| --- | --- |
| `api.api_key` / `api.api_key_env` | API Key 来源, 前者为空时读后者指定的环境变量 |
| `transcribe.diarize` | 是否默认启用说话人识别 |
| `transcribe.auto_async_over_mb` | 音频超过该大小自动改用异步任务 |
| `transcribe.keyterms` | 常驻热词, 提升人名, 品牌名等专有名词的识别率 |
| `output.format` / `output.dir` | 默认输出格式与输出目录 |
| `log.file` | 需要留档排查时把日志落到文件 |

命令行参数始终优先于配置文件.

## 结果格式

| 格式 | 内容 |
| --- | --- |
| `txt` | 整理后的纯文本, 启用说话人识别时按说话人分行, 相邻同说话人分段自动合并 |
| `text` | 接口返回的原始整段文本 |
| `srt` | 字幕文件, 带时间轴, 说话人写入字幕文本 |
| `json` | 接口返回的原始 JSON, 便于脚本继续处理 |
| `md` | Markdown, 每条发言一段, 可带说话人与时间范围 |

## 开发

```shell
just build      # 编译到 bin/mosi-asr, 版本号显示 dev-build
just test       # 单元测试与命令行级测试
just check      # gofmt 检查 + go vet
just version    # 当前工作区对应的版本号
just dist       # 生成当前平台的发布产物, 输出到 dist/
just mock       # 启动本地模拟服务, 不产生真实调用
```

离线调试: 把配置里的 `api.base_url` 指向 `http://127.0.0.1:18080`, 启动 `just mock` 后即可跑通全流程, `just smoke` 会把整个链路跑一遍.

发布产物的版本号自动生成: 恰好停在版本 tag 上时显示该 tag, 非 tag commit 追加 7 位短 hash, 工作区有未提交改动时改用 `^` 分隔. 归档名里的版本段不带 `v` 前缀, 而二进制内显示带 `v`, 例如产物 `mosi-asr-1.2.3-linux-x86_64.tar.gz` 里的二进制报 `v1.2.3`.

## 目录结构

```
cmd/mosi-asr/           入口
internal/buildinfo/     构建期注入的版本号
internal/cli/           参数解析与各子命令
internal/config/        配置加载, 校验与版本迁移
internal/mossapi/       接口客户端: 同步, 流式 SSE, 异步任务
internal/output/        结果整理与渲染
internal/audio/         输入文件预检与展开
internal/logger/        日志设施
scripts/mock-server.py  离线模拟服务
scripts/dist.sh         当前平台的发布产物
docs/api-notes.md       接口要点与本项目的对应实现
docs/changelog/         各版本的人工发布说明
```

## 说明

- 单文件上限 512 MB; 支持 AAC, AMR, FLAC, M4A, ALAC, MOV, MP3, MP4, MPG, OGG, OPUS, WAV, WebM, WMA.
- 流式输出与热词只支持 `moss-transcribe-diarize-pro`, 指定普通模型会被服务端拒绝.
- 转写内容会上传到 Moss 服务端, 敏感音频请自行评估.

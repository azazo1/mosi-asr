# Moss API 转写接口要点

本文记录实现 mosi-asr 时用到的接口约定, 以及这些约定在代码中的落点, 便于日后接口更新时快速对齐.
原始文档: <https://platform.mosi.cn/docs/scenarios/transcribe-diarization>

## 接口

| 用途 | 方法与路径 |
| --- | --- |
| 转写 | `POST /v1/audio/transcriptions` |
| 查询异步任务 | `GET /v1/audio/tasks/{task_id}` |

鉴权统一使用 `Authorization: Bearer <API_KEY>`.

## 模型选择

| 模型 | 能力 |
| --- | --- |
| `moss-transcribe-1.0` | 普通转写, 不支持说话人识别, 热词与流式 |
| `moss-transcribe-diarize-pro` | 多说话人转写, 支持热词与 SSE 流式, 最长 60 分钟 |

代码中由 `TranscribeRequest.withDefaults()` 决定: 只有调用方没有显式指定模型时, 才按
"是否启用说话人识别 / 是否流式 / 是否带热词" 自动选择多说话人模型. 显式指定了普通模型又要求
流式或热词时, 请求会在本地就被拒绝, 而不是等服务的 400.

## 输入源

`file`, `file_id`, `url`, `audio_url` 四选一, 不支持 `audio_data` 或 Base64 内联音频.

- 本地文件走 `multipart/form-data`, 字段顺序为 model, response_format, diarize, keyterms, async, stream;
- `keyterms` 在 multipart 下必须以 JSON 字符串数组传递, 例如 `["MOSS","SGLang"]`;
- URL 必须是服务端可访问的公网地址, 不能指向本机或私网.

本地文件上传有两点实现细节:

1. 请求体通过 `io.Pipe` 流式构造, 避免把最大 512 MB 的文件整体读进内存;
2. 请求体的字节数在发送前精确算好并写入 `Content-Length`. 如果省略, Go 会使用 chunked 传输,
   部分服务端与网关对 chunked 上传支持不佳.

## 结果形态

- `response_format=json`: 返回 `{text}`; 启用 `diarize` 时额外返回 `duration` 与 `segments`;
- `segments[]` 字段为 `type`, `id`, `start`, `end`, `text`, `speaker`, 其中 `speaker` 形如 `S01`;
- `response_format=text`: 响应体是纯文本, 不是 JSON 对象;
- 流式 (`stream=true`) 固定返回 SSE, 建议显式传 `response_format=verbose_json`.

SSE 事件:

| 事件 | 含义 |
| --- | --- |
| `task.created` | 任务创建, 携带 `task_id` |
| `transcript.text.delta` | 增量文本, 按顺序拼接 |
| `transcript.segment.done` | 一段转写完成, 带说话人与时间范围 |
| `transcript.text.done` | 结束, 带最终 `text` 与 `usage` |
| `error` | 流内错误 |

## 异步任务

传 `async=true` 会立即返回任务对象, 使用 `task_id` 查询. 任务状态为
`PENDING` / `PROCESSING` / `SUCCESS` / `FAILED`; 成功后结果字段直接位于响应顶层
(`text`, `segments`), 失败原因位于 `error`. 响应里的 `retry_after` 是服务端建议的轮询间隔,
客户端会优先采用它.

注意 `stream=true` 与 `async=true` 不能同时使用.

## 限制

- 单文件最大 512 MB, 支持 AAC, AMR, FLAC, M4A, ALAC, MOV, MP3, MP4, MPG, OGG, OPUS, WAV, WebM, WMA;
- 热词最多 20 个, 每个最多 30 个字符, 只支持多说话人模型;
- 普通模型携带非空热词会返回参数错误.

## 常见错误码与处理

| HTTP | error.code | 处理 |
| --- | --- | --- |
| 400 | `invalid_input_source` | 四种输入源只能选一个 |
| 400 | `unsupported_stream` | 流式需要多说话人模型 |
| 400 | `unsupported_with_async` | stream 与 async 二选一 |
| 400 | `invalid_keyterms` | 热词数量或长度超限 |
| 401/403 | `authentication_error` / `permission_error` | 检查 API Key |
| 402 | `billing_error` / `insufficient_credits` | 余额不足 |
| 429 | `rate_limit_error` / `concurrency_limit_exceeded` | 稍后重试或降低并发 |
| 404 | `task_not_found` | task_id 不存在或无权限 |
| 5xx | `internal` / `upstream` / `service` / `timeout` | 自动重试后仍失败 |

客户端对 408, 425, 429 与 5xx 做指数退避重试, 默认两次, 并遵循响应中的 `Retry-After`.
错误对象会附带该错误码对应的中文处理建议.

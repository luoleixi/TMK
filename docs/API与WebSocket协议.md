# TMK API 与 WebSocket 协议

## 1. 文档信息

- 协议版本：`v1`
- REST 前缀：`/api/v1`
- WebSocket 入口：`/api/v1/interpret`
- 适用范围：TMK 客户端、测试工具和内部管理端
- 更新时间：2026-08-07

本文档描述当前代码已经实现的接口行为。生产环境仍需补齐用户鉴权、资源归属校验、日志脱敏、CORS 白名单和 WebSocket Origin 校验；这些要求不会被描述为已实现能力。

## 2. 环境与地址

文档和示例不得写入真实服务器地址、账号、密码、API Key 或数据库连接串。调用方通过部署环境获得实际地址。

```text
REST:      https://api.example.invalid/api/v1
WebSocket: wss://api.example.invalid/api/v1/interpret
```

测试和生产环境必须使用不同的域名、配置、数据库和密钥。客户端不得根据文档中的示例地址连接服务。

## 3. 通用约定

### 3.1 HTTP 请求

- JSON 请求使用 `Content-Type: application/json`。
- 当前接口尚未强制校验 `Authorization`。
- 生产接口应使用 `Authorization: Bearer <access_token>`。
- 时间使用 RFC 3339，例如 `2026-08-07T10:30:00+08:00`。
- WebSocket 消息时间戳使用 Unix 毫秒。

### 3.2 REST 响应

除健康检查和语言列表外，业务接口通常使用统一响应结构：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

错误响应示例：

```json
{
  "code": 400,
  "message": "invalid request"
}
```

当前主要状态码：

| HTTP 状态码 | 含义 |
|---|---|
| `200` | 请求成功 |
| `201` | 会话创建成功 |
| `400` | JSON、参数或业务前置条件不合法 |
| `404` | 会话不存在 |
| `500` | 服务端或数据库错误 |
| `502` | 外部翻译或摘要服务失败 |

### 3.3 API 脱敏总则

当前接口会返回会话原文、译文和摘要，尚未实现基于用户的字段脱敏。接入生产环境前必须执行以下规则：

| 数据 | API 响应 | 服务日志 | 管理端展示 |
|---|---|---|---|
| `session_id` | 所有者可见完整值 | 只记录前 8 位或哈希 | 默认显示前 8 位 |
| 用户 Token/API Key | 绝不返回 | 绝不记录 | 仅显示掩码 |
| 原文、译文、摘要 | 所有者按需获取 | 默认不记录正文 | 默认截断和脱敏，授权后查看 |
| IP、设备 ID | 普通业务接口不返回 | 哈希或部分掩码 | 仅风控角色可查看 |
| 数据库错误和模型错误 | 返回稳定错误码 | 内部日志记录完整原因 | 不显示密钥、地址和堆栈 |
| 时间和统计字段 | 可返回 | 可记录 | 可展示 |

所有会话和历史接口必须校验“当前用户是否拥有该资源”。删除、摘要生成和 WebSocket 建连还应记录审计事件。

## 4. REST API

### 4.1 健康检查

```http
GET /api/health
```

请求体：无。

响应示例：

```json
{
  "status": "ok",
  "timestamp": 1786071000,
  "version": "1.0.0",
  "services": {
    "asr": true,
    "translator": true,
    "tts": false
  }
}
```

`status` 可能为 `starting`、`ok` 或 `degraded`。

脱敏要求：不得返回主机名、数据库地址、模型密钥、内部异常、调用栈和服务拓扑。公网存活探针建议只返回整体状态；详细依赖状态仅向内部监控开放。

### 4.2 获取支持语言

```http
GET /api/v1/languages
```

请求体：无。

响应示例：

```json
{
  "languages": [
    {
      "code": "zh",
      "name": "中文",
      "tts_voices": ["longanyang"]
    },
    {
      "code": "en",
      "name": "English",
      "tts_voices": ["longanyang"]
    }
  ]
}
```

当前支持 `zh`、`en`、`ja`、`ko`、`fr`、`de`、`es`、`ru`。

脱敏要求：语言能力不属于敏感数据，可以直接返回；如果语音名称能暴露供应商或商业配置，公开 API 可只返回稳定的业务 voice ID。

### 4.3 创建会话

```http
POST /api/v1/sessions
```

请求体：

```json
{
  "source_lang": "zh",
  "target_lang": "en",
  "input_type": "system_audio"
}
```

字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `source_lang` | 是 | 源语言代码 |
| `target_lang` | 是 | 目标语言代码 |
| `input_type` | 否 | 音频来源；省略时为 `system_audio` |

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "11111111-2222-4333-8444-555555555555",
    "source_lang": "zh",
    "target_lang": "en",
    "input_type": "system_audio",
    "status": "ready",
    "record_count": 0,
    "created_at": "2026-08-07T10:30:00+08:00"
  }
}
```

脱敏要求：`id` 使用虚拟 UUID 展示；生产实现必须绑定当前用户，响应中不得包含模型配置、内部节点和数据库信息。日志只记录脱敏后的会话 ID。

### 4.4 查询会话

```http
GET /api/v1/sessions/{session_id}
```

路径参数：`session_id` 为创建会话时返回的 UUID。请求体：无。

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "11111111-2222-4333-8444-555555555555",
    "source_lang": "zh",
    "target_lang": "en",
    "input_type": "system_audio",
    "status": "active",
    "record_count": 3,
    "brief": "系统架构讨论",
    "created_at": "2026-08-07T10:30:00+08:00"
  }
}
```

脱敏要求：必须校验资源归属。非所有者统一返回 `404` 或权限策略规定的响应，避免通过 `403` 枚举会话是否存在。普通日志不记录 `brief` 和正文。

### 4.5 停止会话

```http
POST /api/v1/sessions/{session_id}/stop
```

请求体：无。

成功响应：

```json
{
  "code": 0,
  "message": "ok"
}
```

停止后服务端会结束会话，并异步尝试生成简短会话主题。

脱敏要求：必须校验资源归属并记录审计日志；审计记录只包含脱敏用户 ID、会话 ID、操作结果和时间，不记录会话正文。

### 4.6 查询历史会话列表

```http
GET /api/v1/history?offset=0&limit=20&source_lang=zh&target_lang=en&date_from=2026-08-01T00:00:00%2B08:00&date_to=2026-08-08T00:00:00%2B08:00&keyword=架构
```

请求体：无。

查询参数：

| 参数 | 默认值 | 说明 |
|---|---|---|
| `offset` | `0` | 分页偏移 |
| `limit` | `20` | 每页数量，范围 `1..100` |
| `source_lang` | 空 | 源语言筛选 |
| `target_lang` | 空 | 目标语言筛选 |
| `date_from` | 空 | RFC 3339 起始时间 |
| `date_to` | 空 | RFC 3339 结束时间 |
| `keyword` | 空 | 历史内容关键词 |

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "total": 1,
    "offset": 0,
    "limit": 20,
    "sessions": [
      {
        "id": "11111111-2222-4333-8444-555555555555",
        "source_lang": "zh",
        "target_lang": "en",
        "input_type": "system_audio",
        "status": "ended",
        "record_count": 3,
        "brief": "系统架构讨论",
        "created_at": "2026-08-07T10:30:00+08:00",
        "ended_at": "2026-08-07T10:35:00+08:00"
      }
    ]
  }
}
```

脱敏要求：只能查询当前用户的数据。列表不返回原文、译文和完整摘要；管理端默认掩码会话 ID，并限制关键词搜索日志记录。

### 4.7 查询历史会话详情

```http
GET /api/v1/history/{session_id}
```

请求体：无。

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "session_id": "11111111-2222-4333-8444-555555555555",
    "source_lang": "zh",
    "target_lang": "en",
    "duration_seconds": 300,
    "brief": "系统架构讨论",
    "summary": "会议讨论了服务拆分、监控和发布计划。",
    "created_at": "2026-08-07T10:30:00+08:00",
    "ended_at": "2026-08-07T10:35:00+08:00",
    "records": [
      {
        "id": 101,
        "session_id": "11111111-2222-4333-8444-555555555555",
        "sequence": 1,
        "source_text": "这是一段脱敏后的示例文本。",
        "translated_text": "This is a sanitized example.",
        "confidence": 0,
        "audio_duration_ms": 0,
        "created_at": "2026-08-07T10:30:02+08:00"
      }
    ]
  }
}
```

脱敏要求：这是高敏感度接口，必须校验资源归属。生产日志不得输出 `source_text`、`translated_text`、`summary`；管理端需要单独的“查看完整内容”权限和审计记录。

### 4.8 生成历史会话摘要

```http
POST /api/v1/history/{session_id}/summary
```

请求体：无。若已有摘要，直接返回已有内容；没有记录时返回 `400`。

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "summary": "会议讨论了服务拆分、监控和发布计划。"
  }
}
```

脱敏要求：必须校验资源归属、限制调用频率并统计模型 Token。模型错误不能原样返回供应商请求、密钥或内部地址；摘要内容不得写入普通访问日志。

### 4.9 删除单条历史会话

```http
DELETE /api/v1/history/{session_id}
```

请求体：无。

响应示例：

```json
{
  "code": 0,
  "message": "ok"
}
```

脱敏要求：必须校验资源归属并记录审计事件。审计日志只记录脱敏标识、操作者、时间和结果，不保留被删除正文。

### 4.10 批量删除历史会话

```http
POST /api/v1/history/delete
```

请求体：

```json
{
  "ids": [
    "11111111-2222-4333-8444-555555555555",
    "66666666-7777-4888-8999-000000000000"
  ]
}
```

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "deleted": 2
  }
}
```

脱敏要求：逐个校验资源归属，限制单次 ID 数量并记录审计事件。响应仅返回删除数量，不回显正文或不存在的会话详情。

### 4.11 单句翻译

```http
POST /api/v1/translate
```

请求体：

```json
{
  "text": "这是一段脱敏后的示例文本。",
  "source_lang": "zh",
  "target_lang": "en"
}
```

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "translated_text": "This is a sanitized example."
  }
}
```

脱敏要求：请求和译文不得写入普通访问日志；生产环境需要请求长度限制、用户配额、Token 统计和限流。外部模型错误应映射为稳定错误码，不能直接返回供应商响应或内部错误。

## 5. WebSocket 同传协议

### 5.1 建立连接

客户端应先调用创建会话接口，再连接：

```http
GET /api/v1/interpret?session_id=11111111-2222-4333-8444-555555555555
Upgrade: websocket
Connection: Upgrade
```

WebSocket 没有 JSON 请求体，`session_id` 通过查询参数传递。连接成功后，服务端校验会话是否存在；当前实现尚未校验会话所有者。

生产脱敏与安全要求：

- URL、代理日志和访问日志必须掩码 `session_id`；
- 使用短期 WebSocket 票据或 Authorization，避免把长期 Token 放进 URL；
- 校验会话归属、Origin、单用户连接数和单 IP 连接数；
- 保持 `wss://`，禁止生产环境明文传输；
- 当前实现允许任意 Origin，生产前必须改为白名单。

### 5.2 音频格式

音频必须使用 WebSocket Binary Message 发送，不得包装成 JSON：

```text
编码：PCM signed 16-bit little-endian
采样率：16000 Hz
声道：单声道
当前客户端分片：约 100 ms / 3200 bytes
```

服务端单条 WebSocket 消息读取上限为 1 MiB。发送 `{"type":"audio"}` 会收到错误消息。

音频属于高敏感数据：默认不落日志、不进入 Trace attribute；需要评测留存时必须取得授权，使用独立对象存储、加密、访问审计和自动过期策略。

### 5.3 推荐时序

```text
客户端                         服务端
  | POST /sessions                |
  |------------------------------>|
  | session_id                    |
  |<------------------------------|
  | WebSocket /interpret          |
  |==============================>|
  | {"type":"start"}             |
  |------------------------------>|
  | {"type":"started"}           |
  |<------------------------------|
  | Binary PCM frames             |
  |==============================>|
  | transcript / translation     |
  |<==============================|
  | {"type":"stop"}              |
  |------------------------------>|
  | {"type":"stopped"}           |
  |<------------------------------|
  | connection closed            |
```

### 5.4 客户端控制消息

#### start

启动 ASR 和翻译流水线：

```json
{
  "type": "start"
}
```

#### ping

应用层探活：

```json
{
  "type": "ping"
}
```

服务端响应 `pong`。除此之外，服务端还会发送 WebSocket 协议层 Ping Frame，客户端库应自动回复 Pong Frame。

#### stop

停止流水线、排空最终翻译并关闭本次连接：

```json
{
  "type": "stop"
}
```

#### 不支持的消息

当前服务端只支持 `start`、`ping`、`stop`。`pause` 和 `resume` 尚未在服务端协议中实现，发送后会得到 `unknown message type`。在服务端正式实现之前，不得将其描述为可用协议。

### 5.5 服务端事件

#### started

```json
{
  "type": "started",
  "timestamp_ms": 1786071000123
}
```

#### pong

```json
{
  "type": "pong",
  "timestamp_ms": 1786071001123
}
```

#### transcript

```json
{
  "type": "transcript",
  "seq": 12,
  "segment_id": 3,
  "revision": 4,
  "text": "这是一段脱敏后的临时识别文本",
  "is_final": false,
  "reason": "partial",
  "timestamp": 1786071002123
}
```

#### translation

```json
{
  "type": "translation",
  "seq": 12,
  "segment_id": 3,
  "revision": 4,
  "text": "This is a sanitized interim translation.",
  "is_final": false,
  "reason": "partial",
  "timestamp": 1786071002323
}
```

翻译失败时服务端降级返回原文，并增加警告：

```json
{
  "type": "translation",
  "seq": 13,
  "segment_id": 3,
  "revision": 5,
  "text": "这是一段脱敏后的示例文本。",
  "is_final": true,
  "reason": "provider_final",
  "timestamp": 1786071003323,
  "warning": "translate_failed_fallback_to_source"
}
```

#### stopped

```json
{
  "type": "stopped",
  "timestamp_ms": 1786071004123
}
```

#### error

```json
{
  "type": "error",
  "message": "invalid websocket message"
}
```

可能出现的稳定错误消息包括：

- `invalid session_id`
- `invalid websocket message`
- `unknown message type`
- `audio must be sent as binary PCM frames, not JSON text`
- `session not ready`
- `database error`

生产环境不得把模型供应商响应、数据库错误、密钥、内部地址和堆栈放入 `message`。

### 5.6 流式字段语义

| 字段 | 说明 |
|---|---|
| `seq` | 当前连接内单调递增的服务端事件序号 |
| `segment_id` | 逻辑句子编号；相同编号表示同一句的修订 |
| `revision` | 同一片段的版本号；客户端忽略更小的版本 |
| `is_final` | 是否为最终结果；只有最终翻译会写入历史记录 |
| `reason` | 当前结果产生或提交的原因 |
| `timestamp` | 服务端产生事件的 Unix 毫秒时间 |
| `warning` | 可恢复降级提示，不等同于请求失败 |

`reason` 当前可能值：

| 值 | 含义 |
|---|---|
| `partial` | ASR 中间结果 |
| `provider_final` | ASR 提供方确认句子结束 |
| `punctuation` | 本地分段器根据稳定终止标点提交 |
| `max_length` | 达到最大长度提交 |
| `max_duration` | 达到最大持续时间提交 |
| `soft_commit` | 软提交等待窗口结束 |
| `flush` | 停止或关闭时提交剩余文本 |

客户端必须以 `(segment_id, revision)` 处理修订，不得把每个 `transcript` 都追加为新消息。相同片段的低版本翻译可能晚到，客户端应丢弃过期版本。

### 5.7 连接限制和背压

当前服务端参数：

- 音频队列容量：32；队列满时丢弃新音频帧；
- 发送队列容量：128；队列满时丢弃新消息；
- WebSocket 单消息上限：1 MiB；
- 写超时：10 秒；
- Pong 等待：60 秒；
- 最终翻译排空最长等待：15 秒。

这些数值属于当前实现，不构成永久协议承诺。后续应通过 Metrics 暴露丢帧、丢消息和慢连接数量，而不是向普通业务响应返回内部队列状态。

## 6. 已删除接口

以下遗留占位接口已删除：

```http
GET /api/v1/audio/devices
```

该接口曾返回服务端硬编码设备，并不能表示用户电脑上的真实音频设备。删除后应返回 `404`。桌面客户端继续通过本地音频采集模块枚举设备，不通过服务端 API 获取。

## 7. 文档维护规则

- REST 路由、JSON 字段或状态码变化时，必须同步修改本文档。
- WebSocket 消息新增字段时必须保持旧客户端可忽略新字段。
- 删除或改变字段语义必须升级协议版本或提供兼容期。
- 示例必须使用 `example.invalid`、虚拟 UUID 和虚构文本。
- 禁止提交真实 Token、API Key、密码、DSN、用户音频或会话内容。
- 后续引入 OpenAPI 时，REST 契约以 `docs/openapi.yaml` 为机器可读来源，本文档继续描述 WebSocket 时序和安全约束。

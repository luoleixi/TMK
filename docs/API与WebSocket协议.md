# TMK API 与 WebSocket 协议

## 1. 文档范围

- 协议版本：`v1`
- REST 前缀：`/api/v1`
- WebSocket 路径：`/api/v1/interpret`
- 适用对象：TMK 客户端、测试工具和内部管理端

本文档只描述调用接口所需的公开契约。部署地址、鉴权实现、密钥、内部依赖、服务拓扑、风控规则和运行参数不在本文档中公开。

文档中的 UUID、时间和文本均为虚构示例，不对应真实用户或环境。

## 2. 通用约定

### 2.1 请求格式

- JSON 请求使用 `Content-Type: application/json`。
- 时间使用 RFC 3339，例如 `2026-08-07T10:30:00+08:00`。
- WebSocket 事件时间戳使用 Unix 毫秒。
- 调用方必须通过运行环境获取服务地址和访问凭证。

### 2.2 业务响应

业务接口通常返回：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

错误响应：

```json
{
  "code": 400,
  "message": "invalid request"
}
```

| HTTP 状态码 | 含义 |
|---|---|
| `200` | 请求成功 |
| `201` | 资源创建成功 |
| `400` | 请求参数或业务状态不合法 |
| `404` | 资源不存在 |
| `500` | 服务端处理失败 |
| `502` | 上游能力暂时不可用 |

### 2.3 数据保护边界

- API 不返回访问凭证、密钥、数据库信息和内部错误详情。
- 会话原文、译文、摘要和音频属于敏感业务数据。
- 普通访问日志不得记录请求正文、翻译正文和完整访问凭证。
- 示例和测试数据不得使用真实用户内容。
- 资源访问权限、限流和审计规则由安全规范管理，不在本文档中展开。

## 3. REST API

### 3.1 健康检查

以下接口由 Glance 提供核心业务服务的存活和就绪状态。管理与监控服务使用独立健康接口，不能用 Glance 的健康结果代替。

| 服务 | 存活 | 就绪 | 依赖诊断 |
| --- | --- | --- | --- |
| Admin API | `/api/health/live` | `/api/health/ready` | `/api/health/dependencies` |
| Monitor API | `/api/health/live` | `/api/health/ready` | `/api/health/dependencies` |

Admin API 的登录接口为 `POST /api/v1/auth/login`，请求体必须是 JSON；直接在浏览器地址栏打开该地址不会产生登录页面。

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

`status` 可取 `starting`、`ok`、`degraded`。

### 3.2 获取支持语言

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

当前语言代码：`zh`、`en`、`ja`、`ko`、`fr`、`de`、`es`、`ru`。

### 3.3 创建会话

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

| 字段 | 必填 | 说明 |
|---|---|---|
| `source_lang` | 是 | 源语言代码 |
| `target_lang` | 是 | 目标语言代码 |
| `input_type` | 否 | 音频来源，默认 `system_audio` |

响应示例：

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

### 3.4 查询会话

```http
GET /api/v1/sessions/{session_id}
```

请求体：无。

响应示例：

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
    "brief": "示例会话主题",
    "created_at": "2026-08-07T10:30:00+08:00"
  }
}
```

### 3.5 停止会话

```http
POST /api/v1/sessions/{session_id}/stop
```

请求体：无。

响应示例：

```json
{
  "code": 0,
  "message": "ok"
}
```

### 3.6 查询历史会话列表

```http
GET /api/v1/history
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
| `keyword` | 空 | 内容关键词 |

请求路径示例：

```http
GET /api/v1/history?offset=0&limit=20&source_lang=zh&target_lang=en
```

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
        "brief": "示例会话主题",
        "created_at": "2026-08-07T10:30:00+08:00",
        "ended_at": "2026-08-07T10:35:00+08:00"
      }
    ]
  }
}
```

### 3.7 查询历史会话详情

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
    "brief": "示例会话主题",
    "summary": "这是一段虚构的会话摘要。",
    "created_at": "2026-08-07T10:30:00+08:00",
    "ended_at": "2026-08-07T10:35:00+08:00",
    "records": [
      {
        "id": 101,
        "session_id": "11111111-2222-4333-8444-555555555555",
        "sequence": 1,
        "source_text": "这是一段虚构的示例文本。",
        "translated_text": "This is fictional sample text.",
        "confidence": 0,
        "audio_duration_ms": 0,
        "created_at": "2026-08-07T10:30:02+08:00"
      }
    ]
  }
}
```

### 3.8 生成历史会话摘要

```http
POST /api/v1/history/{session_id}/summary
```

请求体：无。

响应示例：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "summary": "这是一段虚构的会话摘要。"
  }
}
```

### 3.9 删除单条历史会话

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

### 3.10 批量删除历史会话

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

### 3.11 单句翻译

```http
POST /api/v1/translate
```

请求体：

```json
{
  "text": "这是一段虚构的示例文本。",
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
    "translated_text": "This is fictional sample text."
  }
}
```

## 4. WebSocket 同传协议

### 4.1 建立连接

客户端先创建会话，再使用返回的 `session_id` 建立 WebSocket：

```http
GET /api/v1/interpret?session_id=11111111-2222-4333-8444-555555555555
Upgrade: websocket
Connection: Upgrade
```

WebSocket 握手没有 JSON 请求体。

### 4.2 音频格式

音频通过 WebSocket Binary Message 发送，不包装为 JSON：

```text
格式：PCM signed 16-bit little-endian
采样率：16000 Hz
声道：单声道
```

### 4.3 通信时序

```text
创建会话
  → 建立 WebSocket
  → 发送 start
  → 接收 started
  → 持续发送二进制 PCM
  → 接收 transcript 和 translation
  → 发送 stop
  → 接收 stopped
```

### 4.4 客户端消息

#### start

```json
{
  "type": "start"
}
```

#### ping

```json
{
  "type": "ping"
}
```

#### stop

```json
{
  "type": "stop"
}
```

#### pause

暂停接收音频，但保持当前 WebSocket 和会话不结束：

```json
{
  "type": "pause"
}
```

#### resume

恢复接收音频：

```json
{
  "type": "resume"
}
```

服务端分别返回 `paused` 和 `resumed` 事件。当前协议支持 `start`、`ping`、`pause`、`resume`、`stop` 五种 JSON 控制消息。音频数据必须使用二进制帧。

### 4.5 服务端消息

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
  "text": "这是一段虚构的临时识别文本",
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
  "text": "This is fictional interim text.",
  "is_final": false,
  "reason": "partial",
  "timestamp": 1786071002323
}
```

翻译发生可恢复降级时，消息可能带有 `warning`：

```json
{
  "type": "translation",
  "seq": 13,
  "segment_id": 3,
  "revision": 5,
  "text": "这是一段虚构的示例文本。",
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

#### paused

```json
{
  "type": "paused",
  "timestamp_ms": 1786071005123
}
```

#### resumed

```json
{
  "type": "resumed",
  "timestamp_ms": 1786071006123
}
```

#### error

```json
{
  "type": "error",
  "message": "invalid request"
}
```

### 4.6 流式字段

| 字段 | 说明 |
|---|---|
| `seq` | 当前连接内递增的事件序号 |
| `segment_id` | 逻辑片段编号，相同编号表示同一片段 |
| `revision` | 片段修订版本，较大版本覆盖较小版本 |
| `is_final` | 是否为最终结果 |
| `reason` | 当前片段产生或提交的原因 |
| `timestamp` | Unix 毫秒时间戳 |
| `warning` | 可恢复降级提示 |

`reason` 可取：

| 值 | 含义 |
|---|---|
| `partial` | 中间结果 |
| `provider_final` | 上游确认结束 |
| `punctuation` | 终止标点提交 |
| `max_length` | 最大长度提交 |
| `max_duration` | 最大持续时间提交 |
| `soft_commit` | 等待窗口结束后提交 |
| `flush` | 停止时提交剩余文本 |

客户端应使用 `(segment_id, revision)` 更新流式内容，不能把每个中间结果都追加为新消息。

## 5. 维护规则

- 路由、请求字段、响应字段或状态码变化时同步更新本文档。
- WebSocket 新增字段必须保证旧客户端可以忽略。
- 改变现有字段语义时需要升级协议版本或提供兼容期。
- 示例只使用虚拟标识和虚构内容。
- 禁止提交真实服务地址、访问凭证、密钥、用户音频和会话内容。

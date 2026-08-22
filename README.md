# TMK — 同声传译系统

## 演示视频

[2026-06-07 演示视频](https://pan.baidu.com/s/1YppY-_WuNfAdLhnjVljyJg?pwd=hh46) — 功能演示，包含实时翻译流程及历史记录查看。

## 快速开始

从 [Releases](https://github.com/luoleixi/TMK/releases) 下载最新客户端即可使用。

## 项目介绍

TMK 是一个实时同声传译桌面应用，支持将麦克风或系统音频实时识别（ASR）并翻译为目标语言，适用于跨语言会议、视频通话、外语学习等场景。

> 部分代码来源于本人之前的项目 [MiniTMKAgent](https://github.com/luoleixi/MiniTMKAgent)。

### 核心功能

- **流式语音识别**：通过阿里云百炼 ASR 引擎（paraformer-realtime-v2），实时将语音转为文字
- **实时翻译**：识别结果即时翻译为目标语言，支持中、英、日、韩、法、德、西、俄 8 种语言
- **双音频源**：支持麦克风输入和系统音频环回（WASAPI Loopback）
- **低打扰桌面挂载**：桌面挂载字幕模式和历史记录会话模式，满足不同使用场景
- **会话管理**：完整的会话创建、暂停、恢复、停止控制，历史记录可回溯查看

## 文档索引

- [需求文档]()
- [系统架构]()
- [路线图]()
- [环境与发布规范]()
- [API与WebSocket协议]() 

服务端身份鉴权见 [docs/身份与权限设计.md](docs/身份与权限设计.md)，音频/文本对象存储与评测数据集见 [docs/对象存储与数据集设计.md](docs/对象存储与数据集设计.md)，后台评测队列与指标见 [docs/异步评测任务设计.md](docs/异步评测任务设计.md)，聚合仪表盘、审计查询与保留期治理见 [docs/仪表盘与生产数据治理设计.md](docs/仪表盘与生产数据治理设计.md)，Web 管理后台及部署方式见 [docs/TMK-Admin设计与部署.md](docs/TMK-Admin设计与部署.md)。

### 技术栈

| 组件 | 技术 |
|------|------|
| 后端框架 | Go + Gin |
| 数据库 | MySQL（SQLite 用于本地开发与测试） |
| 实时通信 | Gorilla WebSocket |
| 桌面框架 | Wails v3 |
| 前端 UI | React 18 + TypeScript + Vite |
| Web 管理后台 | React 18 + TypeScript + Vite，静态资源内嵌 Go 服务 |
| 音频采集 | Windows WaveIn / WASAPI (go-wca) |
| ASR 引擎 | 阿里云百炼 paraformer-realtime-v2 |
| 翻译引擎 | 阿里云百炼 qwen-turbo |

### 第三方库

**后端 (TMK-Glance)**

| 库 | 用途 |
|------|------|
| [gin-gonic/gin](https://github.com/gin-gonic/gin) | HTTP Web 框架，路由与中间件 |
| [gorilla/websocket](https://github.com/gorilla/websocket) | WebSocket 实现，实时双向通信 |
| [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) | 生产环境 MySQL 驱动 |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | 本地开发与测试存储，无需 CGO |
| [google/uuid](https://github.com/google/uuid) | UUID 生成，会话唯一标识 |
| [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) | YAML 配置文件解析 |

**客户端 (TMK-Client)**

| 库 | 用途 |
|------|------|
| [wailsapp/wails v3](https://github.com/wailsapp/wails) | 桌面应用框架，Go + Web 前端融合 |
| [gorilla/websocket](https://github.com/gorilla/websocket) | WebSocket 客户端，与服务端实时通信 |
| [moutend/go-wca](https://github.com/moutend/go-wca) | Windows Core Audio API 封装，系统音频环回采集 |
| [go-ole/go-ole](https://github.com/go-ole/go-ole) | Windows COM/OLE 绑定，音频设备操作底层依赖 |
| [react](https://react.dev) + [react-dom](https://react.dev) | 前端 UI 框架 |
| [@wailsio/runtime](https://www.npmjs.com/package/@wailsio/runtime) | Wails 前端运行时，JS 与 Go 方法互调 |
| [vite](https://vitejs.dev) | 前端构建工具 |
| [typescript](https://www.typescriptlang.org) | 类型安全的 JavaScript 超集 |

### 实时同传流程

```
用户语音 → 客户端音频捕获 → PCM 16kHz 16bit 单声道
    → WebSocket 发送至服务端 → ASR 语音识别
    → 翻译引擎翻译 → WebSocket 返回结果
    → 客户端实时展示字幕 + 持久化存储
```

## 未来规划

项目存在的问题及未来迭代计划，见 [docs/未来规划.md](docs/未来规划.md)。

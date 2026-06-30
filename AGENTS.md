### 项目背景

这是一个同声传译助手，已实现内容可阅读README.md文档

### 要实现的内容

#### 解决错误

- translate 接口吞错误，失败也返回 200（已实现）
- WebSocket `"audio"` text 消息发 JSON 而非 PCM（已实现）
- StartInterpret TOCTOU 竞态，Stop 被吞（已实现）
- handlePause/handleStop 缺 try/catch，失败 UI 冻结（已实现）

#### 实现功能

- 翻译错误降级：翻译失败返回原文 + warning 而非静默（已实现）
- ASR  WebSocket 并发写（已实现）
- WASAPI Stop 死锁： 加超时保护（已实现）
- DB错误处理：为所有DB出现err情况进行处理（已实现）

#### 功能迭代

- 桌面悬挂字幕：双窗口架构：frameless + AlwaysOnTop + 透明背景
-   用户设置持久化：语言偏好、设备选择存本地（已实现）
-   快捷键控制：全局热键 启动/暂停/停止 翻译
- 系统托盘：最小化到托盘，托盘图标控制

#### 体验优化

- 翻译记录导出：导出为 TXT/SRT 字幕文件
- 历史记录搜索：按关键词/日期搜索历史翻译
- 历史记录删除：单条/批量删除，API + UI
- AI总结和摘要：为历史会话添加AI会话总结以及摘要功能

#### 架构升级

- MySQL 替代 SQLite

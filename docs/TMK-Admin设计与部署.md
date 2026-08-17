# TMK-Admin 设计与部署

## 定位

TMK-Admin 是供管理员使用的 Web 工作台，覆盖用户与权限、音频/文本对象、评测数据集、异步评测、ASR/CER/WER 对比、分段评测、仪表盘、数据治理和审计日志。业务规则和权限判断仍由 TMK-Glance 执行，浏览器只负责操作和展示。

管理后台放在当前仓库的 `TMK-Admin` 目录，而不是单开仓库。它与服务端 API、数据库迁移和评测模型共同演进，在一次提交中完成接口与页面的兼容校验，避免跨仓库版本漂移。桌面客户端仍保持独立的 Windows 构建方式，不依赖 TMK-Admin。

## 页面职责

| 页面 | 主要能力 |
|---|---|
| 仪表盘 | 用户、会话、存储、数据集、评测质量与近期任务概览 |
| 用户 | 创建用户、修改角色与状态、要求下次登录改密 |
| 对象存储 | 上传音频/文本、筛选、下载和删除未引用对象 |
| 数据集 | 创建草稿、关联音频和参考文本、录入参考分段、发布或归档 |
| 评测任务 | 创建/取消异步任务，查看 CER、WER、分段 F1 和逐条文本对比 |
| 数据治理 | 展示过期、孤立、卡住或缺少成功评测的数据候选，不自动清理 |
| 审计日志 | 按动作、结果和资源筛选管理员操作记录 |

## 部署架构

TMK-Admin 不单独运行常驻 Node 服务。CI 使用 Vite 生成静态文件，Go 通过 `adminui` 构建标签把文件内嵌到 TMK-Glance 二进制。这样继续使用现有 Nginx、systemd、健康检查、版本目录和回滚机制，不新增端口、进程或服务器预算。

```text
浏览器
  -> Nginx /tmk-test/ 或 /tmk-production/
  -> 同一个 TMK-Glance 进程
       /admin/*  返回内嵌的 TMK-Admin
       /api/v1/* 执行业务 API 与 RBAC
  -> MySQL + 同机本地对象目录
```

对外入口：

- 测试：`https://117.72.159.185/tmk-test/admin/`
- 生产：`https://117.72.159.185/tmk-production/admin/`

Vite 在两个环境分别写入资源基准路径。浏览器根据当前 `/admin` 之前的路径自动推导 API 前缀，因此测试和生产产物不会混用后端地址。未知的后台页面路径回退到 `index.html`，静态哈希资源使用长期缓存，入口页禁止缓存。

不带 `adminui` 标签的本地 Go 构建会内嵌一个占位页，使后端单元测试不依赖 Node。正式部署二进制必须先构建前端，再使用 `go build -tags adminui`；流水线已经强制执行这一顺序。

## 身份与安全

后台复用 `/api/v1/auth/login`、`refresh`、`logout` 和 `change-password`。访问令牌与刷新令牌只保存在页面内存，不写入 localStorage、sessionStorage 或 IndexedDB；刷新或关闭页面后需要重新登录。所有管理 API 仍由服务端依次校验登录状态、首次改密状态和 `admin` 角色，页面隐藏按钮不构成授权边界。

静态响应设置 CSP、禁止 MIME 嗅探、禁止被 iframe 嵌入和不发送 Referer。下载请求也携带短期访问令牌。后续若把刷新令牌改为 HttpOnly Cookie，应同时增加 CSRF Token 校验，不能只切换 Cookie 存储方式。

## CI/CD

测试工作流在 `main` 推送后执行：

1. 后端竞态测试、Windows 客户端测试和 TMK-Admin 类型检查并行运行。
2. Admin 以 `/tmk-test/admin/` 构建，并作为独立工件传给服务端构建任务。
3. 服务端用 `adminui` 标签生成单一 Linux 二进制。
4. 受限部署账号上传并原子切换测试版本，最后检查 `/tmk-test/api/health/ready`；只有返回 HTTP 200 才认为版本已就绪。

生产工作流只能人工触发，并要求版本号和 `DEPLOY_PRODUCTION` 二次确认。它会以 `/tmk-production/admin/` 重新构建后台，等待 `production` Environment 的人工审批，部署并通过健康检查后才构建 Windows 正式客户端。流水线不会复制测试数据库、对象文件或密钥到生产环境。

## 本地开发

在 `TMK-Admin` 目录安装依赖后运行 `pnpm dev`，开发服务器把 `/api` 代理到 `127.0.0.1:18080`。本地 Go 测试默认使用占位资源；需要验证完整内嵌产物时，先运行 `ADMIN_BASE_PATH=/admin/ pnpm build`，再在 `TMK-Glance` 运行 `go test -tags adminui ./...`。

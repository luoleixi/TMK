# TMK-Admin 设计与部署

## 定位

TMK-Admin 是供管理员使用的 Web 工作台，覆盖用户与权限、音频/文本对象、评测数据集、异步评测、ASR/CER/WER 对比、分段评测、仪表盘、数据治理和审计日志。浏览器只调用 Admin API；Glance 保留核心业务接口，管理操作通过服务间认证访问 Glance。

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

TMK-Admin 是独立静态前端，Admin API 是独立 Go 服务。前端由 Nginx 静态托管，API 通过独立端口和路径反向代理。Glance 挂掉时管理站点仍可加载，Admin API 也可以独立扩容或迁移。

```text
浏览器
  -> Nginx /tmk-test/ 或 /tmk-production/
  -> Nginx
       /admin/*      返回 TMK-Admin 静态资源
       /admin-api/*  转发到 TMK-Admin-API
       /monitoring/* 转发到 Monitor API
       /api/v1/* 执行业务 API；管理 API 统一走 `/admin-api/api/v1/*`
  -> MySQL + 同机本地对象目录
```

对外入口：

- 测试：`https://117.72.159.185/tmk-test/admin/`
- 生产：`https://117.72.159.185/tmk-production/admin/`

Vite 在两个环境分别写入资源基准路径。浏览器根据当前 `/admin` 之前的路径自动推导 API 前缀，因此测试和生产产物不会混用后端地址。未知的后台页面路径回退到 `index.html`，静态哈希资源使用长期缓存，入口页禁止缓存。

前端构建和 Admin API 构建是两个独立工件。前端不再内嵌 Glance 二进制，CI 分别执行前端类型检查、静态构建和 Admin API 构建。

## 身份与安全

后台复用 `/api/v1/auth/login`、`refresh`、`logout` 和 `change-password`。访问令牌与刷新令牌只保存在页面内存，不写入 localStorage、sessionStorage 或 IndexedDB；刷新或关闭页面后需要重新登录。所有管理 API 仍由服务端依次校验登录状态、首次改密状态和 `admin` 角色，页面隐藏按钮不构成授权边界。

静态响应设置 CSP、禁止 MIME 嗅探、禁止被 iframe 嵌入和不发送 Referer。下载请求也携带短期访问令牌。后续若把刷新令牌改为 HttpOnly Cookie，应同时增加 CSRF Token 校验，不能只切换 Cookie 存储方式。

## CI/CD

测试工作流在 `main` 推送后执行：

1. 后端竞态测试、Windows 客户端测试和 TMK-Admin 类型检查并行运行。
2. Admin 以 `/tmk-test/admin/` 构建，并作为独立工件传给服务端构建任务。
3. Admin API 生成独立 Linux 服务工件，TMK-Admin 生成独立静态工件。
4. 受限部署账号分别上传并原子切换工件，依次检查 Admin API、Glance 和 Monitor 的健康接口；全部返回预期状态后才认为测试环境就绪。

生产工作流只能人工触发，并要求版本号和 `DEPLOY_PRODUCTION` 二次确认。它会以 `/tmk-production/admin/` 重新构建后台，等待 `production` Environment 的人工审批，部署并通过健康检查后才构建 Windows 正式客户端。流水线不会复制测试数据库、对象文件或密钥到生产环境。

## 本地开发

在 `TMK-Admin` 目录安装依赖后运行 `pnpm dev`，开发服务器将 `/admin-api` 代理到本地 Admin API。前端联调不要求启动 Glance 的管理路由。

# TMK 带外故障排查系统

## 目标

监控平面不使用 Admin API 的登录、会话、权限库或业务数据库。Glance、Admin API 或管理站点不可用时，运维人员仍可从 Monitor API 自带的应急页面查看故障信息。

当前组件部署在同一台服务器，但使用独立进程、端口、配置、凭据与数据目录。未来迁移只需将 Prometheus、Alertmanager、Vector 和 Monitor API 移到独立节点，并修改抓取目标及 Nginx 上游。

## 组件边界

| 组件 | 职责 | 默认端口/数据 |
| --- | --- | --- |
| Monitor API | 聚合故障上下文、提供应急页面、接收告警事件 | 测试 `19090`，生产 `29090` |
| Prometheus | 指标抓取、规则计算 | `9090` |
| Alertmanager | 告警分组、路由和通知 | `9093` |
| Blackbox Exporter | 从服务外部执行 HTTP 探测 | `9115` |
| Vector | 收集 systemd 与 Nginx 日志并规范化 | `/var/log/tmk/combined.jsonl` |
| GitHub Actions probe | 从服务器外部探测 Monitor 自身 | 每 10 分钟 |

Alertmanager 使用独立 Bearer 凭据调用 Monitor 告警入口。人工应急访问使用独立 HTTP Basic 凭据。二者均不依赖 Admin API。

## 数据模型

- 指标数据：Prometheus 时序指标，包括 HTTP、WebSocket、ASR、翻译、异步任务、数据库与存储。
- 告警数据：Alertmanager 当前状态，以及写入 `incidents.jsonl` 的历史告警事件。
- 日志数据：Vector 统一收集服务日志与 Nginx 日志，保留服务、环境、请求和链路字段。
- 部署和变更数据：每条部署脚本在成功或回滚失败时写入 `deployments.jsonl`。

Monitor 应急页按同一时间窗口展示业务依赖、当前告警、关键指标、故障事件、最近部署和关联日志，用于判断故障发生前是否存在发布或配置变化。

## 分层健康检查

1. `/api/health/live`：只表示 Monitor API 进程能响应，不检查任何依赖，供进程守护和外部探针使用。
2. `/api/health/ready`：检查 Prometheus 与 Alertmanager 是否可用。失败表示监控基础设施降级，不代表业务服务必然故障。
3. `/api/health/dependencies`：检查 Glance 与 Admin API。失败用于定位业务面或控制面故障，不影响 Monitor 自身存活。

外部 GitHub Actions 同时探测 `live` 和 `ready`。因此 Monitor 进程退出，或其依赖的 Prometheus/Alertmanager 失效，都能在服务器之外留下失败记录。

## 应急访问

入口为 `/tmk-test/monitoring/emergency/`。浏览器使用 `/etc/tmk-monitor/test.env` 中的独立 Basic Auth 凭据，不调用 Admin API。监控指标端点不通过公网 Nginx 暴露；Prometheus仅通过本机回环地址抓取。

生产环境只允许人工部署。测试和生产使用不同的 Monitor 进程、端口、数据目录和人工访问密码；Alertmanager 到 Monitor 的机器凭据由服务器初始化脚本统一生成和保存。

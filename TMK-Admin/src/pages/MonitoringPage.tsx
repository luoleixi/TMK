import { useEffect, useState } from "react";
import { Activity, AlertTriangle, CheckCircle2, RefreshCw, ServerCrash } from "lucide-react";
import { api } from "../api";
import { ErrorNotice, formatDate, Loading, Metric, PageHeader, Status } from "../components";
import type { MonitorAlert, MonitoringSummary } from "../types";

const metricLabels: Record<string, string> = {
  websocket_connections: "WebSocket 连接",
  http_p95_seconds: "HTTP P95",
  http_5xx_rate: "HTTP 5xx",
  audio_drop_rate: "音频丢帧率",
  asr_error_rate: "ASR 错误率",
  translation_error_rate: "翻译错误率",
  evaluation_queued: "评测排队",
  evaluation_running: "评测运行中",
  database_in_use: "数据库连接",
  storage_free_bytes: "对象存储剩余",
};

export default function MonitoringPage() {
  const [data, setData] = useState<MonitoringSummary>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = () => {
    setError("");
    setLoading(true);
    api.monitoringRequest<MonitoringSummary>("/summary")
      .then(setData)
      .catch((reason) => setError(reason.message))
      .finally(() => setLoading(false));
  };
  useEffect(() => {
    load();
    const timer = window.setInterval(load, 15000);
    return () => window.clearInterval(timer);
  }, []);

  return <>
    <PageHeader title="系统监控" description={data ? `独立监控源 · ${formatDate(data.generated_at)}` : "独立监控源与告警"} actions={<button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button>} />
    {error && <ErrorNotice message={error} onClose={() => setError("")} />}
    {loading && !data ? <Loading /> : data && <>
      <section className={`monitor-target ${data.target.up ? "is-up" : "is-down"}`}>
        {data.target.up ? <CheckCircle2 size={23} /> : <ServerCrash size={23} />}
        <div><strong>{data.target.up ? "TMK 服务正常" : "TMK 服务不可用"}</strong><span>{data.target.up ? `健康检查 ${data.target.latency_ms} ms · HTTP ${data.target.status_code}` : data.target.error || "健康检查失败"}</span></div>
      </section>
      <section className={`monitor-target ${data.admin_target.up ? "is-up" : "is-down"}`}>
        {data.admin_target.up ? <CheckCircle2 size={23} /> : <ServerCrash size={23} />}
        <div><strong>{data.admin_target.up ? "Control API 正常" : "Control API 不可用"}</strong><span>{data.admin_target.up ? `健康检查 ${data.admin_target.latency_ms} ms · HTTP ${data.admin_target.status_code}` : data.admin_target.error || "健康检查失败"}</span></div>
      </section>
      <section className="metric-grid monitoring-metrics">
        {Object.entries(data.metrics).map(([name, metric]) => <Metric key={name} label={metricLabels[name] || name} value={formatMetric(name, metric.value, metric.error)} tone={metric.error ? "warn" : metricTone(name, metric.value)} />)}
      </section>
      <section className="panel table-panel monitor-alerts">
        <div className="panel-heading"><div><h2><AlertTriangle size={18} />当前告警</h2><p>来自独立 Alertmanager 的活动告警</p></div><Activity size={19} /></div>
        {data.alerts.length === 0 ? <div className="monitor-clear"><CheckCircle2 size={19} />当前没有活动告警</div> : <div className="alert-list">{data.alerts.map((alert, index) => <AlertRow key={`${alert.labels.alertname || "alert"}-${index}`} alert={alert} />)}</div>}
      </section>
    </>}
  </>;
}

function AlertRow({ alert }: { alert: MonitorAlert }) {
  const name = alert.annotations.summary || alert.labels.alertname || "未命名告警";
  const detail = alert.annotations.description || alert.annotations.error || "无附加说明";
  return <article className={`alert-row alert-${alert.labels.severity || alert.state || "unknown"}`}><div><strong>{name}</strong><p>{detail}</p></div><div className="alert-meta"><Status value={alert.labels.severity || alert.state || "unknown"} />{alert.activeAt && <time>{formatDate(alert.activeAt)}</time>}</div></article>;
}

function formatMetric(name: string, value: number, error?: string): string {
  if (error) return "不可用";
  if (name.endsWith("_rate")) return `${(value * 100).toFixed(2)}%`;
  if (name.endsWith("_seconds")) return `${(value * 1000).toFixed(0)} ms`;
  if (name === "storage_free_bytes") return formatBytes(value);
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function metricTone(name: string, value: number): "neutral" | "good" | "warn" | "bad" {
  if (name === "application_ready") return value === 1 ? "good" : "bad";
  if (name.endsWith("_rate")) return value > 0.05 ? "bad" : value > 0.01 ? "warn" : "good";
  return "neutral";
}

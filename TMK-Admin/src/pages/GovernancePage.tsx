import { useEffect, useState, type ReactNode } from "react";
import { AlertTriangle, CheckCircle2, DatabaseBackup, HardDrive, KeyRound, RefreshCw } from "lucide-react";
import { api } from "../api";
import { ErrorNotice, formatBytes, Loading, Metric, PageHeader } from "../components";
import type { Governance } from "../types";

export default function GovernancePage() {
  const [data, setData] = useState<Governance>(); const [loading, setLoading] = useState(true); const [error, setError] = useState("");
  const load = () => { setLoading(true); api.request<Governance>("/admin/governance/report").then(setData).catch((reason) => setError(reason.message)).finally(() => setLoading(false)); };
  useEffect(load, []);
  if (loading && !data) return <Loading />;
  const risks = data ? [data.stale_draft_datasets, data.unreferenced_objects, data.ready_datasets_without_successful_evaluation, data.stuck_evaluation_jobs, data.expired_active_tokens, data.sessions_past_retention, data.evaluation_jobs_past_retention, data.audit_events_past_retention].filter(Boolean).length : 0;
  return <><PageHeader title="数据治理" description="生产数据生命周期与保留期风险" actions={<button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button>} />{error && <ErrorNotice message={error} onClose={() => setError("")} />}{data && <>
    <section className={`governance-summary ${risks ? "has-risks" : "clear"}`}>{risks ? <AlertTriangle size={23} /> : <CheckCircle2 size={23} />}<div><strong>{risks ? `${risks} 类项目需要复核` : "未发现治理风险"}</strong><span>报告只识别候选，不会自动删除生产数据。</span></div></section>
    <section className="metric-grid governance-metrics"><Metric label="长期草稿" value={data.stale_draft_datasets} detail={`超过 ${data.stale_draft_days} 天`} tone={data.stale_draft_datasets ? "warn" : "good"} /><Metric label="未引用对象" value={data.unreferenced_objects} detail={formatBytes(data.unreferenced_object_bytes)} tone={data.unreferenced_objects ? "warn" : "good"} /><Metric label="未成功评测" value={data.ready_datasets_without_successful_evaluation} detail="已发布数据集" tone={data.ready_datasets_without_successful_evaluation ? "bad" : "good"} /><Metric label="卡住任务" value={data.stuck_evaluation_jobs} detail={`超过 ${data.stuck_job_minutes} 分钟`} tone={data.stuck_evaluation_jobs ? "bad" : "good"} /></section>
    <section className="governance-grid"><GovernanceGroup icon={<KeyRound size={19} />} title="凭据"><Row label="已过期但未撤销" value={data.expired_active_tokens} /><Row label="可维护令牌" value={data.revoked_or_expired_tokens} /></GovernanceGroup><GovernanceGroup icon={<DatabaseBackup size={19} />} title="保留期"><Row label={`会话 · ${data.session_retention_days} 天`} value={data.sessions_past_retention} /><Row label={`评测 · ${data.evaluation_retention_days} 天`} value={data.evaluation_jobs_past_retention} /><Row label={`审计 · ${data.audit_retention_days} 天`} value={data.audit_events_past_retention} /></GovernanceGroup><GovernanceGroup icon={<HardDrive size={19} />} title="磁盘"><Row label="可用空间" value={formatBytes(data.disk_free_bytes)} /><Row label="强制预留" value={formatBytes(data.disk_reserve_bytes)} /></GovernanceGroup></section>
  </>}</>;
}

function GovernanceGroup({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) { return <section className="panel governance-group"><div className="panel-heading"><h2>{icon}{title}</h2></div><dl className="definition-list">{children}</dl></section>; }
function Row({ label, value }: { label: string; value: string | number }) { return <div><dt>{label}</dt><dd>{value}</dd></div>; }

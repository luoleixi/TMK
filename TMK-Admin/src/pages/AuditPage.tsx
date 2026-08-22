import { FormEvent, useEffect, useState } from "react";
import { Filter, RefreshCw } from "lucide-react";
import { api } from "../api";
import { ErrorNotice, formatDate, Loading, PageHeader, Pagination, Status } from "../components";
import type { AuditEvent } from "../types";

const limit = 50;
export default function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]); const [total, setTotal] = useState(0); const [offset, setOffset] = useState(0); const [query, setQuery] = useState(""); const [loading, setLoading] = useState(true); const [error, setError] = useState("");
  const load = () => { setLoading(true); api.request<{ events: AuditEvent[]; total: number }>(`/admin/audit-logs?limit=${limit}&offset=${offset}${query}`).then((data) => { setEvents(data.events); setTotal(data.total); }).catch((reason) => setError(reason.message)).finally(() => setLoading(false)); };
  useEffect(load, [offset, query]);
  const filter = (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); const form = new FormData(event.currentTarget); const parameters = new URLSearchParams(); for (const [key, value] of form) if (String(value).trim()) parameters.set(key, String(value).trim()); setOffset(0); setQuery(parameters.size ? `&${parameters}` : ""); };
  return <><PageHeader title="审计日志" description="安全与高风险操作的只追加记录" actions={<button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button>} />{error && <ErrorNotice message={error} onClose={() => setError("")} />}
    <form className="filter-bar" onSubmit={filter}><label>动作<input name="action" placeholder="例如 dataset.ready" /></label><label>结果<select name="result"><option value="">全部</option><option value="success">成功</option><option value="denied">拒绝</option></select></label><label>资源类型<input name="resource_type" placeholder="dataset" /></label><button className="button-secondary"><Filter size={16} />筛选</button></form>
    {loading ? <Loading /> : <section className="panel table-panel"><div className="table-scroll"><table><thead><tr><th>时间</th><th>动作</th><th>结果</th><th>资源</th><th>操作者</th><th>来源 IP</th><th>详情</th></tr></thead><tbody>{events.map((event) => <tr key={event.id}><td>{formatDate(event.created_at)}</td><td className="mono">{event.action}</td><td><Status value={event.result} /></td><td>{event.resource_type}<span className="cell-subtitle mono">{event.resource_id || "-"}</span></td><td className="mono">{event.actor_user_id?.slice(0, 8) || "system"}</td><td className="mono">{event.ip_address || "-"}</td><td><code className="details-json">{JSON.stringify(event.details)}</code></td></tr>)}</tbody></table></div><Pagination offset={offset} limit={limit} total={total} onChange={setOffset} /></section>}
  </>;
}

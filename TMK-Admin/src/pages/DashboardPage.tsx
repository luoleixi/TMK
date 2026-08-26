import { useEffect, useState } from "react";
import { Activity, Database, HardDrive, RefreshCw, Users, Radio } from "lucide-react";
import { api } from "../api";
import { Empty, ErrorNotice, formatBytes, formatDate, formatRate, Loading, Metric, PageHeader, Status } from "../components";
import type { Dashboard, SegmenterRuntimeConfig } from "../types";

export default function DashboardPage() {
  const [data, setData] = useState<Dashboard>();
  const [days, setDays] = useState(30);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const load = () => {
    setLoading(true); setError("");
    api.request<Dashboard>(`/admin/dashboard?days=${days}`).then(setData).catch((reason) => setError(reason.message)).finally(() => setLoading(false));
  };
  useEffect(load, [days]);
  if (loading && !data) return <Loading />;
  return <>
    <PageHeader title="仪表盘" description={data ? `更新于 ${formatDate(data.generated_at)}` : undefined} actions={<>
      <select value={days} onChange={(event) => setDays(Number(event.target.value))} aria-label="统计窗口"><option value={7}>近 7 天</option><option value={30}>近 30 天</option><option value={90}>近 90 天</option></select>
      <button className="icon-button" onClick={load} title="刷新"><RefreshCw size={18} /></button>
    </>} />
    {error && <ErrorNotice message={error} onClose={() => setError("")} />}
    {data && <>
      <section className="metric-grid">
        <Metric label="启用用户" value={data.users.active} detail={`${data.users.administrators} 位管理员`} />
        <Metric label="窗口内会话" value={data.sessions.in_window} detail={`${data.sessions.records} 条累计记录`} />
        <Metric label="评测样本" value={data.evaluations.completed_items} detail={`${data.evaluations.failed_items} 条失败`} tone={data.evaluations.failed_items ? "warn" : "good"} />
        <Metric label="对象存储" value={formatBytes(data.storage.bytes)} detail={`${data.storage.objects} 个文件`} />
      </section>
      <SegmenterControl />

      <section className="dashboard-grid">
        <div className="panel trend-panel">
          <div className="panel-heading"><div><h2>运行趋势</h2><p>会话与离线评测处理量</p></div><Activity size={19} /></div>
          <Trend data={data.daily} />
        </div>
        <div className="panel quality-panel">
          <div className="panel-heading"><div><h2>质量基线</h2><p>所有已完成评测的全语料指标</p></div><Database size={19} /></div>
          <div className="quality-grid">
            <Metric label="ASR CER" value={formatRate(data.evaluations.asr_cer)} />
            <Metric label="ASR WER" value={formatRate(data.evaluations.asr_wer)} />
            <Metric label="分段 CER" value={formatRate(data.evaluations.segmented_cer)} />
            <Metric label="分段 F1" value={data.evaluations.segment_evaluable ? formatRate(data.evaluations.segment_f1) : "未标注"} />
          </div>
        </div>
        <div className="panel compact-panel">
          <div className="panel-heading"><h2><Users size={18} />账户</h2></div>
          <dl className="definition-list"><div><dt>全部</dt><dd>{data.users.total}</dd></div><div><dt>已禁用</dt><dd>{data.users.disabled}</dd></div><div><dt>活跃会话</dt><dd>{data.sessions.active}</dd></div><div><dt>异常会话</dt><dd>{data.sessions.failed}</dd></div></dl>
        </div>
        <div className="panel compact-panel">
          <div className="panel-heading"><h2><HardDrive size={18} />资料库</h2></div>
          <dl className="definition-list"><div><dt>数据集</dt><dd>{data.datasets.total}</dd></div><div><dt>待发布</dt><dd>{data.datasets.draft}</dd></div><div><dt>音频</dt><dd>{data.storage.audio_files}</dd></div><div><dt>磁盘可用</dt><dd>{formatBytes(data.storage.disk_free_bytes)}</dd></div></dl>
        </div>
      </section>

      <section className="panel table-panel">
        <div className="panel-heading"><div><h2>最近评测</h2><p>最新 10 个任务</p></div></div>
        {data.recent_evaluation_jobs.length === 0 ? <Empty title="暂无评测任务" description="发布数据集后创建第一条评测任务。" /> : <div className="table-scroll"><table><thead><tr><th>任务</th><th>语言</th><th>状态</th><th>进度</th><th>ASR CER</th><th>分段 F1</th><th>创建时间</th></tr></thead><tbody>{data.recent_evaluation_jobs.map((job) => <tr key={job.id}><td className="mono">{job.id.slice(0, 8)}</td><td>{job.dataset_language}</td><td><Status value={job.status} /></td><td>{Math.round(job.progress * 100)}%</td><td>{formatRate(job.asr_cer)}</td><td>{job.segment_evaluable ? formatRate(job.segment_f1) : "-"}</td><td>{formatDate(job.created_at)}</td></tr>)}</tbody></table></div>}
      </section>
    </>}
  </>;
}

function SegmenterControl() {
  const [config, setConfig] = useState<SegmenterRuntimeConfig>(); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  useEffect(() => { api.request<SegmenterRuntimeConfig>("/admin/settings/segmenter").then(setConfig).catch((reason) => setError(reason.message)); }, []);
  const toggle = async () => { if (!config) return; setBusy(true); try { setConfig(await api.request<SegmenterRuntimeConfig>("/admin/settings/segmenter", { method: "PUT", body: JSON.stringify({ enabled: !config.enabled, rollout_percent: config.rollout_percent || 100, version: config.version, change_reason: `管理后台${config.enabled ? "关闭" : "启用"}实时分段器` }) })); } catch (reason) { setError((reason as Error).message); } finally { setBusy(false); } };
  return <section className="panel compact-panel"><div className="panel-heading"><div><h2><Radio size={18} />实时分段器</h2><p>仅影响新建实时会话，A/B 评测使用固定变体配置</p></div><button className="button-secondary" onClick={toggle} disabled={!config || busy}>{config?.enabled ? "关闭" : "开启"}</button></div>{error && <ErrorNotice message={error} />}<dl className="definition-list"><div><dt>运行状态</dt><dd>{config ? (config.enabled ? "已启用" : "已关闭") : "加载中"}</dd></div><div><dt>目标 revision</dt><dd>{config?.revision ?? "-"}</dd></div><div><dt>应用状态</dt><dd>{config?.status ?? "-"}</dd></div></dl></section>;
}

function Trend({ data }: { data: Dashboard["daily"] }) {
  const max = Math.max(1, ...data.flatMap((point) => [point.sessions, point.evaluation_items]));
  return <div className="trend" aria-label="每日运行趋势">
    <div className="trend-legend"><span><i className="legend-sessions" />会话</span><span><i className="legend-evaluations" />评测样本</span></div>
    <div className="trend-bars">{data.map((point) => <div className="trend-day" key={point.date} title={`${point.date}：${point.sessions} 会话，${point.evaluation_items} 样本`}>
      <div className="bar-pair"><i className="bar-sessions" style={{ height: `${Math.max(2, point.sessions / max * 100)}%` }} /><i className="bar-evaluations" style={{ height: `${Math.max(2, point.evaluation_items / max * 100)}%` }} /></div>
      {(data.length <= 14 || point.date.endsWith("-01") || point === data[data.length - 1]) && <span>{point.date.slice(5)}</span>}
    </div>)}</div>
  </div>;
}

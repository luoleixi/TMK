import { FormEvent, useEffect, useState } from "react";
import { Archive, CheckCircle2, ChevronRight, Plus, RefreshCw, Trash2 } from "lucide-react";
import { api } from "../api";
import { Empty, ErrorNotice, formatDate, Loading, Modal, PageHeader, Status, SuccessNotice } from "../components";
import type { Dataset, DatasetItem, StorageObject } from "../types";

export default function DatasetsPage() {
  const [datasets, setDatasets] = useState<Dataset[]>([]); const [selected, setSelected] = useState<string>();
  const [detail, setDetail] = useState<{ dataset: Dataset; items: DatasetItem[] }>();
  const [status, setStatus] = useState(""); const [creating, setCreating] = useState(false); const [adding, setAdding] = useState(false);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(""); const [success, setSuccess] = useState("");
  const load = () => { setLoading(true); api.request<{ datasets: Dataset[] }>(`/admin/datasets?limit=100${status ? `&status=${status}` : ""}`).then((result) => { setDatasets(result.datasets); if (selected && !result.datasets.some((item) => item.id === selected)) setSelected(undefined); }).catch((reason) => setError(reason.message)).finally(() => setLoading(false)); };
  const loadDetail = (id: string) => api.request<{ dataset: Dataset; items: DatasetItem[] }>(`/admin/datasets/${id}`).then(setDetail).catch((reason) => setError(reason.message));
  useEffect(load, [status]); useEffect(() => { if (selected) loadDetail(selected); else setDetail(undefined); }, [selected]);
  const transition = async (action: "ready" | "archive") => {
    if (!selected) return; try { await api.request(`/admin/datasets/${selected}/${action}`, { method: "POST" }); setSuccess(action === "ready" ? "数据集已发布并冻结" : "数据集已归档"); load(); loadDetail(selected); } catch (reason) { setError((reason as Error).message); }
  };
  const removeItem = async (id: string) => { if (!selected) return; try { await api.request(`/admin/datasets/${selected}/items/${id}`, { method: "DELETE" }); setSuccess("条目已移除"); load(); loadDetail(selected); } catch (reason) { setError((reason as Error).message); } };
  return <>
    <PageHeader title="数据集" description="音频、参考文本与人工分段标注" actions={<><select value={status} onChange={(event) => setStatus(event.target.value)} aria-label="数据集状态"><option value="">全部状态</option><option value="draft">草稿</option><option value="ready">已发布</option><option value="archived">已归档</option></select><button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button><button className="button" onClick={() => setCreating(true)}><Plus size={17} />新建数据集</button></>} />
    {error && <ErrorNotice message={error} onClose={() => setError("")} />}{success && <SuccessNotice message={success} />}
    <div className="split-view">
      <section className="panel list-panel">{loading ? <Loading /> : datasets.length === 0 ? <Empty title="没有数据集" description="创建草稿并加入音频与参考文本。" /> : datasets.map((dataset) => <button key={dataset.id} className={`dataset-row ${selected === dataset.id ? "selected" : ""}`} onClick={() => setSelected(dataset.id)}><div><strong>{dataset.name}</strong><span>{dataset.language} · {dataset.item_count} 条 · r{dataset.revision}</span></div><Status value={dataset.status} /><ChevronRight size={17} /></button>)}</section>
      <section className="panel detail-panel">{!detail ? <Empty title="选择数据集" description="查看条目、发布状态与修订信息。" /> : <>
        <div className="detail-heading"><div><h2>{detail.dataset.name}</h2><p>{detail.dataset.description || "无描述"}</p></div><Status value={detail.dataset.status} /></div>
        <dl className="definition-list horizontal"><div><dt>语言</dt><dd>{detail.dataset.language}</dd></div><div><dt>修订</dt><dd>{detail.dataset.revision}</dd></div><div><dt>条目</dt><dd>{detail.dataset.item_count}</dd></div><div><dt>更新</dt><dd>{formatDate(detail.dataset.updated_at)}</dd></div></dl>
        <div className="detail-actions">{detail.dataset.status === "draft" && <><button className="button-secondary" onClick={() => setAdding(true)}><Plus size={16} />添加条目</button><button className="button" disabled={!detail.items.length} onClick={() => transition("ready")}><CheckCircle2 size={16} />发布并冻结</button></>}{detail.dataset.status === "ready" && <button className="button-secondary" onClick={() => transition("archive")}><Archive size={16} />归档</button>}</div>
        <div className="table-scroll"><table><thead><tr><th>#</th><th>音频</th><th>参考文本</th><th>人工分段</th><th aria-label="操作" /></tr></thead><tbody>{detail.items.map((item) => <tr key={item.id}><td>{item.sequence}</td><td>{item.audio_original_name}</td><td>{item.text_original_name}</td><td>{item.reference_segments?.length || 0}</td><td>{detail.dataset.status === "draft" && <button className="icon-button danger" title="移除条目" onClick={() => removeItem(item.id)}><Trash2 size={16} /></button>}</td></tr>)}</tbody></table></div>
      </>}</section>
    </div>
    {creating && <CreateDatasetModal onClose={() => setCreating(false)} onDone={(id) => { setCreating(false); setSuccess("数据集草稿已创建"); load(); setSelected(id); }} onError={setError} />}
    {adding && selected && <AddItemModal datasetID={selected} onClose={() => setAdding(false)} onDone={() => { setAdding(false); setSuccess("数据集条目已添加"); load(); loadDetail(selected); }} onError={setError} />}
  </>;
}

function CreateDatasetModal({ onClose, onDone, onError }: { onClose: () => void; onDone: (id: string) => void; onError: (message: string) => void }) {
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); setBusy(true); const form = new FormData(event.currentTarget); try { const dataset = await api.request<Dataset>("/admin/datasets", { method: "POST", body: JSON.stringify(Object.fromEntries(form)) }); onDone(dataset.id); } catch (reason) { onError((reason as Error).message); setBusy(false); } };
  return <Modal title="新建数据集" onClose={onClose}><form className="form" onSubmit={submit}><label>名称<input name="name" required autoFocus /></label><label>描述<textarea name="description" rows={3} /></label><label>语言<select name="language" defaultValue="zh"><option value="zh">中文</option><option value="en">英语</option><option value="ja">日语</option><option value="ko">韩语</option><option value="fr">法语</option><option value="de">德语</option><option value="es">西班牙语</option><option value="ru">俄语</option></select></label><div className="form-actions"><button type="button" className="button-secondary" onClick={onClose}>取消</button><button className="button" disabled={busy}>创建</button></div></form></Modal>;
}

function AddItemModal({ datasetID, onClose, onDone, onError }: { datasetID: string; onClose: () => void; onDone: () => void; onError: (message: string) => void }) {
  const [audio, setAudio] = useState<StorageObject[]>([]); const [text, setText] = useState<StorageObject[]>([]); const [busy, setBusy] = useState(false);
  useEffect(() => { Promise.all([api.request<{ objects: StorageObject[] }>("/admin/objects?kind=audio&limit=100"), api.request<{ objects: StorageObject[] }>("/admin/objects?kind=text&limit=100")]).then(([a, t]) => { setAudio(a.objects); setText(t.objects); }).catch((reason) => onError(reason.message)); }, []);
  const submit = async (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); setBusy(true); const form = new FormData(event.currentTarget); const lines = String(form.get("segments") || "").split("\n").map((value) => value.trim()).filter(Boolean); const payload = { audio_object_id: form.get("audio_object_id"), reference_text_object_id: form.get("reference_text_object_id"), reference_segments: lines.map((value) => ({ text: value })) }; try { await api.request(`/admin/datasets/${datasetID}/items`, { method: "POST", body: JSON.stringify(payload) }); onDone(); } catch (reason) { onError((reason as Error).message); setBusy(false); } };
  return <Modal title="添加评测条目" onClose={onClose}><form className="form" onSubmit={submit}><label>音频<select name="audio_object_id" required autoFocus><option value="">选择音频</option>{audio.map((item) => <option key={item.id} value={item.id}>{item.original_name}</option>)}</select></label><label>参考文本<select name="reference_text_object_id" required><option value="">选择文本</option>{text.map((item) => <option key={item.id} value={item.id}>{item.original_name}</option>)}</select></label><label>人工分段<textarea name="segments" rows={6} placeholder="每行一个参考分段；留空则只计算 CER/WER" /></label><div className="form-actions"><button type="button" className="button-secondary" onClick={onClose}>取消</button><button className="button" disabled={busy || !audio.length || !text.length}>添加</button></div></form></Modal>;
}

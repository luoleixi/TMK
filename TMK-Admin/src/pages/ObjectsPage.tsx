import { ChangeEvent, useEffect, useRef, useState } from "react";
import { Download, FileAudio, FileText, RefreshCw, Trash2, Upload } from "lucide-react";
import { api } from "../api";
import { Empty, ErrorNotice, formatBytes, formatDate, Loading, PageHeader, Status, SuccessNotice } from "../components";
import type { StorageObject } from "../types";

export default function ObjectsPage() {
  const [objects, setObjects] = useState<StorageObject[]>([]);
  const [kind, setKind] = useState<"" | "audio" | "text">("");
  const [uploadKind, setUploadKind] = useState<"audio" | "text">("audio");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(""); const [success, setSuccess] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const load = () => { setLoading(true); api.request<{ objects: StorageObject[] }>(`/admin/objects?limit=100${kind ? `&kind=${kind}` : ""}`).then((result) => setObjects(result.objects)).catch((reason) => setError(reason.message)).finally(() => setLoading(false)); };
  useEffect(load, [kind]);
  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]; if (!file) return;
    try { await api.upload(uploadKind, file); setSuccess(`${file.name} 已上传`); load(); }
    catch (reason) { setError((reason as Error).message); }
    event.target.value = "";
  };
  const remove = async (object: StorageObject) => {
    if (!window.confirm(`删除 ${object.original_name}？被数据集引用的对象不会被删除。`)) return;
    try { await api.request(`/admin/objects/${object.id}`, { method: "DELETE" }); setSuccess("对象已删除"); load(); }
    catch (reason) { setError((reason as Error).message); }
  };
  const download = async (object: StorageObject) => {
    try { const blob = await api.download(`/admin/objects/${object.id}/content`); const url = URL.createObjectURL(blob); const anchor = document.createElement("a"); anchor.href = url; anchor.download = object.original_name; anchor.click(); URL.revokeObjectURL(url); }
    catch (reason) { setError((reason as Error).message); }
  };
  return <>
    <PageHeader title="对象存储" description="音频与参考文本资料库" actions={<>
      <select value={kind} onChange={(event) => setKind(event.target.value as typeof kind)} aria-label="对象类型"><option value="">全部类型</option><option value="audio">音频</option><option value="text">文本</option></select>
      <select value={uploadKind} onChange={(event) => setUploadKind(event.target.value as typeof uploadKind)} aria-label="上传类型"><option value="audio">上传音频</option><option value="text">上传文本</option></select>
      <input ref={input} className="visually-hidden" type="file" onChange={upload} accept={uploadKind === "audio" ? ".wav,.mp3,.flac,.ogg,.m4a,.webm" : ".txt,.md,.json,.srt,.vtt"} />
      <button className="button" onClick={() => input.current?.click()}><Upload size={17} />选择文件</button><button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button>
    </>} />
    {error && <ErrorNotice message={error} onClose={() => setError("")} />}{success && <SuccessNotice message={success} />}
    {loading ? <Loading /> : objects.length === 0 ? <Empty title="没有文件" description="上传音频或参考文本后可组装评测数据集。" /> : <section className="panel table-panel"><div className="table-scroll"><table><thead><tr><th>文件</th><th>类型</th><th>大小</th><th>SHA-256</th><th>状态</th><th>上传时间</th><th aria-label="操作" /></tr></thead><tbody>{objects.map((object) => <tr key={object.id}><td><span className="file-name">{object.kind === "audio" ? <FileAudio size={18} /> : <FileText size={18} />}<strong>{object.original_name}</strong></span></td><td>{object.content_type}</td><td>{formatBytes(object.size_bytes)}</td><td className="mono hash">{object.sha256}</td><td><Status value={object.status} /></td><td>{formatDate(object.created_at)}</td><td className="row-actions"><button className="icon-button" title="下载" onClick={() => download(object)}><Download size={17} /></button><button className="icon-button danger" title="删除" onClick={() => remove(object)}><Trash2 size={17} /></button></td></tr>)}</tbody></table></div></section>}
  </>;
}

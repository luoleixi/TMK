import type { ReactNode } from "react";
import { AlertCircle, CheckCircle2, ChevronLeft, ChevronRight, LoaderCircle, X } from "lucide-react";

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return <header className="page-header">
    <div><h1>{title}</h1>{description && <p>{description}</p>}</div>
    {actions && <div className="page-actions">{actions}</div>}
  </header>;
}

export function Metric({ label, value, detail, tone = "neutral" }: { label: string; value: ReactNode; detail?: string; tone?: "neutral" | "good" | "warn" | "bad" }) {
  return <div className={`metric metric-${tone}`}><span>{label}</span><strong>{value}</strong>{detail && <small>{detail}</small>}</div>;
}

export function Status({ value }: { value: string }) {
  const tone = ["succeeded", "ready", "active", "success", "completed"].includes(value) ? "good"
    : ["failed", "error", "denied", "disabled"].includes(value) ? "bad"
      : ["running", "completed_with_errors"].includes(value) ? "warn" : "neutral";
  return <span className={`status status-${tone}`}>{value.split("_").join(" ")}</span>;
}

export function Empty({ title, description }: { title: string; description: string }) {
  return <div className="empty"><AlertCircle size={22} /><strong>{title}</strong><span>{description}</span></div>;
}

export function Loading() {
  return <div className="loading"><LoaderCircle size={22} className="spin" />正在加载</div>;
}

export function ErrorNotice({ message, onClose }: { message: string; onClose?: () => void }) {
  return <div className="notice notice-error"><AlertCircle size={18} /><span>{message}</span>{onClose && <button className="icon-button" onClick={onClose} title="关闭"><X size={16} /></button>}</div>;
}

export function SuccessNotice({ message }: { message: string }) {
  return <div className="notice notice-success"><CheckCircle2 size={18} /><span>{message}</span></div>;
}

export function Pagination({ offset, limit, total, onChange }: { offset: number; limit: number; total: number; onChange: (offset: number) => void }) {
  const end = Math.min(offset + limit, total);
  return <div className="pagination"><span>{total === 0 ? "0" : `${offset + 1}-${end}`} / {total}</span>
    <button className="icon-button" title="上一页" disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}><ChevronLeft size={18} /></button>
    <button className="icon-button" title="下一页" disabled={end >= total} onClick={() => onChange(offset + limit)}><ChevronRight size={18} /></button>
  </div>;
}

export function Modal({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  return <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <section className="modal" role="dialog" aria-modal="true" aria-label={title}>
      <header><h2>{title}</h2><button className="icon-button" onClick={onClose} title="关闭"><X size={18} /></button></header>
      {children}
    </section>
  </div>;
}

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function formatRate(value: number | undefined): string {
  return value == null || !Number.isFinite(value) ? "-" : `${(value * 100).toFixed(2)}%`;
}

export function formatDate(value?: string): string {
  return value ? new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value)) : "-";
}

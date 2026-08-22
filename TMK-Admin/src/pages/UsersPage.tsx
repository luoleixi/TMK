import { FormEvent, useEffect, useState } from "react";
import { Plus, RefreshCw, UserRoundCog } from "lucide-react";
import { api } from "../api";
import { ErrorNotice, formatDate, Loading, Modal, PageHeader, Status, SuccessNotice } from "../components";
import type { User } from "../types";

export default function UsersPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<User>();
  const load = () => { setLoading(true); api.request<{ users: User[] }>("/admin/users?limit=100").then((result) => setUsers(result.users)).catch((reason) => setError(reason.message)).finally(() => setLoading(false)); };
  useEffect(load, []);
  return <>
    <PageHeader title="用户" description="账户、角色与访问状态" actions={<><button className="icon-button" title="刷新" onClick={load}><RefreshCw size={18} /></button><button className="button" onClick={() => setCreating(true)}><Plus size={17} />新建用户</button></>} />
    {error && <ErrorNotice message={error} onClose={() => setError("")} />}{success && <SuccessNotice message={success} />}
    {loading ? <Loading /> : <section className="panel table-panel"><div className="table-scroll"><table><thead><tr><th>用户</th><th>角色</th><th>状态</th><th>首次改密</th><th>最近登录</th><th aria-label="操作" /></tr></thead><tbody>{users.map((user) => <tr key={user.id}><td><strong>{user.display_name || "未命名"}</strong><span className="cell-subtitle">{user.email}</span></td><td>{user.role}</td><td><Status value={user.status} /></td><td>{user.must_change_password ? "需要" : "已完成"}</td><td>{formatDate(user.last_login_at)}</td><td><button className="icon-button" title="编辑用户" onClick={() => setEditing(user)}><UserRoundCog size={18} /></button></td></tr>)}</tbody></table></div></section>}
    {creating && <CreateUserModal onClose={() => setCreating(false)} onDone={() => { setCreating(false); setSuccess("用户已创建，首次登录需要修改密码"); load(); }} onError={setError} />}
    {editing && <EditUserModal user={editing} onClose={() => setEditing(undefined)} onDone={() => { setEditing(undefined); setSuccess("用户设置已更新"); load(); }} onError={setError} />}
  </>;
}

function CreateUserModal({ onClose, onDone, onError }: { onClose: () => void; onDone: () => void; onError: (message: string) => void }) {
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setBusy(true);
    const form = new FormData(event.currentTarget);
    try { await api.request("/admin/users", { method: "POST", body: JSON.stringify(Object.fromEntries(form)) }); onDone(); }
    catch (reason) { onError((reason as Error).message); setBusy(false); }
  };
  return <Modal title="新建用户" onClose={onClose}><form className="form" onSubmit={submit}>
    <label>邮箱<input name="email" type="email" required autoFocus /></label><label>显示名称<input name="display_name" required /></label>
    <label>临时密码<input name="password" type="password" minLength={12} maxLength={72} required /></label>
    <label>角色<select name="role" defaultValue="user"><option value="user">普通用户</option><option value="admin">管理员</option></select></label>
    <div className="form-actions"><button type="button" className="button-secondary" onClick={onClose}>取消</button><button className="button" disabled={busy}>{busy ? "创建中" : "创建"}</button></div>
  </form></Modal>;
}

function EditUserModal({ user, onClose, onDone, onError }: { user: User; onClose: () => void; onDone: () => void; onError: (message: string) => void }) {
  const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setBusy(true); const form = new FormData(event.currentTarget);
    const payload = { display_name: form.get("display_name"), role: form.get("role"), status: form.get("status"), must_change_password: form.get("must_change_password") === "on" };
    try { await api.request(`/admin/users/${user.id}`, { method: "PATCH", body: JSON.stringify(payload) }); onDone(); }
    catch (reason) { onError((reason as Error).message); setBusy(false); }
  };
  return <Modal title="编辑用户" onClose={onClose}><form className="form" onSubmit={submit}>
    <div className="form-context">{user.email}</div><label>显示名称<input name="display_name" defaultValue={user.display_name} required autoFocus /></label>
    <div className="form-row"><label>角色<select name="role" defaultValue={user.role}><option value="user">普通用户</option><option value="admin">管理员</option></select></label><label>状态<select name="status" defaultValue={user.status}><option value="active">启用</option><option value="disabled">禁用</option></select></label></div>
    <label className="checkbox"><input name="must_change_password" type="checkbox" defaultChecked={user.must_change_password} />下次登录强制改密</label>
    <div className="form-actions"><button type="button" className="button-secondary" onClick={onClose}>取消</button><button className="button" disabled={busy}>保存</button></div>
  </form></Modal>;
}

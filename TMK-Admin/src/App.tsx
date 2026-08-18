import { FormEvent, useEffect, useState } from "react";
import { Activity, BellRing, ClipboardList, Database, FileStack, Gauge, LogOut, Menu, ShieldCheck, Users, X } from "lucide-react";
import { api } from "./api";
import { ErrorNotice } from "./components";
import type { User } from "./types";
import AuditPage from "./pages/AuditPage";
import DashboardPage from "./pages/DashboardPage";
import DatasetsPage from "./pages/DatasetsPage";
import EvaluationsPage from "./pages/EvaluationsPage";
import GovernancePage from "./pages/GovernancePage";
import ObjectsPage from "./pages/ObjectsPage";
import UsersPage from "./pages/UsersPage";
import MonitoringPage from "./pages/MonitoringPage";

const navigation = [
  { id: "dashboard", label: "仪表盘", icon: Gauge },
  { id: "users", label: "用户", icon: Users },
  { id: "objects", label: "对象存储", icon: FileStack },
  { id: "datasets", label: "数据集", icon: Database },
  { id: "evaluations", label: "评测任务", icon: Activity },
  { id: "governance", label: "数据治理", icon: ShieldCheck },
  { id: "audit", label: "审计日志", icon: ClipboardList },
  { id: "monitoring", label: "系统监控", icon: BellRing },
] as const;

type PageID = typeof navigation[number]["id"];

export default function App() {
  const [user, setUser] = useState<User>();
  const [page, setPage] = useState<PageID>(() => validPage(location.hash.slice(1)) ? location.hash.slice(1) as PageID : "dashboard");
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { api.onAuthLost = () => setUser(undefined); }, []);
  useEffect(() => {
    const syncPage = () => { const next = location.hash.slice(1); if (validPage(next)) setPage(next); };
    window.addEventListener("hashchange", syncPage);
    return () => window.removeEventListener("hashchange", syncPage);
  }, []);
  const navigate = (id: PageID) => { setPage(id); location.hash = id; setMenuOpen(false); };
  if (!user) return <Login onLogin={setUser} />;
  if (user.role !== "admin") return <AccessDenied user={user} onLogout={async () => { await api.logout(); setUser(undefined); }} />;
  if (user.must_change_password) return <ChangePassword user={user} onDone={() => setUser(undefined)} />;
  const content = { dashboard: <DashboardPage />, users: <UsersPage />, objects: <ObjectsPage />, datasets: <DatasetsPage />, evaluations: <EvaluationsPage />, governance: <GovernancePage />, audit: <AuditPage />, monitoring: <MonitoringPage /> };
  return <div className="app-shell">
    <aside className={`sidebar ${menuOpen ? "open" : ""}`}>
      <div className="brand"><span>TMK</span><strong>Admin</strong><button className="mobile-close" onClick={() => setMenuOpen(false)} title="关闭菜单"><X size={20} /></button></div>
      <nav>{navigation.map((item) => { const Icon = item.icon; return <button key={item.id} className={page === item.id ? "active" : ""} onClick={() => navigate(item.id)}><Icon size={18} /><span>{item.label}</span></button>; })}</nav>
      <div className="account"><div><strong>{user.display_name || user.email}</strong><span>{user.email}</span></div><button className="icon-button" title="退出登录" onClick={async () => { await api.logout(); setUser(undefined); }}><LogOut size={18} /></button></div>
    </aside>
    {menuOpen && <button className="sidebar-scrim" onClick={() => setMenuOpen(false)} aria-label="关闭菜单" />}
    <main className="main"><div className="mobile-bar"><button className="icon-button" title="打开菜单" onClick={() => setMenuOpen(true)}><Menu size={20} /></button><strong>TMK Admin</strong></div><div className="content">{content[page]}</div></main>
  </div>;
}

function Login({ onLogin }: { onLogin: (user: User) => void }) {
  const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); setError(""); setBusy(true); const form = new FormData(event.currentTarget); try { onLogin(await api.login(String(form.get("email")), String(form.get("password")))); } catch (reason) { setError((reason as Error).message); setBusy(false); } };
  return <main className="login-screen"><section className="login-panel"><div className="login-brand"><span>TMK</span><h1>TMK Admin</h1><p>同声传译数据与评测管理</p></div>{error && <ErrorNotice message={error} onClose={() => setError("")} />}<form className="form" onSubmit={submit}><label>管理员邮箱<input name="email" type="email" autoComplete="username" required autoFocus /></label><label>密码<input name="password" type="password" autoComplete="current-password" required /></label><button className="button login-button" disabled={busy}>{busy ? "正在登录" : "登录"}</button></form><small>会话凭据仅保存在当前页面内存中</small></section></main>;
}

function ChangePassword({ user, onDone }: { user: User; onDone: () => void }) {
  const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const submit = async (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); const form = new FormData(event.currentTarget); if (form.get("new_password") !== form.get("confirmation")) { setError("两次输入的新密码不一致"); return; } setBusy(true); try { await api.request("/auth/change-password", { method: "POST", body: JSON.stringify({ current_password: form.get("current_password"), new_password: form.get("new_password") }) }); onDone(); } catch (reason) { setError((reason as Error).message); setBusy(false); } };
  return <main className="login-screen"><section className="login-panel"><div className="login-brand"><span>安全设置</span><h1>修改初始密码</h1><p>{user.email}</p></div>{error && <ErrorNotice message={error} />}<form className="form" onSubmit={submit}><label>当前密码<input name="current_password" type="password" required autoFocus /></label><label>新密码<input name="new_password" type="password" minLength={12} maxLength={72} required /></label><label>确认新密码<input name="confirmation" type="password" minLength={12} maxLength={72} required /></label><button className="button login-button" disabled={busy}>修改并重新登录</button></form></section></main>;
}

function AccessDenied({ user, onLogout }: { user: User; onLogout: () => void }) { return <main className="login-screen"><section className="login-panel access-denied"><ShieldCheck size={30} /><h1>需要管理员权限</h1><p>{user.email} 没有管理后台访问权限。</p><button className="button-secondary" onClick={onLogout}>退出登录</button></section></main>; }
function validPage(value: string): value is PageID { return navigation.some((item) => item.id === value); }

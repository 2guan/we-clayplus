// 文件名：main.tsx
// 主要功能：组织控制台路由、认证、实时更新、主题与全局 Mock 场景面板。
// 作者：2guan
import React, { lazy, Suspense, useCallback, useEffect, useState, useRef } from "react";
import { createRoot } from "react-dom/client";
import {
  House,
  ChatCircleDots,
  Users,
  Cpu,
  PuzzlePiece,
  ChartLineUp,
  TerminalWindow,
  GearSix,
  CaretDown,
  SidebarSimple,
  List,
  X,
  SignOut,
  Flask,
  ArrowCounterClockwise,
  CheckCircle,
  WarningCircle,
} from "@phosphor-icons/react";
import { WechatLogin } from "./WechatLogin";
import type { Action, Auth, Group, PageProps, State } from "./types";
import { request } from "./api";
import { Avatar, Badge, Empty, Modal } from "./ui";
import { Contacts, Plugins, Insights, Logs, Settings } from "./Management";
import "./styles.css";
import { AppearancePicker, useAppearance } from "./Appearance";
const Overview = lazy(() => import("./Overview"));
import Chat from "./Chat";
import Models from "./Models";
const pages = [
  ["overview", "总览", House],
  ["wechat", "微信", ChatCircleDots],
  ["contacts", "通讯录", Users],
  ["models", "模型中心", Cpu],
  ["plugins", "插件中心", PuzzlePiece],
  ["insights", "智能洞察", ChartLineUp],
  ["logs", "实时日志", TerminalWindow],
  ["settings", "系统设置", GearSix],
] as const;

// 真实认证与 Mock 外观预览分开；演示提交不创建账号或发起登录请求。
function Login({
  auth,
  brandName,
  onLogin,
  demo = false,
  appearance,
}: {
  auth: Auth;
  brandName: string;
  onLogin: () => void;
  demo?: boolean;
  appearance: React.ReactNode;
}) {
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div className="login-page">
      <div className="auth-appearance">{appearance}</div>
      <div className="login-aside">
        <div className="wordmark">
          <span>W</span>
          {brandName}
        </div>
        <div>
          <span className="eyebrow">连接纷扰，归于从容</span>
          <h1>
            微信与智能，
            <br />
            尽收一处。
          </h1>
          <p>你的微信日常管理工作台。</p>
        </div>
        <small>Dashboard & AI · Designed for everyday conversations</small>
      </div>
      <main className="login-main">
        <form
          className="login-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError("");
            try {
              if (demo) {
                onLogin();
                return;
              }
              await request(
                auth.initialized ? "/api/auth/login" : "/api/auth/setup",
                { name, password },
              );
              onLogin();
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <span className="login-mark">W</span>
          {demo && (
            <div className="hint">
              脱机外观预览，不创建真实账号。
              <button type="button" className="text-link" onClick={onLogin}>
                返回控制台
              </button>
            </div>
          )}
          <h2>{auth.initialized ? "欢迎回来" : "创建你的工作空间"}</h2>
          <p>
            {auth.initialized
              ? "登录控制台，继续今天的工作。"
              : "先设置管理员账号，再连接微信。"}
          </p>
          <label>
            登录名
            <input
              autoComplete="username"
              name="username"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="输入登录名…"
            />
          </label>
          <label>
            {auth.initialized ? "密码" : "管理员密码"}
            <input
              autoComplete={
                auth.initialized ? "current-password" : "new-password"
              }
              name="password"
              type="password"
              required
              minLength={auth.initialized ? undefined : 6}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={auth.initialized ? "输入密码…" : "至少 6 个字符…"}
            />
          </label>
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          <button className="button primary" disabled={busy}>
            {busy
              ? "正在验证…"
              : auth.initialized
                ? "登录控制台"
                : "创建管理员"}
          </button>
          <small className="login-note">
            账号和微信登录信息仅由本机服务管理。
          </small>
        </form>
      </main>
    </div>
  );
}
// 密码表单随弹窗卸载，关闭后不保留输入；真实修改成功后重新获取登录状态。
function PasswordDialog({
  mock,
  onClose,
  onSuccess,
}: {
  mock: boolean;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <Modal
      open
      onClose={onClose}
      title="修改密码"
      description={
        mock ? "脱机演示，不改变真实凭据。" : "修改成功后需要重新登录。"
      }
    >
      <form
        className="form-stack"
        onSubmit={async (event) => {
          event.preventDefault();
          if (busy) return;
          const data = new FormData(event.currentTarget);
          setBusy(true);
          setError("");
          try {
            // 密码更新会使原会话失效，不再通过需要认证的完整状态刷新判断成功。
            await request("/api/action", {
              kind: "password",
              content: String(data.get("currentPassword") || ""),
              password: String(data.get("newPassword") || ""),
            });
            onSuccess();
          } catch (error) {
            setError((error as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          当前密码
          <input
            name="currentPassword"
            type="password"
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          新密码
          <input
            name="newPassword"
            type="password"
            autoComplete="new-password"
            minLength={6}
            required
            placeholder="至少 6 个字符…"
          />
        </label>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="actions">
          <button type="button" className="button" onClick={onClose}>
            取消
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? "正在更新…" : "更新密码"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
// 应用壳统一持有会话、主题和状态快照，页面组件不保存密钥。
function App() {
  const [auth, setAuth] = useState<Auth | null>(null);
  const [state, setState] = useState<State | null>(null);
  const [schema, setSchema] = useState<Group[]>([]);
  const [error, setError] = useState("");
  const [page, setPage] = useState(
    location.hash.slice(1).split("?")[0] || "overview",
  );
  const [mobileNav, setMobileNav] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return localStorage.getItem("weclay.sidebarCollapsed") === "true"; }
    catch { return false; }
  });
  useEffect(() => {
    try { localStorage.setItem("weclay.sidebarCollapsed", String(sidebarCollapsed)); }
    catch { /* 浏览器禁用存储时，仍允许本次会话收缩导航。 */ }
  }, [sidebarCollapsed]);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [toast, setToast] = useState("");
  const [connected, setConnected] = useState(true);
  const appearance = useAppearance();
  const [mockPanel, setMockPanel] = useState(false);
  const [previewAuth, setPreviewAuth] = useState<"login" | "setup" | null>(
    null,
  );
  const loadAuth = useCallback(() => {
    request<Auth>("/api/auth")
      .then(setAuth)
      .catch((e) => setError(e.message));
  }, []);
  // SSE 突发更新共享一次请求，进行中只标记一次补读，避免占满浏览器连接。
  const refreshFlight = useRef<Promise<void> | null>(null);
  const refreshAgain = useRef(false);
  const refresh = useCallback((): Promise<void> => {
    if (refreshFlight.current) {refreshAgain.current=true;return refreshFlight.current;}
    const run = async () => {
      try {
        do {
          refreshAgain.current=false;
          const value=await request<State>("/api/state");
          setState(value);setConnected(true);
        } while(refreshAgain.current);
      } catch(e) {setConnected(false);throw e;}
      finally {refreshFlight.current=null;}
    };
    refreshFlight.current=run();return refreshFlight.current;
  }, []);
  const notify = useCallback((text: string) => setToast(text), []);
  const act = useCallback(
    async (action: Action) => {
      await request("/api/action", action);
      // 人工发送拿到回执后立即解锁；状态刷新失败不能把已发送消息误报成失败。
      if (action.kind === "send" || action.kind === "send-voice") {
        void refresh().catch(() => {}); // refresh 已更新连接状态，等待 SSE 或下次刷新恢复。
      } else {
        await refresh();
      }
    },
    [refresh],
  );
  useEffect(loadAuth, [loadAuth]);
  useEffect(() => {
    const expire = () => {
      setState(null);
      loadAuth();
    };
    window.addEventListener("weclay-session-expired", expire);
    return () => window.removeEventListener("weclay-session-expired", expire);
  }, [loadAuth]);
  useEffect(() => {
    if (!auth?.authenticated) return;
    void Promise.all([
      refresh(),
      request<Group[]>("/api/schema").then(setSchema),
    ]).catch((e) => setError(e.message));
    const events = new EventSource("/api/events");
    // 重连后读取完整快照，补回断线期间错过的事件。
    events.addEventListener("ready", () => {
      void refresh().catch(() => {});
    });
    events.addEventListener("update", () => {
      void refresh().catch(() => {});
    });
    events.onerror = () => setConnected(false);
    return () => events.close();
  }, [auth?.authenticated, refresh]);
  useEffect(() => {
    const onHash = () => {
      setPage(location.hash.slice(1).split("?")[0] || "overview");
      setMobileNav(false);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(""), 4000);
    return () => clearTimeout(t);
  }, [toast]);
  // 已登录和 Mock 预览使用最新设置；未登录时使用认证接口公开的名称。
  const brandName = String(
    state?.settings["brand.name"] || auth?.brandName || "we-clay",
  );
  const robotName = state?.account.name?.trim();
  const robotPhoto = state?.account.photo || state?.account.avatar || "";
  useEffect(() => {
    const icon = document.createElement("link");
    icon.rel = "icon";
    const fallback = "data:image/svg+xml," + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="8" fill="#39775a"/><text x="16" y="23" text-anchor="middle" font-family="sans-serif" font-size="22" fill="white">W</text></svg>');
    icon.href = fallback;
    document.head.appendChild(icon);
    const image = new Image();
    if (/^https?:\/\//.test(robotPhoto)) {
      const url = `/api/wechat-image?url=${encodeURIComponent(robotPhoto)}`;
      image.onload = () => { icon.href = url; };
      image.src = url;
    }
    return () => { image.onload = null; icon.remove(); };
  }, [robotPhoto]);
  useEffect(() => {
    const authPage =
      previewAuth ||
      (auth && !auth.authenticated
        ? auth.initialized
          ? "login"
          : "setup"
        : null);
    const title = authPage
      ? authPage === "login"
        ? "登录"
        : "初始化"
      : pages.find((p) => p[0] === page)?.[1] || "控制台";
    document.title = authPage
      ? `${title} · ${brandName}`
      : `${robotName || brandName} · ${title}`;
  }, [page, brandName, robotName, auth, previewAuth]);
  if (!auth)
    return (
      <div className="startup">
        <span className="login-mark">W</span>
        {error ? (
          <>
            <h2>无法连接控制台</h2>
            <p>{error}</p>
            <button className="button" onClick={loadAuth}>
              重新连接
            </button>
          </>
        ) : (
          <>
            <span className="loader" />
            <p>正在打开工作空间…</p>
          </>
        )}
      </div>
    );
  if (auth.mock && previewAuth)
    return (
      <Login
        appearance={<AppearancePicker {...appearance} />}
        auth={{ ...auth, initialized: previewAuth === "login" }}
        brandName={brandName}
        demo
        onLogin={() => setPreviewAuth(null)}
      />
    );
  if (!auth.authenticated)
    return (
      <Login
        appearance={<AppearancePicker {...appearance} />}
        auth={auth}
        brandName={brandName}
        onLogin={loadAuth}
      />
    );
  if (!state)
    return (
      <div className="startup">
        <span className="loader" />
        <p>{error || "正在读取本机数据…"}</p>
        {error && (
          <button
            className="button"
            onClick={() => refresh().catch((e) => setError(e.message))}
          >
            重试
          </button>
        )}
      </div>
    );
  const props: PageProps = {
    state,
    schema,
    user: auth.user,
    act,
    notify,
    refresh,
  };
  const content =
    page === "overview" ? (
      <Overview {...props} />
    ) : page === "wechat" ? (
      <Chat {...props} />
    ) : page === "contacts" ? (
      <Contacts {...props} />
    ) : page === "models" ? (
      <Models {...props} />
    ) : page === "plugins" ? (
      <Plugins {...props} />
    ) : page === "insights" ? (
      <Insights {...props} />
    ) : page === "logs" ? (
      <Logs {...props} />
    ) : page === "settings" ? (
      <Settings {...props} />
    ) : (
      <Empty
        title="页面不存在"
        action={
          <a href="#overview" className="button">
            回到总览
          </a>
        }
      />
    );
  return (
    <div className={`app-shell ${page === "wechat" ? "chat-shell" : ""}`}>
      <a
        className="skip-link"
        href="#main-content"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        跳到主要内容
      </a>
      {mobileNav && (
        <button
          className="nav-backdrop"
          aria-label="关闭导航"
          onClick={() => setMobileNav(false)}
        />
      )}
      <aside id="sidebar" className={`sidebar ${mobileNav ? "open" : ""} ${sidebarCollapsed ? "collapsed" : ""}`}>
        <a href="#overview" className="brand" aria-label={`${brandName} 总览`} title={brandName}>
          <span className="brand-mark">W</span>
          <div>
            <strong>{brandName}</strong>
            <small>微信与 AI 工作台</small>
          </div>
        </a>
        <WechatLogin className="workspace" online={state.account.status === "online"}>
          <Avatar src={state.account.photo} blank={state.account.status !== "online"} name={state.account.avatar || state.account.name} size="small" />
          <div>
            <strong>{state.account.name}</strong>
            <small>
              <span className="status-dot" />
              {state.mock
                ? "脱机工作空间"
                : state.account.status === "online"
                  ? "微信已连接"
                  : "等待连接"}
            </small>
          </div>
          <CaretDown size={13} />
        </WechatLogin>
        <span className="nav-caption">工作空间</span>
        <nav>
          {pages.map(([id, label, Icon], i) => (
            <React.Fragment key={id}>
              {i === 6 && <span className="nav-caption second">管理</span>}
              <a
                href={`#${id}`}
                aria-label={label}
                title={label}
                aria-current={page === id ? "page" : undefined}
                className={page === id ? "active" : ""}
              >
                <Icon size={20} weight={page === id ? "fill" : "regular"} />
                <span>{label}</span>
                {id === "wechat" &&
                  state.contacts.some((c) => c.unread > 0) && (
                    <i className="nav-unread">
                      {state.contacts.reduce((sum, c) => sum + c.unread, 0)}
                    </i>
                  )}
              </a>
            </React.Fragment>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="user-menu">
            <button
              className="user-info"
              title="修改密码"
              aria-label="修改密码"
              aria-haspopup="dialog"
              onClick={() => setPasswordOpen(true)}
            >
              <Avatar name={auth.user.nickname || "管理员"} size="small" />
              <span>
                <strong>{auth.user.nickname || auth.user.name}</strong>
                <small>
                  {auth.user.role === "admin" ? "系统管理员" : "操作员"}
                </small>
              </span>
            </button>
            <button
              className="icon-button"
              title="退出登录"
              aria-label="退出登录"
              onClick={() => {
                if (state.mock) {
                  notify("Mock 模式使用独立演示身份，无需登录");
                  return;
                }
                request("/api/auth/logout", {})
                  .then(() => {
                    setState(null);
                    loadAuth();
                  })
                  .catch((e) => notify(e.message));
              }}
            >
              <SignOut size={18} />
            </button>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <button
            className="icon-button nav-trigger"
            onClick={() => setMobileNav(true)}
            aria-label="打开导航"
          >
            <List size={23} />
          </button>
          <button
            type="button"
            className="icon-button sidebar-toggle"
            aria-label={sidebarCollapsed ? "展开侧边栏" : "收起侧边栏"}
            title={sidebarCollapsed ? "展开侧边栏" : "收起侧边栏"}
            aria-expanded={!sidebarCollapsed}
            aria-controls="sidebar"
            onClick={() => setSidebarCollapsed((value) => !value)}
          >
            <SidebarSimple size={20} aria-hidden="true" />
          </button>
          {/* 顶栏集中展示服务状态，页面标题保留在内容区。 */}
          <div className="connection-status" aria-live="polite">
            <span className={`status-dot ${connected ? "" : "offline"}`} />
            <span className="connection-label">
              {connected ? "服务连接正常" : "连接中断，正在重连"}
            </span>
            <Badge>{state.mock ? "MOCK" : "LOCAL"}</Badge>
          </div>
          <div className="topbar-right">
            {state.mock && (
              <button
                className="mock-chip"
                onClick={() => setMockPanel(!mockPanel)}
              >
                <Flask size={14} />
                <span>脱机 Mock</span>
                <CaretDown size={10} />
              </button>
            )}
            <AppearancePicker {...appearance} />
          </div>
        </header>
        {mockPanel && (
          <div className="mock-panel">
            <div>
              <Flask size={21} />
              <div>
                <strong>完全脱机的演示环境</strong>
                <p>
                  不会登录微信，不会调用真实模型。数据保存在独立的 Mock 数据库。
                </p>
              </div>
            </div>
            <div className="actions">
              <button
                className="button small"
                onClick={() => setPreviewAuth("login")}
              >
                登录页
              </button>
              <button
                className="button small"
                onClick={() => setPreviewAuth("setup")}
              >
                初始化页
              </button>
              <select
                aria-label="Mock 场景"
                value={state.scenario}
                onChange={(e) =>
                  act({ kind: "mock-scenario", content: e.target.value }).catch(
                    (e) => notify(e.message),
                  )
                }
              >
                <option value="normal">正常运行</option>
                <option value="empty">空数据</option>
                <option value="offline">微信离线</option>
                <option value="error">发送 / 模型失败</option>
              </select>
              <button
                className="button small"
                onClick={() => {
                  if (confirm("重置虚构数据和演示设置？正式数据不会改变。"))
                    void act({ kind: "mock-reset" })
                      .then(() => notify("演示数据已重置"))
                      .catch((e) => notify(e.message));
                }}
              >
                <ArrowCounterClockwise size={15} />
                重置演示
              </button>
              <button
                className="icon-button"
                aria-label="收起 Mock 面板"
                onClick={() => setMockPanel(false)}
              >
                <X size={17} />
              </button>
            </div>
          </div>
        )}
        {!connected && (
          <div className="connection-banner">
            <WarningCircle size={16} />
            连接暂时中断，当前展示最后一次收到的数据。
          </div>
        )}
        <main
          id="main-content"
          tabIndex={-1}
          className={`main-content ${page === "wechat" ? "chat-content" : ""}`}
        >
          <Suspense
            fallback={
              <div className="startup">
                <span className="loader" />
              </div>
            }
          >
            {content}
          </Suspense>
        </main>
        <footer className="app-footer">
          <span>{String(state.settings["brand.footer"])}</span>
          <span>
            {state.mock ? "所有展示数据均为虚构" : "本机运行"}
            <i />
            v0.6.0
          </span>
        </footer>
      </div>
      {passwordOpen && (
        <PasswordDialog
          mock={state.mock}
          onClose={() => setPasswordOpen(false)}
          onSuccess={() => {
            setPasswordOpen(false);
            notify(
              state.mock
                ? "Mock：密码修改流程已验证，不改变真实凭据"
                : "密码已修改，请重新登录",
            );
            if (!state.mock) {
              setState(null);
              loadAuth();
            }
          }}
        />
      )}
      <nav className="mobile-tabs">
        {[
          ["wechat", "微信", ChatCircleDots],
          ["contacts", "通讯录", Users],
          ["insights", "发现", ChartLineUp],
          ["overview", "控制台", House],
        ].map(([id, label, Icon]) => {
          const I = Icon as typeof House;
          return (
            <a
              key={String(id)}
              href={`#${id}`}
              className={page === id ? "active" : ""}
            >
              <I size={23} weight={page === id ? "fill" : "regular"} />
              <span>{String(label)}</span>
            </a>
          );
        })}
      </nav>
      {toast && (
        <div className="toast" role="status">
          <CheckCircle size={18} />
          <span>{toast}</span>
          <button aria-label="关闭提示" onClick={() => setToast("")}>
            <X size={15} />
          </button>
        </div>
      )}
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);

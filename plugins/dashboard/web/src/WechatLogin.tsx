// 文件名：WechatLogin.tsx
// 主要功能：在同一控制台查看 core 状态、启停受管 core 并完成微信扫码。
import { useEffect, useState, type ReactNode } from "react";
import { request } from "./api";
import { Modal } from "./ui";

type CoreStateName = "stopped" | "starting" | "waiting_login" | "online" | "stopping" | "error" | "unmanaged";
type CoreView = { state: CoreStateName; managed: boolean; status: string };
type LoginView = {
  online: boolean;
  status?: string;
  phase?: string;
  can_retry?: boolean;
  qr?: string;
  pin_required?: boolean;
  verification_status?: string;
  device_confirm_available?: boolean;
};
type Action = "start" | "stop" | "retry" | "verify" | "confirm";

const coreLabels: Record<CoreStateName, string> = {
  stopped: "core 已停止",
  starting: "core 启动中",
  waiting_login: "等待微信登录",
  online: "微信已连接",
  stopping: "core 停止中",
  error: "core 需要检查",
  unmanaged: "外部 core",
};

export function WechatLogin({ children, className, online }: {
  children: ReactNode; className: string; online: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [core, setCore] = useState<CoreView>();
  const [view, setView] = useState<LoginView>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<Action | null>(null);
  const [confirmStop, setConfirmStop] = useState(false);
  const [pin, setPin] = useState("");
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    if (!open || busy) return;
    let active = true;
    let timer: number;
    async function poll() {
      try {
        const status = await request<CoreView>("/api/core");
        if (!active) return;
        setCore(status);
        if (status.state === "waiting_login" || status.state === "online" || status.state === "unmanaged") {
          try {
            const login = await request<LoginView>("/api/wechat-login");
            if (!active) return;
            setView(login);
            setError("");
          } catch (e) {
            if (!active) return;
            setView(undefined);
            setError((e as Error).message);
          }
        } else {
          setView(undefined);
          setError("");
        }
      } catch (e) {
        if (!active) return;
        setError((e as Error).message);
      }
      if (active) timer = window.setTimeout(poll, 2000);
    }
    void poll();
    return () => { active = false; window.clearTimeout(timer); };
  }, [open, busy, generation]);

  async function act(action: Action) {
    setBusy(action);
    setError("");
    try {
      if (action === "retry") {
        await request("/api/wechat-login", {});
        setView(undefined);
      } else if (action === "verify") {
        await request("/api/wechat-login/verify-code", { code: pin });
        setPin("");
      } else if (action === "confirm") {
        await request("/api/wechat-login/device-confirm", {});
      } else {
        const status = await request<CoreView>("/api/core", { action });
        setCore(status);
        if (action === "stop") { setView(undefined); setPin(""); }
      }
      setConfirmStop(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
      setGeneration((value) => value + 1);
    }
  }

  const waiting = core?.managed && core.state === "waiting_login";
  const canRetry = !!core?.managed && (view?.can_retry ?? !!view?.status?.startsWith("登录流程已暂停"));
  // core 返回内嵌二维码图片，只接受图片 data URI，不加载外部地址。
  const qr = waiting && view?.phase !== "verification_required" && !canRetry && !busy && !error && /^data:image\/(png|jpe?g|gif|webp);base64,/.test(view?.qr || "")
    ? view?.qr : undefined;
  const canStart = core?.managed && (core.state === "stopped" || core.state === "error");
  const canStop = core?.managed && (core.state === "waiting_login" || core.state === "online" || core.state === "error");
  const verificationRequired = waiting && view?.phase === "verification_required";
  const close = () => { setOpen(false); setPin(""); setConfirmStop(false); };

  return <>
    <button type="button" className={className} onClick={() => setOpen(true)} aria-label={online ? "查看微信连接状态" : "扫码登录微信"}>
      {children}
    </button>
    <Modal open={open} onClose={close} title="微信连接" description="在这里管理 core，并使用手机微信扫码确认登录。">
      <div className="wechat-login-view">
        <div className="wechat-core-status" role="status">
          <strong>{core ? coreLabels[core.state] : "正在读取 core 状态…"}</strong>
          {core?.status && <small>{core.status}</small>}
        </div>
        {qr && <img className="wechat-login-qr" src={qr} alt="微信登录二维码" />}
        {(core?.state === "online" || view?.online) && <p>微信已连接，可以收发消息。</p>}
        {waiting && <p role="status">{view?.status || "等待手机微信扫码并确认…"}</p>}
        {view?.verification_status && <p className="wechat-login-hint">{view.verification_status}</p>}
        {verificationRequired && view?.pin_required && <form className="wechat-pin-form" onSubmit={(event) => { event.preventDefault(); if (pin) void act("verify"); }}>
          <label htmlFor="wechat-pin">微信提示的 PIN 验证码</label>
          <input id="wechat-pin" type="password" inputMode="numeric" autoComplete="one-time-code" maxLength={32} value={pin} onChange={(event) => setPin(event.target.value)} />
          <button type="submit" className="button primary" disabled={!pin || !!busy}>{busy === "verify" ? "正在提交…" : "提交验证码"}</button>
        </form>}
        {verificationRequired && view?.device_confirm_available && !view.pin_required && <button type="button" className="button primary" disabled={!!busy} onClick={() => void act("confirm")}>
          {busy === "confirm" ? "正在确认…" : "确认本次设备登录"}
        </button>}
        {error && <p role="alert" className="form-error">{error}</p>}
        {core && !core.managed && <p className="wechat-login-hint">当前 core 由其他程序管理。此处仅显示在线状态；扫码、验证和启停请在原程序操作。</p>}
        <div className="wechat-login-actions">
          {canStart && <button type="button" className="button primary" disabled={!!busy} onClick={() => void act("start")}>
            {busy === "start" ? "正在启动…" : "启动 core"}
          </button>}
          {waiting && canRetry && <button type="button" className="button primary" disabled={!!busy} onClick={() => void act("retry")}>
            {busy === "retry" ? "正在请求…" : "重新获取二维码"}
          </button>}
          {canStop && !confirmStop && <button type="button" className="button" disabled={!!busy} onClick={() => setConfirmStop(true)}>停止 core</button>}
          {canStop && confirmStop && <div className="wechat-stop-confirm">
            <p>停止后会暂停微信消息接收；设备和登录数据会保留。</p>
            <button type="button" className="button" disabled={!!busy} onClick={() => void act("stop")}>
              {busy === "stop" ? "正在停止…" : "确认停止"}
            </button>
            <button type="button" className="button ghost" disabled={!!busy} onClick={() => setConfirmStop(false)}>取消</button>
          </div>}
          <button type="button" className="button" onClick={close}>{core?.state === "online" || view?.online ? "完成" : "关闭"}</button>
        </div>
      </div>
    </Modal>
  </>;
}

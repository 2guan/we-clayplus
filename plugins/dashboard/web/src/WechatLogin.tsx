// 文件名：WechatLogin.tsx
// 主要功能：提供账号区域扫码入口，显示并轮询 core 的真实登录二维码。
// 作者：2guan
import { useEffect, useState, type ReactNode } from "react";
import { request } from "./api";
import { Modal } from "./ui";

type LoginView = { online: boolean; status: string; qr?: string };

export function WechatLogin({ children, className, online }: {
  children: ReactNode; className: string; online: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<LoginView>();
  const [error, setError] = useState("");
  const [restarting, setRestarting] = useState(false);
  const [generation, setGeneration] = useState(0);
  const paused = !!view?.status?.startsWith("登录流程已暂停");
  useEffect(() => {
    if (!open || restarting) return;
    let active = true;
    let timer: number;
    setView(undefined);
    setError("");
    // 串行轮询避免重叠请求；关闭弹窗后不再更新或发起下一次请求。
    async function poll() {
      try {
        const result = await request<LoginView>("/api/wechat-login");
        if (!active) return;
        setView(result);
        setError("");
        if (result.online) return;
      } catch (e) {
        if (!active) return;
        setError((e as Error).message);
      }
      if (active) timer = window.setTimeout(poll, 2000);
    }
    void poll();
    return () => { active = false; window.clearTimeout(timer); };
  }, [open, restarting, generation]);
  // core 返回内嵌二维码图片，只接受图片 data URI，不加载外部地址。
  const qr = !view?.online && !paused && !restarting && !error && /^data:image\/(png|jpe?g|gif|webp);base64,/.test(view?.qr || "") ? view?.qr : undefined;
  return <>
    <button type="button" className={className} onClick={() => setOpen(true)} aria-label={online ? "查看微信连接状态" : "扫码登录微信"}>
      {children}
    </button>
    <Modal open={open} onClose={() => setOpen(false)} title="连接微信" description="使用手机微信扫码，并按手机提示确认登录。">
      <div className="wechat-login-view">
        {qr && <img className="wechat-login-qr" src={qr} alt="微信登录二维码" />}
        <p role="status">{restarting ? "正在重启 core，重新获取二维码…" : error || (view?.online ? "微信已连接" : view?.status || "正在获取二维码…")}</p>
        {paused && <button className="button primary" disabled={restarting} onClick={async () => {
          setRestarting(true); setError("");
          try {
            await request("/api/wechat-login", {});
            setView(undefined);
            setGeneration((value) => value + 1);
          } catch (e) { setError((e as Error).message); }
          finally { setRestarting(false); }
        }}>{restarting ? "正在重启…" : "重启 core，重新获取二维码"}</button>}
        <button className="button" onClick={() => setOpen(false)}>{view?.online ? "完成" : "关闭"}</button>
      </div>
    </Modal>
  </>;
}

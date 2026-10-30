import { useEffect, useState } from "react";
import { request } from "./api";

// 只读取微信未读计数，不下载每条提醒详情，也不把历史评论冒充未读。
export function useMomentUnread(account: string, online: boolean) {
  const [value, setValue] = useState({ account: "", unread: 0, error: "" });
  useEffect(() => {
    if (!account || !online) return;
    let stopped = false;
    let controller: AbortController | undefined;
    let revision = 0;
    let timer: ReturnType<typeof setTimeout>;
    const check = async () => {
      if (stopped || controller || document.visibilityState === "hidden") return;
      clearTimeout(timer);
      controller = new AbortController();
      const version = revision;
      try {
        const result = await request<{ unread: number }>("/api/moments/unread", undefined, controller.signal);
        if (!Number.isSafeInteger(result.unread) || result.unread < 0) throw new Error("微信未返回有效的朋友圈未读数");
        if (!stopped && version === revision) setValue({ account, unread: result.unread, error: "" });
      } catch (error) {
        if (!stopped) setValue((previous) => ({ account, unread: previous.account === account ? previous.unread : 0, error: (error as Error).message }));
      } finally {
        controller = undefined;
        if (!stopped) timer = setTimeout(check, 60_000);
      }
    };
    void check();
    const read = (event: Event) => {
      const detail = (event as CustomEvent<{account: string; unread: number}>).detail;
      if (detail?.account === account && Number.isSafeInteger(detail.unread) && detail.unread >= 0) {
        revision++;
        setValue({ account, unread: detail.unread, error: "" });
      }
    };
    window.addEventListener("weclay:moment-unread", read);
    document.addEventListener("visibilitychange", check);
    window.addEventListener("focus", check);
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller?.abort();
      window.removeEventListener("weclay:moment-unread", read);
      document.removeEventListener("visibilitychange", check);
      window.removeEventListener("focus", check);
    };
  }, [account, online]);
  return online && value.account === account ? value : { account, unread: 0, error: "" };
}

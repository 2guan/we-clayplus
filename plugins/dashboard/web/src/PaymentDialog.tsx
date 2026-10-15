// 文件名：PaymentDialog.tsx
// 主要功能：展示原消息收款对象与金额，人工确认收红包和转账收款。
// 作者：2guan
import { useEffect, useState } from "react";
import { Modal } from "./ui";
import { request } from "./api";
import type { Message, PageProps } from "./types";
type Preview = {
  status: string;
  sender: string;
  kind: string;
  amount: string;
  mock: boolean;
};
export function PaymentDialog({
  message,
  onClose,
  ...props
}: PageProps & { message: Message; onClose: () => void }) {
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!message) return;
    let active = true;
    request<Preview>(
      `/api/payments?target=${encodeURIComponent(message.target)}&id=${encodeURIComponent(message.id)}`,
    )
      .then((value) => {
        if (active) setPreview(value);
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [message]);
  async function receive() {
    if (!message || busy || preview?.status !== "ready") return;
    setBusy(true);
    setError("");
    try {
      const result = await request<{ status: string }>("/api/payments", {
        target: message.target,
        id: message.id,
        confirmed: true,
      });
      setPreview((p) => p && { ...p, status: result.status });
    } catch (e) {
      setError((e as Error).message);
      // 请求结果未知时停止再次提交，重新打开会从持久化操作记录读取状态。
      setPreview((p) => p && { ...p, status: "unknown" });
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      onClose={onClose}
      title={message.kind === "redpacket" ? "领取红包" : "确认收款"}
    >
      <div className="payment-confirmation">
        {preview && (
          <>
            <p className="payment-recipient">{preview.sender}</p>
            {preview.amount && (
              <strong className="payment-amount">{preview.amount}</strong>
            )}
            {preview.mock && <p className="muted">脱机演示，不操作真实资金</p>}
            <p role="status">
              {preview.status === "confirmed"
                ? "领取回执已确认"
                : preview.status === "unknown" ||
                    preview.status === "processing"
                  ? "该交易已提交，请在微信核对结果，不重复操作。"
                  : "确认后将提交这条消息对应的领取请求。"}
            </p>
          </>
        )}
        {!preview && !error && <p role="status">正在读取原消息…</p>}
        {error && (
          <p role="alert" className="error-text">
            {error}
          </p>
        )}
        <div className="form-actions">
          <button className="button" onClick={onClose}>
            关闭
          </button>
          <button
            className="button primary"
            disabled={
              !preview ||
              preview.status !== "ready" ||
              busy ||
              props.user.role !== "admin"
            }
            onClick={() => void receive()}
          >
            {busy
              ? "正在提交…"
              : message.kind === "redpacket"
                ? "确认领取红包"
                : "确认收款"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

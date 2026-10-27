import { useState } from "react";
import type { Message } from "./types";
import { dateTime } from "./api";

export function ImageDescription({ message, enabled, retry, notify }: {
  message: Message;
  enabled: boolean;
  retry: (message: Message) => Promise<void>;
  notify: (message: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const analysis = message.image_analysis;
  if (message.kind !== "image" || ["sending", "unknown", "failed"].includes(message.status) || message.status.startsWith("revoked")) return null;
  if (!analysis && !enabled) return null;
  const pending = analysis?.status === "pending" || analysis?.status === "running";
  return <div className="image-description">
    {analysis?.status === "completed" ? <details>
      <summary>图片描述{message.image_generated ? " · AI 生成" : ""}</summary>
      <p>{analysis.description}</p>
      <small>{analysis.model} · {dateTime(analysis.updated)}{analysis.cache_hit ? " · 复用缓存" : ""}</small>
    </details> : <div className="image-description-status">
      {pending ? <span role="status">正在识图…</span> : analysis?.status === "failed" ? <details>
        <summary>识图失败</summary><p>{analysis.error || "暂时无法识别图片，请重试。"}</p>
      </details> : null}
      {!pending && enabled && <button type="button" disabled={busy} aria-busy={busy} aria-label={analysis ? "重试图片识别" : "补充图片描述"} onClick={async () => {
        if (busy) return;
        setBusy(true);
        try { await retry(message); notify("识图任务已提交"); }
        catch (error) { notify(error instanceof Error ? error.message : "识图未能提交，请稍后重试"); }
        finally { setBusy(false); }
      }}>{busy ? "提交中…" : analysis ? "重试识图" : "补充识图"}</button>}
    </div>}
  </div>;
}

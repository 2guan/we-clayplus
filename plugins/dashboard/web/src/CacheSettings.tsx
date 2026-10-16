// 主要功能：按类别统计和确认清理本机缓存，明确保护数据与重新获取的影响。
import { useEffect, useState } from "react";
import { ArrowClockwise } from "@phosphor-icons/react";
import { request } from "./api";
import type { PageProps } from "./types";
import { Badge } from "./ui";

type Item = {
  kind: string;
  name: string;
  description: string;
  count: number;
  bytes: number;
};
const descriptions: Record<string, string> = {
  logs: "清理后台事件与 DEBUG 原文，保留聊天归档。",
  avatars: "重新获取头像，浏览器旧副本按其缓存策略过期。",
  media:
    "清理已下载附件与无引用文件，保护音色样本、垫图和已发送素材。过期资源可能无法再次下载。",
  insights:
    "清理本机所有账号的复用缓存，下次分析重新采集；保留报告、追问和资料快照。",
  temporary: "清理未完成下载遗留的文件，保留最近 24 小时的临时文件。",
};
function size(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / 1024 ** 2).toFixed(1)} MB`;
}
export function CacheSettings({ user, refresh, notify }: PageProps) {
  const [days, setDays] = useState("7");
  const [items, setItems] = useState<Item[]>([]);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    if (user.role !== "admin") return;
    request<Item[]>(`/api/cache?days=${days}`)
      .then((v) => {
        if (active) setItems(v);
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [days, revision, user.role]);
  if (user.role !== "admin")
    return (
      <section className="settings-card">
        <div className="section-heading">
          <div>
            <h3>缓存清理</h3>
            <p>仅管理员可以统计和清理缓存。</p>
          </div>
        </div>
      </section>
    );
  const clean = async (item: Item) => {
    const scope = days === "0" ? "全部可清理项目" : `${days} 天前的项目`;
    const prompt =
      item.kind === "avatars"
        ? "刷新头像缓存？各页面将重新获取头像，浏览器旧副本按自身策略过期。"
        : `清理${item.name}（${item.kind === "insights" ? "本机所有账号的复用缓存" : scope}）？\n当前可清理 ${item.count} 项，约 ${size(item.bytes)}。\n${item.description}\n清理后无法撤销。`;
    if (!confirm(prompt)) return;
    setBusy(item.kind);
    setError("");
    try {
      const result = await request<{ count: number; bytes: number }>(
        "/api/cache",
        { kind: item.kind, days: Number(days), confirm: true },
      );
      await refresh();
      setRevision((v) => v + 1);
      notify(
        item.kind === "avatars"
          ? "已刷新头像缓存"
          : `已清理 ${result.count} 项`,
      );
    } catch (e) {
      setError((e as Error).message);
      setRevision((v) => v + 1);
    } finally {
      setBusy("");
    }
  };
  return (
    <section className="settings-card cache-settings" aria-label="缓存清理">
      <div className="section-heading">
        <div>
          <h3>缓存清理</h3>
          <p>按需释放缓存空间，保留聊天归档、记忆、报告与账号资料。</p>
        </div>
        <Badge>手动清理</Badge>
      </div>
      <div className="fields">
        <div className="field-row">
          <label htmlFor="cache-range">
            <strong>清理范围</strong>
            <small>适用于日志、文件与媒体、下载临时文件。</small>
          </label>
          <div className="field-input">
            <select
              id="cache-range"
              aria-label="缓存清理范围"
              value={days}
              disabled={!!busy}
              onChange={(e) => {
                setError("");
                setDays(e.target.value);
              }}
            >
              <option value="7">7 天前</option>
              <option value="30">30 天前</option>
              <option value="90">90 天前</option>
              <option value="0">全部可清理项目</option>
            </select>
          </div>
        </div>
        {items.map((item) => (
          <article
            className="field-row cache-item"
            key={item.kind}
            aria-label={item.name}
          >
            <div className="cache-item-info">
              <strong>{item.name}</strong>
              <small>{descriptions[item.kind] || item.description}</small>
            </div>
            <div className="cache-item-actions">
              <span className="cache-amount">
                {item.kind === "avatars"
                  ? "浏览器管理"
                  : `${item.count} 项 · ${size(item.bytes)}`}
              </span>
              <button
                className="button small"
                aria-label={
                  item.kind === "avatars" ? "刷新头像缓存" : `清理${item.name}`
                }
                disabled={
                  !!busy || (item.kind !== "avatars" && item.count === 0)
                }
                onClick={() => void clean(item)}
              >
                {busy === item.kind ? <span className="loader" /> : null}
                {busy === item.kind
                  ? "清理中…"
                  : item.kind === "avatars"
                    ? "刷新头像"
                    : "清理"}
              </button>
            </div>
          </article>
        ))}
      </div>
      {!items.length && !error && (
        <p className="cache-feedback" role="status">
          正在统计可清理缓存…
        </p>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="form-footer">
        <span>清理前会再次确认，任务运行时暂不可清理。</span>
        <button
          className="button ghost"
          disabled={!!busy}
          onClick={() => {
            setError("");
            setRevision((v) => v + 1);
          }}
        >
          <ArrowClockwise size={15} />
          重新统计
        </button>
      </div>
    </section>
  );
}

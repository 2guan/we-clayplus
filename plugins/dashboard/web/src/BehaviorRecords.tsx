import { useEffect, useState } from "react";
import { request } from "./api";
import { type PageProps } from "./types";

type OperationRecord = {
  id: string;
  task: string;
  origin: string;
  actor: string;
  operation: string;
  target: string;
  status: string;
  created: string;
  preview: Record<string, unknown>;
  reference_ids?: Record<string, string[]>;
  result?: unknown;
  error?: string;
};
const statusLabels: Record<string, string> = {
  pending: "待确认",
  running: "执行中",
  succeeded: "已完成",
  failed: "失败",
  unknown: "结果未知",
  rejected: "已拒绝",
};
const previewLabels: Record<string, string> = {
  mode: "调整方式",
 content: "内容",
  target: "目标",
  target_ref: "目标",
  member_refs: "成员",
  media_ref: "素材",
  media_refs: "图片",
  label_ref: "标签",
  label_refs: "标签",
  save: "保存到通讯录",
  blacklist_refs: "不可见成员",
  with_refs: "提到成员",
  kind: "类型",
  url: "链接",
  person_ref: "指定人员",
  message_ref: "原消息",
  request_ref: "申请",
  moment_ref: "动态",
  comment_ref: "回复评论",
  latitude: "纬度",
  longitude: "经度",
};
export function BehaviorRecords(
  props: PageProps & {
    names: Record<string, string>;
    domains: Record<string, { label: string }>;
  },
) {
  const [rows, setRows] = useState<OperationRecord[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(0);
  const filtered = rows.filter((r) => !status || r.status === status);
  const currentPage = Math.min(
    page,
    Math.max(0, Math.ceil(filtered.length / 20) - 1),
  );
  const visible = filtered.slice(currentPage * 20, currentPage * 20 + 20);
  const load = async () => {
    setRows(await request<OperationRecord[]>("/api/ai-actions"));
  };
  useEffect(() => {
    if (props.user.role !== "admin") return;
    const refresh = () => load().catch((e) => setError(e.message));
    refresh();
    const timer = window.setInterval(refresh, 15000);
    return () => window.clearInterval(timer);
  }, [props.user.role]);
  if (props.user.role !== "admin") return null;
  return (
    <section
      className="panel form-stack behavior-records"
      aria-label="AI操作记录"
    >
      <div className="section-heading">
        <h3>操作记录与确认</h3>
        <button
          className="button ghost"
          type="button"
          onClick={() => load().catch((e) => setError(e.message))}
        >
          刷新记录
        </button>
      </div>
      <p className="muted">
        展示最近100条操作。待确认的操作尚未执行；结果未知时先在微信核对，系统不会自动重放。后台确认只执行当前这一步。
      </p>
      <label>
        筛选操作状态
        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(0);
          }}
        >
          <option value="">全部状态</option>
          {Object.entries(statusLabels).map(([id, name]) => (
            <option value={id} key={id}>
              {name}
            </option>
          ))}
        </select>
      </label>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      {filtered.length === 0 ? (
        <p className="muted">暂无模型操作记录。</p>
      ) : (
        visible.map((row) => (
          <details
            key={row.id}
            className="behavior-record"
            open={row.status === "pending"}
          >
            <summary>
              <span>
                {props.domains[row.operation.split(".")[0]]?.label ||
                  row.operation.split(".")[0]}{" "}
                ·{" "}
                {props.names[row.operation.split(".")[1]] ||
                  row.operation.split(".")[1]}
              </span>
              <span
                className={`badge ${row.status === "unknown" ? "warning" : ""}`}
              >
                {statusLabels[row.status] || row.status}
              </span>
              <time>
                {new Date(row.created).toLocaleString("zh-CN", {
                  timeZone: "Asia/Shanghai",
                })}
              </time>
            </summary>
            <dl className="behavior-preview">
              <dt>发起人</dt>
              <dd>
                {props.state.contacts.find((c) => c.id === row.actor)?.remark ||
                  props.state.contacts.find((c) => c.id === row.actor)?.name ||
                  row.actor ||
                  "本人"}
              </dd>
              {row.target && (
                <div>
                  <dt>实际目标 ID</dt>
                  <dd><code>{row.target}</code></dd>
                </div>
              )}
              {Object.entries(row.preview || {})
                .filter(([, v]) => v !== "" && v !== null)
                .map(([key, value]) => (
                  <div key={key}>
                    <dt>{previewLabels[key] || key}</dt>
                    <dd>
                      {Array.isArray(value)
                        ? value.join("、") || "空"
                        : String(value)}
                    </dd>
                  </div>
                ))}
              {Object.entries(row.reference_ids || {})
                .filter(([, ids]) => ids.length > 0)
                .map(([key, ids]) => {
                  const value = row.preview?.[key];
                  const names = Array.isArray(value)
                    ? value.map(String)
                    : value == null ? [] : [String(value)];
                  const namesMatchIDs = names.length === ids.length;
                  return (
                    <div key={`${key}-ids`}>
                      <dt>{previewLabels[key] || key} ID</dt>
                      <dd>
                        {ids.map((id, index) => (
                          <div key={`${id}-${index}`}>
                            {namesMatchIDs && names[index]
                              ? `${names[index]} · `
                              : ""}
                            <code>{id}</code>
                          </div>
                        ))}
                      </dd>
                    </div>
                  );
                })}
            </dl>
            {row.status === "pending" &&
              ["member_refs", "blacklist_refs", "with_refs", "person_ref"].some(
                (key) =>
                  row.preview?.[key] != null &&
                  (row.reference_ids?.[key]?.length || 0) !==
                    (Array.isArray(row.preview[key])
                      ? row.preview[key].length
                      : 1),
              ) && (
                <p className="hint">
                  此记录未完整保存成员唯一 ID；如有同名成员，请先核对微信再确认。
                </p>
              )}
            {row.result &&
            typeof row.result === "object" &&
            "notice" in row.result &&
            typeof (row.result as { notice?: unknown }).notice === "string" ? (
              <p>{String((row.result as { notice: string }).notice)}</p>
            ) : null}
            {row.error && <p role="alert">{row.error}</p>}
            {row.status === "pending" && (
              <div className="button-row">
                {[true, false].map((approve) => (
                  <button
                    key={String(approve)}
                    className={`button ${approve ? "primary" : "ghost"}`}
                    type="button"
                    disabled={!!busy}
                    onClick={async () => {
                      setBusy(row.id);
                      setError("");
                      try {
                        await request("/api/ai-actions", {
                          id: row.id,
                          approve,
                        });
                        await load();
                        await props.refresh();
                        props.notify(approve ? "操作已处理" : "已拒绝操作");
                      } catch (e) {
                        setError((e as Error).message);
                        await load();
                      } finally {
                        setBusy("");
                      }
                    }}
                  >
                    {busy === row.id
                      ? "处理中…"
                      : approve
                        ? "确认执行"
                        : "拒绝"}
                  </button>
                ))}
              </div>
            )}
          </details>
        ))
      )}
      {filtered.length > 20 && (
        <div className="button-row">
          <button
            className="button ghost"
            type="button"
            disabled={currentPage === 0}
            onClick={() => setPage(currentPage - 1)}
          >
            上一页
          </button>
          <span aria-live="polite">
            {currentPage + 1} / {Math.ceil(filtered.length / 20)}
          </span>
          <button
            className="button ghost"
            type="button"
            disabled={(currentPage + 1) * 20 >= filtered.length}
            onClick={() => setPage(currentPage + 1)}
          >
            下一页
          </button>
        </div>
      )}
    </section>
  );
}

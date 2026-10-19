// 文件名：Memory.tsx
// 主要功能：按会话审核、管理和扫描长期记忆，查看来源与实际回复依据。
// 作者：2guan
import { useCallback, useEffect, useRef, useState } from "react";
import { Brain, Plus, ArrowClockwise } from "@phosphor-icons/react";
import { dateTime, request } from "./api";
import {
  Badge,
  Busy,
  Empty,
  Modal,
  SettingsForm,
  useUnsavedDraft,
} from "./ui";
import type {
  MemoryItem,
  MemoryScan,
  MemoryTrace,
  Message,
  PageProps,
} from "./types";

const statuses: Record<string, string> = {
  pending: "待确认",
  confirmed: "已确认",
  conflicted: "有冲突",
  rejected: "已拒绝",
  expired: "已过期",
  superseded: "已替代",
  deleted: "已删除",
};
type MemoryChange = {
  id: string;
  time: string;
  actor: string;
  action: string;
  before?: MemoryItem;
  after?: MemoryItem;
};
const scopes = { person: "联系人", group: "群共享", bot: "机器人状态" };
function route() {
  return new URLSearchParams(location.hash.split("?")[1]);
}
function setRoute(values: Record<string, string>) {
  const params = route();
  for (const [key, value] of Object.entries(values)) {
    if (value) params.set(key, value);
    else params.delete(key);
  }
  location.hash = `models?${params}`;
}
function blank(target: string, group: boolean): MemoryItem {
  return {
    id: "",
    scope: group ? "group" : "person",
    subject: target,
    target,
    category: "preference",
    key: "",
    content: "",
    status: "pending",
    sensitivity: "low",
    consent: false,
    origin: "manual",
    created_at: "",
    confirmed_at: "",
    confirmed_by: "",
    version: 0,
    sources: [],
  };
}

export default function Memory(props: PageProps & { section: string }) {
  const { state, user, notify } = props;
  const [params, setParams] = useState(route);
  const target = params.get("target") || "";
  const scope = params.get("scope") || "";
  const status = params.get("status") || "";
  const subject = params.get("subject") || "";
  const tab = props.section;
  const requestedPage = Math.max(1, Number(params.get("page")) || 1);
  const contact = state.contacts.find((c) => c.id === target);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [items, setItems] = useState<MemoryItem[]>([]);
  const page = Math.min(
    requestedPage,
    Math.max(1, Math.ceil(items.length / 20)),
  );
  const [scans, setScans] = useState<MemoryScan[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editor, setEditor] = useState<MemoryItem | null>(null);
  const [scanOpen, setScanOpen] = useState(false);
  const [source, setSource] = useState<Message | null>(null);
  const [available, setAvailable] = useState<MemoryItem[] | null>(null);
  const [trace, setTrace] = useState<MemoryTrace | null>(null);
  const [history, setHistory] = useState<MemoryChange[] | null>(null);
  const [task, setTask] = useState(params.get("task") || "");
  const generation = useRef(0);
  const account = state.account.id;
  const currentAccount = useRef(account);
  currentAccount.current = account;
  const showResult = async <T,>(
    promise: Promise<T>,
    apply: (value: T) => void,
  ) => {
    const value = await promise;
    if (currentAccount.current === account) apply(value);
  };
  useEffect(() => {
    const change = () => setParams(route());
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  const load = useCallback(async () => {
    const id = ++generation.current;
    setError("");
    setLoading(true);
    const query = new URLSearchParams({ target, scope, status, subject });
    try {
      const [memories, jobs] = await Promise.all([
        request<{ items: MemoryItem[] }>(`/api/memory?${query}`),
        target
          ? request<{ scans: MemoryScan[] }>(
              `/api/memory/scans?${new URLSearchParams({ target })}`,
            )
          : Promise.resolve({ scans: [] }),
      ]);
      if (id === generation.current) {
        setItems(memories.items || []);
        setSelected(
          (old) =>
            new Set(
              [...old].filter((id) =>
                memories.items?.some(
                  (item) =>
                    item.id === id &&
                    ["pending", "conflicted"].includes(item.status),
                ),
              ),
            ),
        );
        setScans(jobs.scans || []);
      }
    } catch (e) {
      if (id === generation.current) setError((e as Error).message);
    } finally {
      if (id === generation.current) setLoading(false);
    }
  }, [account, target, scope, status, subject]);
  useEffect(() => {
    setItems([]);
    setSelected(new Set());
    setScans([]);
    setAvailable(null);
    setSource(null);
    setTrace(null);
    setHistory(null);
    setEditor(null);
    setScanOpen(false);
    void load();
    return () => {
      generation.current++;
    };
  }, [load]);
  const running = scans.some((s) =>
    ["running", "pending", "queued"].includes(s.status),
  );
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => {
      void load();
    }, 3000);
    return () => clearInterval(timer);
  }, [load, running]);
  const work = async (fn: () => Promise<unknown>, success?: string) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      if (success) notify(success);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const action = (item: MemoryItem, action: string) =>
    work(async () => {
      await request("/api/memory", {
        action,
        id: item.id,
        version: item.version,
      });
      await load();
    }, "记忆已更新");
  const name = (id: string) =>
    state.contacts.find((c) => c.id === id)?.remark ||
    state.contacts.find((c) => c.id === id)?.name ||
    id ||
    "当前账号";
  const memoryGroup = props.schema.find((g) => g.id === "memory");
  if (user.role !== "admin")
    return (
      <Empty
        title="仅管理员可管理 AI 记忆"
        text="请联系管理员查看或调整记忆。"
      />
    );
  return (
    <div className="memory-page">
      <div className="page-toolbar">
          <h2 className="page-title">
            <Brain size={28} /> AI 记忆
          </h2>
        <div className="actions">
          <button
            className="button"
            disabled={busy || loading}
            onClick={() => void load()}
          >
            <ArrowClockwise size={16} />
            刷新
          </button>
          <button
            className="button primary"
            onClick={() => setEditor(blank(target, contact?.kind === "group"))}
          >
            <Plus size={16} />
            新增记忆
          </button>
        </div>
      </div>
      {error && (
        <p className="form-error memory-error" role="alert">
          {error}
          <button className="text-link" onClick={() => void load()}>
            重新读取
          </button>
        </p>
      )}
      {tab === "settings" ? (
        memoryGroup ? (
          <SettingsForm {...props} group={memoryGroup} />
        ) : (
          <Empty title="记忆配置尚未就绪" text="刷新页面以读取服务端配置。" />
        )
      ) : tab === "trace" ? (
        <section className="panel memory-section">
          <h2>本次回复依据</h2>
          <p className="muted">
            查询任务实际使用的消息 ID
            与记忆版本；当前可用记忆不代表已经发送给模型。
          </p>
          <form
            className="memory-trace-form"
            onSubmit={(e) => {
              e.preventDefault();
              setRoute({ task });
              void work(async () =>
                showResult(
                  request<MemoryTrace>(
                    `/api/memory/trace?${new URLSearchParams({ task })}`,
                  ),
                  setTrace,
                ),
              );
            }}
          >
            <label>
              任务 ID
              <input
                name="task"
                required
                value={task}
                onChange={(e) => setTask(e.target.value)}
                placeholder="输入任务 ID…"
              />
            </label>
            <button className="button primary" disabled={busy}>
              {busy ? <Busy /> : "查询依据"}
            </button>
          </form>
          {trace && (
            <dl className="memory-trace">
              <dt>当前消息</dt>
              <dd>{trace.current_id || "未记录"}</dd>
              <dt>近期消息</dt>
              <dd>{trace.history_ids?.join("、") || "无"}</dd>
              <dt>采用的记忆</dt>
              <dd>
                {trace.memory_ids
                  ?.map(
                    (id, i) =>
                      `${id} · v${trace.memory_versions?.[i] ?? "未知"}`,
                  )
                  .join("；") || "无"}
              </dd>
            </dl>
          )}
        </section>
      ) : (
        <>
          <section className="panel memory-section">
            <div className="memory-filters">
              <label>
                会话
                <select
                  aria-label="记忆会话"
                  value={target}
                  onChange={(e) =>
                    setRoute({ target: e.target.value, page: "" })
                  }
                >
                  <option value="">全部会话</option>
                  {state.contacts
                    .filter((c) => ["friend", "group"].includes(c.kind))
                    .map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.remark || c.name}
                      </option>
                    ))}
                </select>
              </label>
              <label>
                范围
                <select
                  aria-label="记忆范围"
                  value={scope}
                  onChange={(e) =>
                    setRoute({ scope: e.target.value, page: "" })
                  }
                >
                  <option value="">全部范围</option>
                  {Object.entries(scopes).map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                状态
                <select
                  aria-label="记忆状态"
                  value={status}
                  onChange={(e) =>
                    setRoute({ status: e.target.value, page: "" })
                  }
                >
                  <option value="">全部状态</option>
                  {Object.entries(statuses).map(([id, label]) => (
                    <option key={id} value={id}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                主体 ID
                <input
                  name="subject"
                  aria-label="记忆主体 ID"
                  defaultValue={subject}
                  key={subject}
                  placeholder="成员微信 ID…"
                  onBlur={(e) => {
                    if (e.target.value.trim() !== subject)
                      setRoute({ subject: e.target.value.trim(), page: "" });
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter")
                      setRoute({
                        subject: e.currentTarget.value.trim(),
                        page: "",
                      });
                  }}
                />
              </label>
            </div>
            <div className="memory-toolbar">
              <span className="muted">
                {target
                  ? `当前会话：${name(target)}`
                  : "选择一个会话后，可扫描聊天记录和预览可用记忆。"}
              </span>
              <div className="actions">
                <button
                  className="button"
                  disabled={!target || busy}
                  onClick={() =>
                    void work(async () =>
                      showResult(
                        request<{ items: MemoryItem[] }>(
                          `/api/memory/preview?${new URLSearchParams({ target, sender: subject || (contact?.kind === "friend" ? target : "") })}`,
                        ),
                        (data) => setAvailable(data.items || []),
                      ),
                    )
                  }
                >
                  当前可用记忆
                </button>
                <button
                  className="button"
                  disabled={
                    !target ||
                    busy ||
                    running ||
                    state.settings["memory.scan_enabled"] !== true
                  }
                  onClick={() => setScanOpen(true)}
                >
                  从聊天记录更新…
                </button>
              </div>
            </div>
            {state.settings["memory.scan_enabled"] !== true && (
              <p className="muted">聊天扫描已关闭，可在“记忆设置”中开启。</p>
            )}
          </section>
          {scans.length > 0 && (
            <section className="panel memory-section" aria-label="扫描进度">
              <h2>扫描进度</h2>
              {scans.slice(0, 5).map((scan) => (
                <div className="memory-scan" key={scan.id}>
                  <div>
                    <strong>
                      {(
                        {
                          running: "正在扫描",
                          pending: "等待扫描",
                          queued: "等待扫描",
                          completed: "扫描完成",
                          failed: "扫描失败",
                          cancelled: "已取消",
                        } as Record<string, string>
                      )[scan.status] || scan.status}
                    </strong>
                    <span className="muted">
                      {scan.processed} / {scan.total} 条 ·{" "}
                      {dateTime(scan.updated_at)}
                    </span>
                    {scan.error && (
                      <p role="alert" className="form-error">
                        {scan.error}
                      </p>
                    )}
                    {scan.warning && <p role="status" className="muted">{scan.warning}</p>}
                    <progress
                      value={scan.processed}
                      max={Math.max(scan.total, 1)}
                      aria-label="已扫描消息"
                    />
                  </div>
                  {["running", "pending", "queued"].includes(scan.status) && (
                    <button
                      className="button small"
                      disabled={busy}
                      onClick={() =>
                        void work(async () => {
                          await request("/api/memory/scans", {
                            action: "cancel",
                            target,
                            id: scan.id,
                          });
                          await load();
                        }, "已请求取消扫描")
                      }
                    >
                      取消扫描
                    </button>
                  )}
                </div>
              ))}
            </section>
          )}
          {loading && !items.length ? (
            <div className="memory-loading" role="status">
              <Busy />
            </div>
          ) : !items.length ? (
            <Empty
              title="没有匹配的记忆"
              text="可以手动新增，或选择会话从聊天记录生成待确认候选。"
            />
          ) : (
            <>
              <div className="memory-bulk">
                <span className="muted">
                  勾选候选后批量应用 · 已选 {selected.size} 条
                </span>
                <div className="actions">
                  {["confirm", "reject"].map((action) => (
                    <button
                      key={action}
                      className="button small"
                      disabled={busy || !selected.size}
                      onClick={() =>
                        void work(async () => {
                          try {
                            for (const item of items.filter((item) =>
                              selected.has(item.id),
                            ))
                              await request("/api/memory", {
                                action,
                                id: item.id,
                                version: item.version,
                              });
                            setSelected(new Set());
                          } finally {
                            await load();
                          }
                        }, "所选候选已更新")
                      }
                    >
                      {action === "confirm" ? "确认所选候选" : "拒绝所选候选"}
                    </button>
                  ))}
                </div>
              </div>
              <div className="memory-list">
                {items.slice((page - 1) * 20, page * 20).map((item) => (
                  <article className="panel memory-item" key={item.id}>
                    <div className="memory-item-heading">
                      <div className="actions">
                        {["pending", "conflicted"].includes(item.status) && (
                          <label className="memory-check">
                            <input
                              type="checkbox"
                              aria-label={`选择候选：${item.content}`}
                              checked={selected.has(item.id)}
                              disabled={
                                busy ||
                                (item.sensitivity === "sensitive" &&
                                  !item.consent)
                              }
                              onChange={(e) =>
                                setSelected((old) => {
                                  const next = new Set(old);
                                  if (e.target.checked) next.add(item.id);
                                  else next.delete(item.id);
                                  return next;
                                })
                              }
                            />
                            选择
                          </label>
                        )}
                        <Badge
                          tone={item.status === "confirmed" ? "green" : ""}
                        >
                          {statuses[item.status] || item.status}
                        </Badge>
                        <Badge>{scopes[item.scope]}</Badge>
                        {item.sensitivity === "sensitive" && (
                          <Badge>
                            敏感 · {item.consent ? "已同意保存" : "待同意"} ·{" "}
                            {item.allow_model ? "可发送给模型" : "不发送给模型"}
                          </Badge>
                        )}
                      </div>
                      <small className="muted">
                        v{item.version} ·{" "}
                        {item.origin === "scan"
                          ? "聊天扫描"
                          : item.origin === "explicit"
                            ? "明确记住"
                            : "后台维护"}
                      </small>
                    </div>
                    <p className="memory-content">{item.content}</p>
                    <dl className="memory-meta">
                      <div>
                        <dt>主体</dt>
                        <dd>{name(item.subject)}</dd>
                      </div>
                      <div>
                        <dt>可用会话</dt>
                        <dd>{name(item.target)}</dd>
                      </div>
                      <div>
                        <dt>确认时间</dt>
                        <dd>
                          {dateTime(item.confirmed_at)}
                          {item.confirmed_by && ` · ${item.confirmed_by}`}
                        </dd>
                      </div>
                      <div>
                        <dt>有效期至</dt>
                        <dd>
                          {item.expires_at &&
                          !item.expires_at.startsWith("0001-")
                            ? dateTime(item.expires_at)
                            : "未设置"}
                        </dd>
                      </div>
                    </dl>
                    <div className="memory-sources">
                      {item.sources?.length ? (
                        item.sources.map((s, i) => (
                          <button
                            className="text-link"
                            key={`${s.message_id}-${i}`}
                            disabled={busy}
                            onClick={() =>
                              void work(async () =>
                                showResult(
                                  request<{ message: Message }>(
                                    `/api/memory/source?${new URLSearchParams({ target: s.target, id: s.message_id })}`,
                                  ),
                                  (data) => setSource(data.message),
                                ),
                              )
                            }
                          >
                            查看来源 {i + 1}
                          </button>
                        ))
                      ) : (
                        <span className="muted">后台维护，无聊天来源</span>
                      )}
                    </div>
                    <div className="memory-item-actions">
                      <button
                        className="button small"
                        disabled={busy || item.status === "deleted"}
                        onClick={() => setEditor(item)}
                      >
                        编辑
                      </button>
                      {["pending", "conflicted"].includes(item.status) && (
                        <>
                          <button
                            className="button small primary"
                            disabled={
                              busy ||
                              (item.sensitivity === "sensitive" &&
                                !item.consent)
                            }
                            onClick={() => void action(item, "confirm")}
                          >
                            确认生效
                          </button>
                          <button
                            className="button small"
                            disabled={busy}
                            onClick={() => void action(item, "reject")}
                          >
                            拒绝
                          </button>
                        </>
                      )}
                      {item.status === "confirmed" && (
                        <button
                          className="button small"
                          disabled={busy}
                          onClick={() => void action(item, "expire")}
                        >
                          标记过期
                        </button>
                      )}
                      <button
                        className="button small"
                        disabled={busy}
                        onClick={() =>
                          void work(async () =>
                            showResult(
                              request<{ changes: MemoryChange[] }>(
                                `/api/memory/history?${new URLSearchParams({ id: item.id })}`,
                              ),
                              (data) => setHistory(data.changes || []),
                            ),
                          )
                        }
                      >
                        变更记录
                      </button>
                      <button
                        className="button small danger"
                        disabled={busy || item.status === "deleted"}
                        onClick={() => {
                          if (
                            confirm(
                              "删除这条长期记忆？删除后不再用于回复，聊天归档将保留。",
                            )
                          )
                            void action(item, "delete");
                        }}
                      >
                        删除
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              <div className="memory-pagination">
                <span className="muted">
                  共 {items.length} 条 · 第 {page} /{" "}
                  {Math.max(1, Math.ceil(items.length / 20))} 页
                </span>
                <div className="actions">
                  <button
                    className="button small"
                    disabled={page <= 1}
                    onClick={() => setRoute({ page: String(page - 1) })}
                  >
                    上一页
                  </button>
                  <button
                    className="button small"
                    disabled={page * 20 >= items.length}
                    onClick={() => setRoute({ page: String(page + 1) })}
                  >
                    下一页
                  </button>
                </div>
              </div>
            </>
          )}
        </>
      )}
      {editor && (
        <MemoryEditor
          key={editor.id || "new"}
          item={editor}
          contacts={state.contacts}
          onClose={() => setEditor(null)}
          onSave={async (item) => {
            const { origin: _origin, ...saved } = item;
            await request("/api/memory", {
              action: "save",
              item: { ...saved, status: "pending" },
            });
            setEditor(null);
            await load();
            notify("记忆已保存，请确认后生效");
          }}
        />
      )}
      {scanOpen && (
        <ScanDialog
          target={target}
          name={name(target)}
          models={state.models}
          onClose={() => setScanOpen(false)}
          onStart={async (values) => {
            await request("/api/memory/scans", {
              action: "start",
              target,
              ...values,
            });
            setScanOpen(false);
            await load();
            notify("已开始生成记忆候选");
          }}
        />
      )}
      <Modal
        open={!!source}
        onClose={() => setSource(null)}
        title="来源消息"
        description="来源仅用于核对事实；撤回或删除的内容不能继续作为有效依据。"
      >
        {source && (
          <div className="memory-source">
            <p>
              {source.sender || "未知成员"} · {source.sender_id || "身份未知"} ·{" "}
              {dateTime(source.time)}
            </p>
            <p>{source.content || "无可展示正文"}</p>
            <a
              className="button"
              href={`#wechat?target=${encodeURIComponent(source.target)}`}
            >
              打开来源会话
            </a>
          </div>
        )}
      </Modal>
      <Modal
        open={available !== null}
        onClose={() => setAvailable(null)}
        title="当前可用记忆"
        description="这是当前范围内可用的条目；每轮仍会按主题筛选，实际采用内容请查询“本次回复依据”。"
      >
        {available?.length ? (
          <ul className="memory-preview">
            {available.map((item) => (
              <li key={item.id}>
                {item.content}
                <small>
                  {scopes[item.scope]} · v{item.version}
                </small>
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted">
            当前没有可用记忆。群聊个人记忆需填写当前发言人 ID 才能预览。
          </p>
        )}
      </Modal>
      <Modal
        open={history !== null}
        onClose={() => setHistory(null)}
        title="变更记录"
        description="记录记忆的创建、审核与状态变更。"
      >
        {history?.length ? (
          <ol className="memory-preview memory-history">
            {history.slice(0, 20).map((change, index) => (
              <li key={change.id || index}>
                <strong>
                  {(
                    {
                      save: "保存",
                      create: "创建",
                      confirm: "确认",
                      reject: "拒绝",
                      expire: "标记过期",
                      delete: "删除",
                    } as Record<string, string>
                  )[change.action] || change.action}
                </strong>
                <small>
                  {change.actor || "系统"} · {dateTime(change.time)} · v
                  {change.after?.version || "未知"}
                </small>
                {change.before?.content &&
                  change.before.content !== change.after?.content && (
                    <p>修改前：{change.before.content}</p>
                  )}
                {change.after?.content && <p>{change.after.content}</p>}
              </li>
            ))}
          </ol>
        ) : (
          <p className="muted">暂无变更记录。</p>
        )}
      </Modal>
    </div>
  );
}

function MemoryEditor({
  item,
  contacts,
  onClose,
  onSave,
}: {
  item: MemoryItem;
  contacts: PageProps["state"]["contacts"];
  onClose: () => void;
  onSave: (item: MemoryItem) => Promise<void>;
}) {
  const [draft, setDraft] = useState(item);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dirty = JSON.stringify(draft) !== JSON.stringify(item);
  useUnsavedDraft(dirty);
  const close = () => {
    if (!busy && (!dirty || confirm("有尚未保存的修改，确定关闭？"))) onClose();
  };
  const update = (values: Partial<MemoryItem>) =>
    setDraft({ ...draft, ...values });
  const expiryDate = draft.expires_at ? new Date(draft.expires_at) : null;
  const expiry =
    expiryDate &&
    Number.isFinite(expiryDate.getTime()) &&
    expiryDate.getFullYear() > 1
      ? `${expiryDate.getFullYear()}-${String(expiryDate.getMonth() + 1).padStart(2, "0")}-${String(expiryDate.getDate()).padStart(2, "0")}`
      : "";
  return (
    <Modal
      open
      title={item.id ? "编辑记忆" : "新增记忆"}
      onClose={close}
      description="后台维护的事实也需要确认后生效；联系人事实必须明确主体和可用会话。"
    >
      <form
        className="form-stack"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            await onSave({
              ...draft,
              content: draft.content.trim(),
              subject: draft.subject.trim(),
              key: draft.key.trim(),
            });
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="form-two">
          <label>
            记忆范围
            <select
              value={draft.scope}
              onChange={(e) =>
                update({
                  scope: e.target.value as MemoryItem["scope"],
                  ...(e.target.value === "group"
                    ? { subject: draft.target }
                    : e.target.value === "bot"
                      ? { sources: [] }
                      : {}),
                })
              }
            >
              {Object.entries(scopes).map(([id, label]) => (
                <option key={id} value={id}>
                  {label}
                </option>
              ))}
            </select>
          </label>
          <label>
            可用会话
            <select
              required={draft.scope !== "bot"}
              value={draft.target}
              onChange={(e) =>
                update({
                  target: e.target.value,
                  subject:
                    draft.scope === "group"
                      ? e.target.value
                      : draft.subject || e.target.value,
                })
              }
            >
              <option value="">
                {draft.scope === "bot" ? "当前账号" : "请选择会话…"}
              </option>
              {contacts
                .filter((c) => ["friend", "group"].includes(c.kind))
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.remark || c.name}
                  </option>
                ))}
            </select>
          </label>
        </div>
        <label>
          主体 ID
          <input
            name="subject"
            required={draft.scope === "person"}
            value={draft.subject}
            onChange={(e) => update({ subject: e.target.value })}
            placeholder="事实所属成员的微信 ID…"
          />
        </label>
        <div className="form-two">
          <label>
            类别
            <input
              name="category"
              required
              value={draft.category}
              onChange={(e) => update({ category: e.target.value })}
            />
          </label>
          <label>
            事实标识
            <input
              name="key"
              required
              value={draft.key}
              onChange={(e) => update({ key: e.target.value })}
              placeholder="例如 preferred_name…"
            />
          </label>
        </div>
        <label>
          记忆内容
          <textarea
            name="content"
            required
            rows={4}
            maxLength={4000}
            value={draft.content}
            onChange={(e) => update({ content: e.target.value })}
            placeholder="填写一条可核对的事实…"
          />
        </label>
        <div className="form-two">
          <label>
            敏感级别
            <select
              value={draft.sensitivity}
              onChange={(e) =>
                update({
                  sensitivity: e.target.value as MemoryItem["sensitivity"],
                  consent: false,
                  allow_model: false,
                })
              }
            >
              <option value="low">低敏</option>
              <option value="sensitive">敏感</option>
            </select>
          </label>
          <label>
            有效期至
            <input
              type="date"
              name="expires_at"
              value={expiry}
              onChange={(e) =>
                update({
                  expires_at: e.target.value
                    ? new Date(`${e.target.value}T23:59:59`).toISOString()
                    : undefined,
                })
              }
            />
            <small className="muted">
              留空不设有效期；日期按浏览器本地时区的所选日结束。
            </small>
          </label>
        </div>
        {draft.sensitivity === "sensitive" && (
          <>
            <label className="memory-check">
              <input
                type="checkbox"
                checked={draft.consent}
                onChange={(e) => update({ consent: e.target.checked })}
              />
              已获得本人明确同意保存此敏感信息
            </label>
            <label className="memory-check">
              <input
                type="checkbox"
                checked={draft.allow_model === true}
                onChange={(e) => update({ allow_model: e.target.checked })}
              />
              已获得本人明确同意发送给配置的模型
            </label>
          </>
        )}
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="actions">
          <button
            className="button"
            type="button"
            disabled={busy}
            onClick={close}
          >
            取消
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? <Busy /> : "保存记忆"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function ScanDialog({
  target,
  name,
  models,
  onClose,
  onStart,
}: {
  target: string;
  name: string;
  models: PageProps["state"]["models"];
  onClose: () => void;
  onStart: (values: Record<string, string>) => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [model, setModel] = useState("");
  return (
    <Modal
      open
      title="从聊天记录更新"
      description={`仅扫描“${name}”会话。所选聊天内容将发送至配置的模型，结果先进入候选，确认后才生效。`}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        className="form-stack"
        onSubmit={async (e) => {
          e.preventDefault();
          const data = new FormData(e.currentTarget);
          setBusy(true);
          setError("");
          try {
            const values: Record<string, string> = { model };
            for (const key of ["from", "to"]) {
              const value = String(data.get(key) || "");
              if (value) values[key] = new Date(value).toISOString();
            }
            await onStart(values);
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <p className="muted">
          会话 ID：{target}
          。留空时间则从上次检查点继续；首次扫描处理该会话现有记录。
        </p>
        <div className="form-two">
          <label>
            开始时间
            <input name="from" type="datetime-local" />
          </label>
          <label>
            截止时间
            <input name="to" type="datetime-local" />
          </label>
        </div>
        <label>
          抽取模型
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="">使用已配置的默认对话模型</option>
            {models
              .filter((m) => m.enabled && m.capabilities.includes("chat"))
              .map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} · {m.provider}
                </option>
              ))}
          </select>
        </label>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="actions">
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? <Busy /> : "发送至模型并生成候选"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

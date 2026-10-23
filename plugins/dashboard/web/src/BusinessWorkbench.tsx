// 文件名：BusinessWorkbench.tsx
// 主要功能：提供业务插件预览、确认发送、计划编排、素材管理和执行记录，复用控制台主题与表单样式。
// 作者：2guan
import { useEffect, useId, useRef, useState } from "react";
import {
  ArrowClockwise,
  Play,
  Plus,
  Trash,
  PaperPlaneTilt,
} from "@phosphor-icons/react";
import { Modal, Tabs, Empty, Toggle } from "./ui";
import { dateTime, readFile, request } from "./api";
import type { Contact, PageProps, Plugin } from "./types";

const targetName = (contact: Contact) =>
  contact.remark || contact.name || contact.id;
const targetOption = (contact: Contact) =>
  `${targetName(contact)} · ${contact.id}`;

function TargetInput({
  label,
  contacts,
  value,
  onChange,
}: {
  label: string;
  contacts: Contact[];
  value: string;
  onChange: (value: string) => void;
}) {
  const descriptionID = useId();
  const selected = contacts.find((contact) => contact.id === value);
  return (
    <label>
      {label}
      <input
        aria-label={label}
        aria-describedby={selected ? descriptionID : undefined}
        list="business-targets"
        value={selected ? targetName(selected) : value}
        onChange={(event) => {
          const text = event.target.value;
          const contact = contacts.find((item) => targetOption(item) === text);
          onChange(contact?.id || text);
        }}
        placeholder="搜索群名、昵称或输入会话 ID…"
      />
      {selected && <small id={descriptionID}>ID：{selected.id}</small>}
    </label>
  );
}

export const businessIDs = [
  "cron",
  "statistics",
  "wordcloud",
  "auto_accept",
  "welcome",
  "digest",
  "news",
  "video_parser",
  "music",
  "meme",
  "what_to_eat",
  "moyu_daily",
];
type Input = {
  plugin: string;
  target: string;
  query: string;
  days: number;
  start: string;
  end: string;
  member: string;
  mode: string;
  media: string;
  asset: string;
  index: number;
};
type Result = {
  images?: { media: string; mime: string; caption: string }[];
  music_source?: string;
  preview?: string;
  text: string;
  mock: boolean;
  media?: string;
  mime?: string;
  task?: string;
  count?: number;
  music?: {
    title: string;
    artist?: string;
    cover?: string;
    lyric?: string;
    audio_url: string;
  };
  items?: {
    title: string;
    url: string;
    artist?: string;
    media_url?: string;
    cover?: string;
    lyric?: string;
  }[];
  ranking?: { name: string; count: number }[];
  words?: { name: string; count: number }[];
};
type Job = {
  id: string;
  name: string;
  enabled: boolean;
  kind: string;
  at: string;
  expression: string;
  timezone: string;
  request: Input;
  next: string;
  last: string;
  revision: number;
};
type Asset = { id: string; name: string; media: string };
type Data = {
  jobs: Job[];
  assets: Asset[];
  runs: {
    id: string;
    plugin: string;
    target: string;
    time: string;
    status: string;
    detail: string;
  }[];
};
const statuses: Record<string, string> = {
  running: "执行中",
  accepted: "已接收",
  previewed: "已预览",
  sent: "已发送",
  completed: "已完成",
  skipped: "已跳过",
  failed: "失败",
  unknown: "结果未确认",
  interrupted: "已中断",
};
const emptyInput = (plugin: string, target: string): Input => ({
  plugin,
  target,
  query: "",
  days: plugin === "digest" ? 1 : 7,
  start: "",
  end: "",
  member: "",
  mode: "",
  media: "",
  asset: "",
  index: 0,
});

export function BusinessWorkbench({
  plugin,
  onClose,
  ...props
}: PageProps & { plugin: Plugin; onClose: () => void }) {
  const { state, notify } = props;
  const [tab, setTab] = useState(plugin.id === "cron" ? "jobs" : "preview");
  const [data, setData] = useState<Data>({ jobs: [], assets: [], runs: [] });
  const [input, setInput] = useState(() =>
    emptyInput(plugin.id, state.contacts[0]?.id || ""),
  );
  const [result, setResult] = useState<Result | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const [dirty, setDirty] = useState(false);
  const [assetName, setAssetName] = useState("");
  const [assetMedia, setAssetMedia] = useState("");
  const abort = useRef<AbortController | null>(null);
  const closed = useRef(false);
  async function reload() {
    try {
      const next = await request<Data>("/api/business");
      if (!closed.current) setData(next);
    } catch (e) {
      if (!closed.current) setError((e as Error).message);
    }
  }
  useEffect(() => {
    closed.current = false;
    void reload();
    return () => {
      closed.current = true;
      abort.current?.abort();
    };
  }, []);
  useEffect(() => {
    const guard = (e: BeforeUnloadEvent) => {
      if (dirty || busy || assetMedia) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty, busy, assetMedia]);
  const close = () => {
    if ((dirty || assetMedia) && !confirm("还有未保存的任务或素材，确定离开？"))
      return;
    abort.current?.abort();
    onClose();
  };
  const change = (patch: Partial<Input>) => {
    setInput((v) => ({ ...v, ...patch }));
    setResult(null);
    setSent(false);
  };
  async function perform(body: unknown): Promise<unknown> {
    setBusy(true);
    setError("");
    try {
      const v = await request("/api/business", body);
      await reload();
      return v;
    } catch (e) {
      setError((e as Error).message);
      throw e;
    } finally {
      setBusy(false);
    }
  }
  async function preview(patch: Partial<Input> = {}) {
    setBusy(true);
    setError("");
    setSent(false);
    setResult(null);
    abort.current = new AbortController();
    try {
      const response = await fetch("/api/business", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        signal: abort.current.signal,
        body: JSON.stringify({
          operation: "run",
          request: { ...input, ...patch },
        }),
      });
      const value = await response.json();
      if (!response.ok) throw new Error(value.error || "执行失败");
      if (!closed.current) setResult(value);
      await reload();
    } catch (e) {
      if (!closed.current)
        setError(
          (e as Error).name === "AbortError"
            ? "已取消预览"
            : (e as Error).message,
        );
    } finally {
      if (!closed.current) setBusy(false);
    }
  }
  function newJob() {
    if (dirty && !confirm("放弃未保存的任务？")) return;
    setJob({
      id: "",
      name: "",
      enabled: true,
      kind: "cron",
      at: "",
      expression: "0 9 * * *",
      timezone: String(state.settings["cron.timezone"] || "Asia/Shanghai"),
      request: { ...emptyInput("text", state.contacts[0]?.id || "") },
      next: "",
      last: "",
      revision: 0,
    });
    setDirty(false);
  }
  const editJob = (patch: Partial<Job>) => {
    setJob((v) => (v ? { ...v, ...patch } : null));
    setDirty(true);
  };
  const inputJob = (patch: Partial<Input>) => {
    if (job) editJob({ request: { ...job.request, ...patch } });
  };
  const archive = ["statistics", "wordcloud", "digest"].includes(plugin.id);
  const canSend = !["auto_accept", "cron"].includes(plugin.id);
  const targetOptions = (
    <datalist id="business-targets">
      {state.contacts.map((c) => (
        <option key={c.id} value={targetOption(c)} />
      ))}
    </datalist>
  );
  return (
    <Modal
      open
      onClose={close}
      title={`${plugin.name} · 工作台`}
      description={
        state.mock
          ? "脱机 Mock · 不连接外部服务或真实微信"
          : "预览不会发送消息，确认结果后再发送。"
      }
      wide
    >
      <div className="business-workbench">
        {targetOptions}
        <Tabs
          value={tab}
          onChange={setTab}
          items={
            plugin.id === "cron"
              ? [
                  ["jobs", "计划任务"],
                  ["records", "执行记录"],
                ]
              : [
                  ["preview", "功能预览"],
                  ...(plugin.id === "meme"
                    ? ([["assets", "素材库"]] as [string, string][])
                    : []),
                  ["records", "执行记录"],
                ]
          }
        />
        {error && (
          <p role="alert" className="form-error">
            {error}
          </p>
        )}
        {tab === "preview" && (
          <div className="business-columns">
            <form
              className="business-form"
              onSubmit={(e) => {
                e.preventDefault();
                void preview();
              }}
            >
              <TargetInput
                label="目标会话"
                contacts={state.contacts}
                value={input.target}
                onChange={(target) => change({ target })}
              />
              {archive && (
                <>
                  <div className="business-pair">
                    <label>
                      最近天数
                      <input
                        type="number"
                        min={1}
                        max={365}
                        value={input.days}
                        onChange={(e) =>
                          change({
                            days: Number(e.target.value),
                            start: "",
                            end: "",
                          })
                        }
                      />
                    </label>
                    <label>
                      成员筛选
                      <input
                        value={input.member}
                        onChange={(e) => change({ member: e.target.value })}
                        placeholder="成员 ID 或名称…"
                      />
                    </label>
                  </div>
                  <div className="business-pair">
                    <label>
                      开始日期
                      <input
                        type="date"
                        value={input.start}
                        onChange={(e) => change({ start: e.target.value })}
                      />
                    </label>
                    <label>
                      结束日期
                      <input
                        type="date"
                        value={input.end}
                        onChange={(e) => change({ end: e.target.value })}
                      />
                    </label>
                  </div>
                  <p className="hint">
                    留空日期时使用最近天数，只分析本地已归档记录。
                  </p>
                </>
              )}
              {plugin.id === "meme" && (
                <label>
                  操作
                  <select
                    aria-label="操作"
                    value={input.mode}
                    onChange={(e) =>
                      change({ mode: e.target.value, asset: "" })
                    }
                  >
                    <option value="">搜索表情</option>
                    <option value="create">制作文字表情</option>
                    <option value="asset">使用自定义素材</option>
                  </select>
                </label>
              )}
              {plugin.id === "news" && (
                <label>
                  新闻类型
                  <select
                    aria-label="新闻类型"
                    value={input.mode}
                    onChange={(e) =>
                      change({ mode: e.target.value, query: "" })
                    }
                  >
                    <option value="">跟随插件配置</option>
                    <option value="today">今日新闻 · 当天单图</option>
                    <option value="diagram">今日图卦 · 昨日双图</option>
                    <option value="rss">RSS 新闻 · 标题与链接</option>
                  </select>
                  <small>
                    图片新闻可点击查看原图；图卦使用昨日内容，两张按顺序发送。
                  </small>
                </label>
              )}
              {plugin.id === "music" && (
                <label>
                  分享形式
                  <select
                    aria-label="分享形式"
                    value={input.mode}
                    onChange={(e) => change({ mode: e.target.value })}
                  >
                    <option value="">跟随插件配置</option>
                    <option value="card">音乐卡片</option>
                    <option value="link">文字链接</option>
                    <option value="media">试听 / 音频文件</option>
                  </select>
                </label>
              )}
              {input.mode === "asset" ? (
                <label>
                  选择素材
                  <select
                    value={input.asset}
                    onChange={(e) => change({ asset: e.target.value })}
                  >
                    <option value="">请选择…</option>
                    {data.assets.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                </label>
              ) : (
                !["statistics", "wordcloud", "moyu_daily"].includes(
                  plugin.id,
                ) &&
                (plugin.id !== "news" ||
                  (input.mode ||
                    String(state.settings["news.default_mode"] || "today")) ===
                    "rss") && (
                  <label>
                    {(
                      {
                        welcome: "新成员名称",
                        auto_accept: "好友申请附言",
                        digest: "补充摘要要求",
                        video_parser: "视频分享链接",
                        what_to_eat: "本次排除项",
                      } as Record<string, string>
                    )[plugin.id] || "关键词 / 内容"}
                    <textarea
                      rows={4}
                      value={input.query}
                      onChange={(e) => change({ query: e.target.value })}
                      placeholder="请输入…"
                    />
                  </label>
                )
              )}
              {plugin.id === "meme" && input.mode === "create" && (
                <label>
                  背景图片（可选）
                  <input
                    type="file"
                    accept="image/png,image/jpeg"
                    onChange={async (e) => {
                      const f = e.target.files?.[0];
                      if (f)
                        try {
                          change({ media: await readFile(f) });
                        } catch (err) {
                          setError((err as Error).message);
                        }
                    }}
                  />
                </label>
              )}
              {plugin.id === "auto_accept" && (
                <label>
                  来源场景
                  <input
                    type="number"
                    min={0}
                    max={1000}
                    value={input.index}
                    onChange={(e) => change({ index: Number(e.target.value) })}
                  />
                </label>
              )}
              {plugin.id === "auto_accept" && (
                <p className="hint">
                  此处仅检查名单和附言。实际验证只接受系统好友申请事件；群邀请暂不支持。
                </p>
              )}
              <div className="actions">
                <button className="button primary" disabled={busy}>
                  <Play size={16} />
                  {busy ? "正在处理…" : "生成预览"}
                </button>
                {state.mock &&
                  ["auto_accept", "welcome"].includes(plugin.id) && (
                    <button
                      type="button"
                      className="button"
                      disabled={busy}
                      onClick={() =>
                        void perform({
                          operation: "simulate-event",
                          request: input,
                        })
                          .then(() => {
                            notify(
                              "模拟事件已提交，请在执行记录中查看处理结果",
                            );
                            setTab("records");
                          })
                          .catch(() => {})
                      }
                    >
                      模拟系统事件
                    </button>
                  )}
                {busy && (
                  <button
                    type="button"
                    className="button"
                    onClick={() => abort.current?.abort()}
                  >
                    取消
                  </button>
                )}
              </div>
            </form>
            <section className="business-result" aria-live="polite">
              {!result ? (
                <Empty
                  title={busy ? "正在准备结果" : "结果预览"}
                  text={
                    busy
                      ? "摘要会使用默认模型流水线，请稍候。"
                      : "选择输入项后生成预览，确认内容后再发送。"
                  }
                />
              ) : (
                <>
                  <div className="business-heading">
                    <h3>{result.mock ? "脱机预览" : "预览结果"}</h3>
                    {canSend && (
                      <button
                        type="button"
                        className="button primary"
                        disabled={busy || sent}
                        onClick={() => {
                          if (
                            !confirm(
                              `将预览结果发送至 ${state.contacts.find((c) => c.id === input.target)?.name || input.target}？`,
                            )
                          )
                            return;
                          void perform({
                            operation: "send",
                            id: result.preview,
                          })
                            .then(() => {
                              setSent(true);
                              notify(
                                state.mock ? "已模拟发送" : "发送请求已完成",
                              );
                            })
                            .catch(() => {});
                        }}
                      >
                        <PaperPlaneTilt size={16} />
                        {sent ? "已发送" : "发送预览"}
                      </button>
                    )}
                  </div>
                  {result.music_source && (
                    <p className="hint">音源：{result.music_source}</p>
                  )}
                  {result.images && (
                    <div className="business-image-gallery">
                      {result.images.map((x) => (
                        <figure key={x.media}>
                          <a
                            href={x.media}
                            target="_blank"
                            rel="noreferrer"
                            aria-label={`查看原图：${x.caption}`}
                          >
                            <img src={x.media} alt={x.caption} loading="lazy" />
                          </a>
                          <figcaption>{x.caption}</figcaption>
                        </figure>
                      ))}
                    </div>
                  )}
                  {result.media &&
                    (result.mime?.startsWith("video/") ? (
                      <video controls src={result.media} />
                    ) : result.mime?.startsWith("audio/") ? (
                      <audio controls src={result.media} />
                    ) : (
                      <img src={result.media} alt={`${plugin.name}生成结果`} />
                    ))}
                  <pre>{result.text}</pre>
                  {result.music && (
                    <div className="wechat-music">
                      <div className="wechat-music-heading">
                        {result.music.cover && (
                          <img
                            src={`/api/music-media?preview=1&id=${encodeURIComponent(result.preview || "")}&part=cover`}
                            alt="专辑封面"
                            referrerPolicy="no-referrer"
                          />
                        )}
                        <div>
                          <strong>{result.music.title}</strong>
                          <p>{result.music.artist}</p>
                        </div>
                      </div>
                      <audio
                        controls
                        preload="none"
                        src={
                          state.mock
                            ? "/sample-audio.wav"
                            : `/api/music-media?preview=1&id=${encodeURIComponent(result.preview || "")}&part=audio`
                        }
                        aria-label="歌曲试听"
                      />
                      {result.music.lyric && (
                        <details>
                          <summary>查看歌词</summary>
                          <pre>{result.music.lyric}</pre>
                        </details>
                      )}
                    </div>
                  )}
                  {result.items?.map((item, i) => (
                    <div className="business-item" key={`${item.url}-${i}`}>
                      <div>
                        <strong>{item.title}</strong>
                        {item.artist && <small>{item.artist}</small>}
                        {item.url && (
                          <a href={item.url} target="_blank" rel="noreferrer">
                            打开来源
                          </a>
                        )}
                      </div>
                      {item.media_url && (
                        <button
                          type="button"
                          className="button"
                          disabled={busy}
                          onClick={() =>
                            void preview({ mode: "media", index: i })
                          }
                        >
                          预览媒体
                        </button>
                      )}
                      {plugin.id === "music" && (
                        <div className="actions">
                          <button
                            type="button"
                            className="button"
                            disabled={busy}
                            onClick={() =>
                              void preview({ mode: "card", index: i })
                            }
                          >
                            预览卡片
                          </button>
                          <button
                            type="button"
                            className="button"
                            disabled={busy}
                            onClick={() =>
                              void preview({ mode: "link", index: i })
                            }
                          >
                            文字分享
                          </button>
                        </div>
                      )}
                    </div>
                  ))}
                  {result.words && (
                    <details>
                      <summary>查看词频（{result.words.length} 项）</summary>
                      <div className="business-words">
                        {result.words.map((w) => (
                          <span key={w.name}>
                            {w.name} <b>{w.count}</b>
                          </span>
                        ))}
                      </div>
                    </details>
                  )}
                </>
              )}
            </section>
          </div>
        )}
        {tab === "jobs" && (
          <>
            <div className="business-heading">
              <p className="hint">
                单次或周期执行；错过超过一分钟的计划不补发，未知结果不重试。
              </p>
              <button type="button" className="button" onClick={newJob}>
                <Plus size={16} />
                新建任务
              </button>
            </div>
            <div className="business-columns">
              <div className="business-job-list">
                {data.jobs.length === 0 && (
                  <Empty
                    title="还没有计划任务"
                    text="创建第一项消息、摘要或内容推送计划。"
                  />
                )}
                {data.jobs.map((j) => (
                  <article className="business-job" key={j.id}>
                    <div className="business-heading">
                      <strong>{j.name}</strong>
                      <span className="muted">
                        {j.enabled ? "已启用" : "已停用"}
                      </span>
                    </div>
                    <p>
                      {j.kind === "cron"
                        ? `${j.expression} · ${j.timezone}`
                        : dateTime(j.at)}
                    </p>
                    <small>
                      下次：{j.enabled ? dateTime(j.next) : "—"} · 上次：
                      {dateTime(j.last)}
                    </small>
                    <div className="actions">
                      <button
                        className="button"
                        onClick={() => {
                          if (dirty && !confirm("放弃未保存的任务？")) return;
                          setJob(structuredClone(j));
                          setDirty(false);
                        }}
                      >
                        编辑
                      </button>
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() => {
                          if (confirm("立即执行并发送此任务？"))
                            void perform({ operation: "job-run", id: j.id })
                              .then(() => notify("任务已执行"))
                              .catch(() => {});
                        }}
                      >
                        立即执行
                      </button>
                      <button
                        className="icon-button"
                        aria-label={`删除任务 ${j.name}`}
                        disabled={busy}
                        onClick={() => {
                          if (confirm(`删除任务「${j.name}」？`))
                            void perform({
                              operation: "job-delete",
                              id: j.id,
                              revision: j.revision,
                            })
                              .then(() => {
                                if (job?.id === j.id) {
                                  setJob(null);
                                  setDirty(false);
                                }
                              })
                              .catch(() => {});
                        }}
                      >
                        <Trash size={16} />
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              {job ? (
                <form
                  className="business-form"
                  onSubmit={(e) => {
                    e.preventDefault();
                    const next = { ...job };
                    if (next.kind === "once") {
                      const d = new Date(next.at);
                      if (Number.isNaN(d.getTime())) {
                        setError("请填写有效的执行时间");
                        return;
                      }
                      next.at = d.toISOString();
                    }
                    void perform({ operation: "job-save", job: next })
                      .then((v) => {
                        setJob(v as Job);
                        setDirty(false);
                        notify("任务已保存");
                      })
                      .catch(() => {});
                  }}
                >
                  <label>
                    任务名称
                    <input
                      value={job.name}
                      onChange={(e) => editJob({ name: e.target.value })}
                    />
                  </label>
                  <Toggle
                    label="启用计划"
                    checked={job.enabled}
                    onChange={(enabled) => editJob({ enabled })}
                  />
                  <div className="business-pair">
                    <label>
                      执行方式
                      <select
                        value={job.kind}
                        onChange={(e) => editJob({ kind: e.target.value })}
                      >
                        <option value="cron">周期</option>
                        <option value="once">单次</option>
                      </select>
                    </label>
                    <label>
                      时区
                      <input
                        value={job.timezone}
                        onChange={(e) => editJob({ timezone: e.target.value })}
                      />
                    </label>
                  </div>
                  {job.kind === "cron" ? (
                    <label>
                      Cron 表达式
                      <input
                        value={job.expression}
                        onChange={(e) =>
                          editJob({ expression: e.target.value })
                        }
                        placeholder="0 9 * * *"
                      />
                      <small>
                        分 时 日 月 周；周日为 0。例如每工作日九点：0 9 * *
                        1-5。
                      </small>
                    </label>
                  ) : (
                    <label>
                      执行时间
                      <input
                        type="datetime-local"
                        value={job.at ? localDate(job.at) : ""}
                        onChange={(e) => editJob({ at: e.target.value })}
                      />
                      <small>单次时间按当前浏览器时区输入。</small>
                    </label>
                  )}
                  <label>
                    调用插件
                    <select
                      value={job.request.plugin}
                      onChange={(e) =>
                        inputJob({
                          plugin: e.target.value,
                          mode: "",
                          query: "",
                        })
                      }
                    >
                      <option value="text">发送文字</option>
                      {state.plugins
                        .filter(
                          (p) =>
                            businessIDs.includes(p.id) &&
                            !["cron", "auto_accept", "welcome"].includes(p.id),
                        )
                        .map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                    </select>
                  </label>
                  <TargetInput
                    label="任务目标"
                    contacts={state.contacts}
                    value={job.request.target}
                    onChange={(target) => inputJob({ target })}
                  />
                  {job.request.plugin === "news" && (
                    <label>
                      计划新闻类型
                      <select
                        aria-label="计划新闻类型"
                        value={job.request.mode}
                        onChange={(e) =>
                          inputJob({ mode: e.target.value, query: "" })
                        }
                      >
                        <option value="">跟随插件配置</option>
                        <option value="today">今日新闻 · 当天单图</option>
                        <option value="diagram">今日图卦 · 昨日双图</option>
                        <option value="rss">RSS 新闻 · 标题与链接</option>
                      </select>
                    </label>
                  )}
                  {(job.request.plugin !== "news" ||
                    (job.request.mode ||
                      String(
                        state.settings["news.default_mode"] || "today",
                      )) === "rss") && (
                    <label>
                      任务内容 / 查询参数
                      <textarea
                        value={job.request.query}
                        rows={3}
                        onChange={(e) => inputJob({ query: e.target.value })}
                      />
                    </label>
                  )}
                  {["statistics", "wordcloud", "digest"].includes(
                    job.request.plugin,
                  ) && (
                    <label>
                      消息范围（天）
                      <input
                        type="number"
                        min={1}
                        max={365}
                        value={job.request.days}
                        onChange={(e) =>
                          inputJob({ days: Number(e.target.value) })
                        }
                      />
                    </label>
                  )}
                  <button className="button primary" disabled={busy}>
                    {busy ? "保存中…" : "保存任务"}
                  </button>
                </form>
              ) : (
                <Empty
                  title="选择或新建任务"
                  text="计划与当前微信账号绑定，切换账号不会执行旧账号任务。"
                />
              )}
            </div>
          </>
        )}
        {tab === "assets" && (
          <>
            <form
              className="business-form business-asset-form"
              onSubmit={(e) => {
                e.preventDefault();
                void perform({
                  operation: "asset-save",
                  name: assetName,
                  media: assetMedia,
                })
                  .then(() => {
                    setAssetName("");
                    setAssetMedia("");
                    notify("素材已保存");
                  })
                  .catch(() => {});
              }}
            >
              <label>
                素材名称
                <input
                  value={assetName}
                  onChange={(e) => setAssetName(e.target.value)}
                />
              </label>
              <label>
                素材图片
                <input
                  type="file"
                  accept="image/png,image/jpeg"
                  onChange={async (e) => {
                    const f = e.target.files?.[0];
                    if (f)
                      try {
                        setAssetMedia(await readFile(f));
                        if (!assetName) setAssetName(f.name);
                      } catch (err) {
                        setError((err as Error).message);
                      }
                  }}
                />
              </label>
              <button className="button primary" disabled={busy}>
                保存素材
              </button>
            </form>
            <div className="business-assets">
              {data.assets.map((a) => (
                <article key={a.id}>
                  <img src={a.media} alt={a.name} />
                  <strong>{a.name}</strong>
                  <div className="actions">
                    <button
                      className="button"
                      onClick={() => {
                        change({ mode: "asset", asset: a.id });
                        setTab("preview");
                      }}
                    >
                      使用素材
                    </button>
                    <button
                      className="icon-button"
                      aria-label={`删除素材 ${a.name}`}
                      onClick={() => {
                        if (confirm("删除此素材？已发送消息不受影响。"))
                          void perform({
                            operation: "asset-delete",
                            id: a.id,
                          }).catch(() => {});
                      }}
                    >
                      <Trash size={16} />
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </>
        )}
        {tab === "records" && (
          <>
            <div className="business-heading">
              <p className="hint">
                最近 100 条记录；票据和服务密钥不写入记录。
              </p>
              <button className="button" onClick={() => void reload()}>
                <ArrowClockwise size={16} />
                刷新
              </button>
            </div>
            <div className="business-records">
              {data.runs
                .filter((v) => plugin.id === "cron" || v.plugin === plugin.id)
                .map((v) => (
                  <article key={v.id}>
                    <div className="business-heading">
                      <strong>{statuses[v.status] || v.status}</strong>
                      <time>{dateTime(v.time)}</time>
                    </div>
                    <p>{v.detail}</p>
                    <small>
                      {state.plugins.find((p) => p.id === v.plugin)?.name ||
                        v.plugin}{" "}
                      ·{" "}
                      {state.contacts.find((c) => c.id === v.target)?.name ||
                        v.target ||
                        "未选会话"}
                    </small>
                  </article>
                ))}
            </div>
            {!data.runs.some(
              (v) => plugin.id === "cron" || v.plugin === plugin.id,
            ) && (
              <Empty
                title="还没有执行记录"
                text="生成预览或运行任务后，记录会出现在这里。"
              />
            )}
          </>
        )}
      </div>
    </Modal>
  );
}
function localDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const offset = date.getTimezoneOffset() * 60000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

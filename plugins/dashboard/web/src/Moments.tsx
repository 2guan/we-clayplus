// 文件名：Moments.tsx
// 主要功能：紧凑朋友圈、真实互动动态、图文编辑和聊天头像资料浮窗。
// 作者：2guan
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Bell,
  ThumbsUp,
  Camera,
  ArrowClockwise,
  Plus,
  Smiley,
  X,
  Sparkle,
  ChatCircle,
  Stop,
} from "@phosphor-icons/react";
import type { Contact, Moment, PageProps } from "./types";
import { Avatar, Empty, Modal } from "./ui";
import { dateTime, readFile, request } from "./api";
import { EmojiPicker } from "./ChatTools";
import "./moments.css";
import { useMomentUnread } from "./useMomentUnread";

type MomentPage = {
  items: Moment[];
  unread: number;
  notice?: string;
  cover?: string;
  next_cursor?: string;
  notifications?: { id: string; moment_id: string; actor: string; kind: string; created: number; content: string; original?: string; read: boolean }[];
};
type ReplyComment = Moment["comments"][number];
const replyParams = (reply?: ReplyComment) => reply
  ? { reply_comment_id: Number(reply.id), reply_author_id: reply.author_id }
  : {};
const draftComment = (id: string, signal: AbortSignal, reply?: ReplyComment) =>
  request<{ text: string; warnings: string[] }>("/api/moments/draft", { id, ...replyParams(reply) }, signal);
const photo = (src: string) =>
  /^https?:\/\//.test(src)
    ? `/api/wechat-image?url=${encodeURIComponent(src)}`
    : src;

// 缩略图固定占位；加载失败不留下浏览器破图图标。
function Thumb({ src, onClick }: { src: string; onClick: () => void }) {
  const [failed, setFailed] = useState(false);
  return (
    <button
      type="button"
      className="moment-thumb"
      onClick={onClick}
      aria-label="查看动态配图"
    >
      {failed ? (
        <span>图片暂不可用</span>
      ) : (
        <img
          src={photo(src)}
          alt="动态配图"
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
        />
      )}
    </button>
  );
}
export function Moments(props: PageProps & { author?: string }) {
  const { state, notify, author } = props;
  const reminders = useMomentUnread(state.account.id, state.account.status === "online" || state.mock);
  const [page, setPage] = useState<MomentPage>({
    items: author
      ? state.moments.filter((m) => m.author_id === author)
      : state.moments,
    unread: 0,
  });
  const [loading, setLoading] = useState(false),
    [error, setError] = useState("");
  const [compose, setCompose] = useState(false),
    [notifications, setNotifications] = useState(false);
  const [preview, setPreview] = useState("");
  const [coverFailed, setCoverFailed] = useState(false);
  const bottom = useRef<HTMLDivElement>(null);
  const inFlight = useRef(false);
  const sequence = useRef(0);
  const owner = state.contacts.find((c) => c.id === author);
  const cover =
    page.cover ||
    (author && author !== state.account.id ? "" : state.account.moment_cover);
  async function load(older = false) {
    if (inFlight.current || (older && !page.next_cursor)) return;
    inFlight.current = true;
    const current = ++sequence.current;
    setLoading(true);
    setError("");
    try {
      const result = await request<MomentPage>(
        `/api/moments?${new URLSearchParams({ target: author || "", cursor: older ? page.next_cursor || "" : "" })}`,
      );
      if (current !== sequence.current) return;
      setPage((previous) => {
        const added = result.items.filter(
          (m) => !previous.items.some((p) => p.id === m.id),
        );
        const stuck =
          older &&
          (!added.length || result.next_cursor === previous.next_cursor);
        return {
          ...result,
          cover: result.cover || previous.cover,
          unread: older ? previous.unread : result.unread,
          items: older ? [...previous.items, ...added] : result.items,
          next_cursor: stuck ? "" : result.next_cursor,
          notice: stuck ? "微信暂未返回更早动态" : result.notice,
        };
      });
    } catch (e) {
      if (current === sequence.current) setError((e as Error).message);
    } finally {
      if (current === sequence.current) {
        inFlight.current = false;
        setLoading(false);
      }
    }
  }
  // 切换对象时让旧请求失效，避免迟到的分页数据串入另一个人的主页。
  useEffect(() => {
    inFlight.current = false;
    setPage({
      items: author
        ? state.moments.filter((m) => m.author_id === author)
        : state.moments,
      unread: 0,
    });
    void load();
    return () => {
      sequence.current++;
      inFlight.current = false;
    };
  }, [author]);
  // 头图不再阻塞动态列表，关闭页面后忽略迟到响应。
  useEffect(() => {
    let active = true;
    request<{ cover: string }>(
      `/api/moments/cover?${new URLSearchParams({ target: author || "" })}`,
    )
      .then((result) => {
        if (active && result.cover)
          setPage((p) => ({ ...p, cover: result.cover }));
      })
      .catch(() => {}); // 保留主题色回退，头图失败不影响动态阅读。
    return () => {
      active = false;
    };
  }, [author]);
  // 观察底部哨兵，兼容对话框和整页滚动；失败后只允许人工重试，不循环请求。
  useEffect(() => {
    if (!bottom.current || loading || error || !page.next_cursor) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void load(true);
      },
      {
        root: bottom.current.closest(".dialog"),
        threshold: 0.1,
        rootMargin: "0px 0px 300px 0px",
      },
    );
    observer.observe(bottom.current);
    return () => observer.disconnect();
  }, [page.next_cursor, page.items.length, loading, error]);
  useEffect(() => setCoverFailed(false), [cover]);
  return (
    <div className="moments moments-redesigned">
      <div className="moment-cover">
        {cover && !coverFailed && (
          <img
            className="moment-cover-photo"
            src={photo(cover)}
            alt="朋友圈封面"
            onError={() => setCoverFailed(true)}
          />
        )}
        <div className="moment-toolbar">
          <button
            className="icon-button"
            aria-label="刷新朋友圈"
            disabled={loading}
            onClick={() => load()}
          >
            <ArrowClockwise size={20} />
          </button>
          {!author && (
            <>
              <button
                className="icon-button"
                aria-label="朋友圈互动提醒"
                onClick={() => setNotifications(true)}
              >
                <Bell size={20} />
                {reminders.unread > 0 && (
                  <span className="moment-unread">{reminders.unread > 99 ? "99+" : reminders.unread}</span>
                )}
              </button>
              <button
                className="icon-button"
                aria-label="发表朋友圈"
                onClick={() => setCompose(true)}
              >
                <Camera size={21} />
              </button>
            </>
          )}
        </div>
        <div className="moment-owner">
          <strong>
            {owner?.remark ||
              owner?.name ||
              (author && author !== state.account.id
                ? author
                : state.account.name)}
          </strong>
          <Avatar
            src={
              owner?.avatar ||
              (!author || author === state.account.id
                ? state.account.photo
                : "")
            }
            name={owner?.name || state.account.name}
            blank={!author && state.account.status !== "online"}
            size="large"
          />
        </div>
      </div>
      {!author && props.user.role === "admin" && <MomentRandomControls key={state.account.id} settings={state.settings} onDone={() => void load()} />}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <Feed
        key={`${state.account.id}:${author || ""}`}
        items={page.items}
        onPreview={setPreview}
        onDraft={
          props.user.role === "admin"
            ? draftComment
            : undefined
        }
        onAct={async (kind, id, content, reply) => {
          try {
            const updated = await request<Moment>("/api/moments/interact", {
              kind,
              id,
              content: content || "",
              target: author || "",
              ...replyParams(reply),
            });
            // 仅更新操作的动态，不回到第一页，不打断已加载的历史列表。
            setPage((p) => ({
              ...p,
              items: p.items.map((m) => (m.id === id ? updated : m)),
            }));
          } catch (e) {
            notify((e as Error).message);
            throw e;
          }
        }}
      />
      {!loading && !error && !page.items.length && (
        <Empty title="暂无可见朋友圈" text="仅展示微信当前允许查看的动态。" />
      )}
      <div ref={bottom} className="moment-load-state" role="status">
        {loading ? (
          "正在加载动态…"
        ) : error ? (
          <button className="button" onClick={() => load(!!page.next_cursor)}>
            重新加载
          </button>
        ) : page.next_cursor ? (
          "下滑加载更多"
        ) : page.items.length ? (
          page.notice || "已显示全部可见动态"
        ) : (
          ""
        )}
      </div>
      {compose && (
        <Composer
          {...props}
          onClose={() => setCompose(false)}
          onDone={() => {
            setCompose(false);
            void load();
          }}
        />
      )}
      {notifications && (
        <Notifications key={state.account.id} {...props} onClose={() => setNotifications(false)} />
      )}
      <Modal
        open={!!preview}
        onClose={() => setPreview("")}
        title="动态配图"
        description="查看原图"
      >
        <img className="lightbox" src={preview} alt="动态配图" />
      </Modal>
    </div>
  );
}
function Feed({
  items,
  onPreview,
  onAct,
  onDraft,
}: {
  items: Moment[];
  onPreview: (src: string) => void;
  onAct?: (kind: string, id: string, content?: string, reply?: ReplyComment) => Promise<void>;
  onDraft?: (
    id: string,
    signal: AbortSignal,
    reply?: ReplyComment,
  ) => Promise<{ text: string; warnings: string[] }>;
}) {
  const [comments, setComments] = useState<Record<string, string>>({}),
    [busy, setBusy] = useState("");
  const [drafting, setDrafting] = useState("");
  const [draftNotes, setDraftNotes] = useState<Record<string, string>>({});
  const [replying, setReplying] = useState<Record<string, ReplyComment | undefined>>({});
  const posting = useRef(false);
  const edits = useRef<Record<string, number>>({});
  const fields = useRef<Record<string, HTMLInputElement | null>>({});
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  return (
    <div className="moment-feed">
      {items.map((m) => (
        <article className="moment" key={m.id}>
          <Avatar src={m.avatar} name={m.author} />
          <div>
            <h3>{m.author}</h3>
            {m.content && <p>{m.content}</p>}
            <div
              className={`moment-images ${m.images.length === 1 ? "single" : ""}`}
            >
              {(!m.link ? m.images : []).map((src, i) => (
                <Thumb
                  key={`${src}-${i}`}
                  src={src}
                  onClick={() =>
                    onPreview(photo(m.original_images?.[i] || src))
                  }
                />
              ))}
            </div>
            {m.link && /^https?:\/\//.test(m.link) && (
              <a
                className="moment-link"
                href={m.link}
                target="_blank"
                rel="noopener noreferrer"
              >
                {m.thumbnail || m.images[0] ? (
                  <img
                    src={photo(m.thumbnail || m.images[0])}
                    alt="链接缩略图"
                    loading="lazy"
                    decoding="async"
                    onError={(e) => {
                      e.currentTarget.style.visibility = "hidden";
                    }}
                  />
                ) : (
                  <span className="moment-link-placeholder" aria-hidden="true">
                    ↗
                  </span>
                )}
                <span className="moment-link-title">
                  {m.title || "查看分享链接"}
                </span>
              </a>
            )}
            <div className="moment-meta">
              <small>{dateTime(m.time)}</small>

              {onAct && (
                <button
                  className="moment-like-button"
                  aria-label={m.liked ? "取消赞" : "赞"}
                  aria-pressed={m.liked}
                  title={m.liked ? "取消赞" : "赞"}
                  disabled={!!busy || drafting === m.id}
                  onClick={async () => {
                    setBusy(m.id);
                    try {
                      await onAct("moment-like", m.id);
                    } catch {
                    } finally {
                      setBusy("");
                    }
                  }}
                >
                  <ThumbsUp size={18} weight={m.liked ? "fill" : "regular"} />
                </button>
              )}
            </div>
            {m.like_users?.length || m.likes > 0 ? (
              <div className="moment-likes">
                <ThumbsUp size={15} aria-hidden="true" />
                <span>
                  {m.like_users?.length
                    ? m.like_users.map((p) => p.name).join("、")
                    : "微信暂未返回点赞名单"}
                  {m.like_users?.length && m.likes > m.like_users.length
                    ? "（部分名单）"
                    : ""}
                </span>
              </div>
            ) : null}
            {m.comments.length > 0 && (
              <div className="comments">
                {m.comments.map((c, i) => (
                  <p key={i}>
                    <strong>{c.author}{(c.reply_to || c.reply_to_id) && <>回复{c.reply_to || "未知用户"}</>}：</strong>
                    {c.content}
                    {onAct && <button type="button" className="moment-comment-reply"
                      aria-label={`回复${c.author}的评论：${c.content}`}
                      disabled={!!busy || !!drafting || !c.id || !c.author_id}
                      title={!c.id || !c.author_id ? "微信未返回可回复的评论编号或作者" : "回复这条评论"}
                      onClick={() => {
                        edits.current[m.id] = (edits.current[m.id] || 0) + 1;
                        setReplying((previous) => ({ ...previous, [m.id]: c }));
                        fields.current[m.id]?.focus();
                      }}>回复</button>}
                  </p>
                ))}
              </div>
            )}
            {onAct && (
              <>
                {replying[m.id] && <div className="moment-reply-target" role="status">
                  <div><strong>回复 {replying[m.id]!.author}</strong><p>{replying[m.id]!.content}</p></div>
                  <button type="button" className="icon-button" aria-label={`取消回复${replying[m.id]!.author}`} disabled={!!busy}
                    onClick={() => {
                      edits.current[m.id] = (edits.current[m.id] || 0) + 1;
                      setReplying((previous) => ({ ...previous, [m.id]: undefined }));
                    }}><X size={16} /></button>
                </div>}
                <form
                  className="comment-input"
                  onSubmit={async (e) => {
                    e.preventDefault();
                    if (posting.current || busy || drafting === m.id || !comments[m.id]?.trim())
                      return;
                    posting.current = true;
                    setBusy(m.id);
                    try {
                      await onAct("moment-comment", m.id, comments[m.id], replying[m.id]);
                      setComments((previous) => ({ ...previous, [m.id]: "" }));
                      setReplying((previous) => ({ ...previous, [m.id]: undefined }));
                    } catch {
                    } finally {
                      posting.current = false;
                      setBusy("");
                    }
                  }}
                >
                  <input
                    ref={(el) => {
                      fields.current[m.id] = el;
                    }}
                    aria-label={replying[m.id] ? `回复 ${replying[m.id]!.author} 的评论` : `评论 ${m.author} 的动态`}
                    placeholder={replying[m.id] ? `回复 ${replying[m.id]!.author}…` : "评论…"}
                    value={comments[m.id] || ""}
                    onChange={(e) => {
                    const value = e.target.value;
                      edits.current[m.id] = (edits.current[m.id] || 0) + 1;
                      setComments((previous) => ({
                        ...previous,
                      [m.id]: value,
                      }));
                    }}
                  />
                  <button
                    className="button small"
                    disabled={
                      !!busy || drafting === m.id || !comments[m.id]?.trim()
                    }
                  >
                    发送
                  </button>
                  {onDraft && (
                    <button
                      type="button"
                      className="button small moment-draft-button"
                      aria-label={`AI生成${m.author}的留言草稿`}
                    title="阅读动态、缩略图、已有评论和相关记忆，生成留言草稿"
                      aria-busy={drafting === m.id}
                      disabled={!!busy || !!drafting}
                      onClick={async () => {
                        if (controller.current) return;
                        const current = new AbortController();
                        controller.current = current;
                        const revision = edits.current[m.id] || 0;
                        setDrafting(m.id);
                        setDraftNotes((previous) => ({
                          ...previous,
                          [m.id]: "",
                        }));
                        try {
                          const result = await onDraft(m.id, current.signal, replying[m.id]);
                          if (current.signal.aborted) return;
                          if ((edits.current[m.id] || 0) !== revision) {
                            setDraftNotes((previous) => ({
                              ...previous,
                              [m.id]: "已保留你编辑的留言，AI草稿未覆盖。",
                            }));
                          } else {
                            setComments((previous) => ({
                              ...previous,
                              [m.id]: result.text,
                            }));
                            setDraftNotes((previous) => ({
                              ...previous,
                              [m.id]:
                                result.warnings?.join("；") ||
                                "草稿已填入，确认后再发送。",
                            }));
                            fields.current[m.id]?.focus();
                          }
                        } catch (error) {
                          if (!current.signal.aborted)
                            setDraftNotes((previous) => ({
                              ...previous,
                              [m.id]: (error as Error).message,
                            }));
                        } finally {
                          if (!current.signal.aborted) setDrafting("");
                          controller.current = null;
                        }
                      }}
                    >
                      {drafting === m.id ? (
                        <span className="loader" aria-hidden="true" />
                      ) : (
                        <Sparkle size={15} aria-hidden="true" />
                      )}
                      AI
                    </button>
                  )}
                </form>
                {draftNotes[m.id] && (
                  <p className="moment-draft-note" role="status">
                    {draftNotes[m.id]}
                  </p>
                )}
              </>
            )}
          </div>
        </article>
      ))}
    </div>
  );
}
type RandomMomentJob = {
  id: string; kind: "like" | "comment" | "reply"; status: string;
  total: number; checked: number; sent: number; skipped: number; message: string;
};
function MomentRandomControls({ settings, onDone }: { settings: PageProps["state"]["settings"]; onDone: () => void }) {
  const [job, setJob] = useState<RandomMomentJob>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  const lastStatus = useRef("");
  const revision = useRef(0);
  const done = useRef(onDone);
  done.current = onDone;
  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const version=revision.current;
        const result = await request<RandomMomentJob>("/api/moments/random", undefined, controller.signal);
        if (controller.signal.aborted || version!==revision.current) return;
        setJob(result);
        setError("");
        if (lastStatus.current === "running" && result.status !== "running") done.current();
        lastStatus.current = result.status;
      } catch (e) { if (!controller.signal.aborted) setError((e as Error).message); }
      if (!controller.signal.aborted) timer = setTimeout(poll, 2000);
    }
    void poll();
    return () => { mounted.current = false; controller.abort(); clearTimeout(timer); };
  }, []);
  async function run(body: { kind?: string; stop?: string }) {
    if (busy) return;
    revision.current++;
    setBusy(true); setError("");
    try {
      const result = await request<RandomMomentJob>("/api/moments/random", body);
      if (mounted.current) { setJob(result); lastStatus.current = result.status; }
    } catch (e) { if (mounted.current) setError((e as Error).message); }
    finally { if (mounted.current) setBusy(false); }
  }
  const running = job?.status === "running";
  return <section className="moment-random-controls" aria-label="朋友圈随机互动">
    <div className="moment-random-buttons">
      <button type="button" className="button" disabled={busy || running} onClick={() => void run({ kind: "like" })}><ThumbsUp size={17} />随机点赞</button>
      <button type="button" className="button" disabled={busy || running} onClick={() => void run({ kind: "comment" })}><ChatCircle size={17} />随机评论</button>
      {running && <button type="button" className="button" disabled={busy} onClick={() => void run({ stop: job.id })}><Stop size={16} />停止</button>}
      <a className="text-link" href="#models?tab=global">互动设置</a>
    </div>
    <p className="muted">最近 {Number(settings["moments.lookback_hours"] ?? 24)} 小时 · 点赞 {Number(settings["moments.like_probability"] ?? 80)}% · 评论 {Number(settings["moments.comment_probability"] ?? 50)}% · 评论间隔 {Number(settings["moments.comment_interval_min"] ?? 15)}–{Number(settings["moments.comment_interval_max"] ?? 30)} 秒</p>
    {job?.id && <p role="status">{job.kind === "like" ? "随机点赞" : job.kind === "reply" ? "回应朋友圈回复" : "随机评论"} · {job.message} · 已检查 {job.checked}/{job.total}，成功 {job.sent}，跳过 {job.skipped}</p>}
    {error && <p role="alert" className="form-error">{error}</p>}
  </section>;
}

function Notifications({ onClose, ...props }: PageProps & { onClose: () => void }) {
  const [page, setPage] = useState<MomentPage>();
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Moment>();
  const [loading, setLoading] = useState("");
  const [preview, setPreview] = useState("");
  const pending = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    request<MomentPage>("/api/moments/notifications", undefined, controller.signal)
      .then((p) => { if (!controller.signal.aborted) setPage(p); })
      .catch((e) => { if (!controller.signal.aborted) setError(e.message); });
    return () => { controller.abort(); pending.current?.abort(); };
  }, []);
  async function openNotification(item: NonNullable<MomentPage["notifications"]>[number]) {
    if (pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setLoading(item.id);
    setError("");
    try {
      const moment = await request<Moment>(`/api/moments/notifications?id=${encodeURIComponent(item.id)}`, undefined, controller.signal);
      if (controller.signal.aborted) return;
      setSelected(moment);
      if (!item.read) {
        const result = await request<{ unread: number }>("/api/moments/notifications/read", { ids: [item.id] }, controller.signal);
        if (controller.signal.aborted) return;
        setPage((p) => p && ({ ...p, unread: result.unread, notifications: p.notifications?.map((n) => n.id === item.id ? { ...n, read: true } : n) }));
        window.dispatchEvent(new CustomEvent("weclay:moment-unread", { detail: { account: props.state.account.id, unread: result.unread } }));
      }
    } catch (e) {
      if (!controller.signal.aborted) setError((e as Error).message);
    } finally {
      if (!controller.signal.aborted) setLoading("");
      if (pending.current === controller) pending.current = null;
    }
  }
  return (
    <Modal open onClose={onClose} title="朋友圈消息" description="点击一条互动消息查看对应动态，并将该条标为已读">
      {!page && !error && <p role="status">正在读取互动提醒…</p>}
      {error && <p role="alert">{error}</p>}
      {page && <>
        {page.notice && <p className="muted">{page.notice}</p>}
        <div className="moment-notifications-list">
          {page.notifications?.map((item) => <button type="button" key={item.id}
            className={item.read ? "" : "is-unread"} disabled={!!loading}
            onClick={() => void openNotification(item)}>
            <strong>{item.actor || "微信用户"}{item.kind === "like" ? `赞了你的${item.original ? "评论" : "朋友圈"}` : item.original ? "回复了你的评论" : "评论了你的朋友圈"}</strong>
            {item.content && <p>{item.content}</p>}
            {item.original && <blockquote>{item.original}</blockquote>}
            <small>{dateTime(new Date(item.created * 1000).toISOString())} · {item.read ? "已读" : "未读"}{loading === item.id ? " · 正在打开…" : ""}</small>
          </button>)}
        </div>
        {!page.notifications?.length && <Empty title={page.unread ? `${page.unread} 条提醒暂无详情` : "暂无互动提醒"} />}
      </>}
      <Modal open={!!selected} onClose={() => setSelected(undefined)} title="互动消息详情" description="该条提醒对应的朋友圈">
        {error && <p role="alert">{error}</p>}
        {selected && <Feed key={selected.id} items={[selected]} onPreview={setPreview}
          onDraft={props.user.role === "admin" ? draftComment : undefined}
          onAct={async (kind, id, content, reply) => {
            try {
              const updated = await request<Moment>("/api/moments/interact", { kind, id, content: content || "", target: "", ...replyParams(reply) });
              setSelected((previous) => previous?.id === id ? updated : previous);
            } catch (error) {
              props.notify((error as Error).message);
              throw error;
            }
          }} />}
      </Modal>
      <Modal open={!!preview} onClose={() => setPreview("")} title="动态配图" description="查看原图">
        <img className="lightbox" src={preview} alt="动态配图" />
      </Modal>
    </Modal>
  );
}

function Composer({
  state,
  onClose,
  onDone,
}: PageProps & { onClose: () => void; onDone: () => void }) {
  const [text, setText] = useState(""),
    [images, setImages] = useState<{ file: File; url: string }[]>([]);
  const [emoji, setEmoji] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const [hidden, setHidden] = useState<string[]>([]),
    [mentioned, setMentioned] = useState<string[]>([]);
  const [picker, setPicker] = useState<"hidden" | "mentioned" | null>(null),
    [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null),
    urls = useRef<string[]>([]);
  useEffect(() => () => urls.current.forEach(URL.revokeObjectURL), []);
  const close = () => {
    if (
      !busy &&
      (!(text || images.length) || window.confirm("放弃未发布的内容？"))
    )
      onClose();
  };
  async function publish() {
    if (busy || uncertain) return;
    setError("");
    if (!text.trim() && !images.length) {
      setError("请输入内容或添加图片");
      return;
    }
    setBusy(true);
    try {
      await request("/api/moments", {
        content: text,
        images: await Promise.all(images.map((i) => readFile(i.file))),
        blacklist: hidden,
        with_users: mentioned,
      });
      onDone();
    } catch (e) {
      setError((e as Error).message);
      setUncertain(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      open
      onClose={close}
      title="发表朋友圈"
      description={
        state.mock ? "脱机预览，不会发布到微信" : "发表后将发送到当前微信账号"
      }
    >
      <form
        className="moment-editor"
        onSubmit={(e) => {
          e.preventDefault();
          void publish();
        }}
      >
        <textarea
          aria-label="朋友圈内容"
          placeholder="这一刻的想法…"
          value={text}
          disabled={busy || uncertain}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
              e.preventDefault();
              void publish();
            }
          }}
        />
        <div className="moment-emoji">
          <button
            type="button"
            className="icon-button"
            aria-label="选择表情"
            disabled={busy || uncertain}
            onClick={() => setEmoji(!emoji)}
          >
            <Smiley size={21} />
          </button>
          {emoji && (
            <EmojiPicker
              onSelect={(e) => {
                setText((t) => t + e);
                setEmoji(false);
              }}
              onClose={() => setEmoji(false)}
            />
          )}
        </div>
        <div className="moment-uploads">
          {images.map((i, index) => (
            <div key={i.url}>
              <img src={i.url} alt={`待发布图片 ${index + 1}`} />
              <button
                type="button"
                className="icon-button"
                disabled={busy || uncertain}
                aria-label={`移除图片 ${index + 1}`}
                onClick={() => {
                  URL.revokeObjectURL(i.url);
                  setImages(images.filter((v) => v !== i));
                }}
              >
                <X size={16} />
              </button>
            </div>
          ))}
          {images.length < 9 && (
            <button
              type="button"
              className="moment-add"
              aria-label="添加图片"
              disabled={busy || uncertain}
              onClick={() => input.current?.click()}
            >
              <Plus size={28} />
            </button>
          )}
        </div>
        <input
          hidden
          ref={input}
          type="file"
          accept="image/jpeg,image/png"
          multiple
          onChange={(e) => {
            const files = Array.from(e.target.files || []);
            e.target.value = "";
            if (
              files.some((f) => f.size > 8 * 1024 * 1024) ||
              files.reduce(
                (n, f) => n + f.size,
                images.reduce((n, i) => n + i.file.size, 0),
              ) >
                16 * 1024 * 1024 ||
              files.length + images.length > 9
            ) {
              setError("最多 9 张，单张不超过 8 MB，总计不超过 16 MB");
              return;
            }
            setImages([
              ...images,
              ...files.map((file) => {
                const url = URL.createObjectURL(file);
                urls.current.push(url);
                return { file, url };
              }),
            ]);
            setError("");
          }}
        />
        <button
          type="button"
          className="moment-option"
          disabled={busy || uncertain}
          onClick={() => {
            setPicker(picker === "mentioned" ? null : "mentioned");
            setSearch("");
          }}
        >
          <span>＠ 提醒谁看</span>
          <span>
            {mentioned.length ? `${mentioned.length} 位好友` : "不提醒"} ›
          </span>
        </button>
        <button
          type="button"
          className="moment-option"
          disabled={busy || uncertain}
          onClick={() => {
            setPicker(picker === "hidden" ? null : "hidden");
            setSearch("");
          }}
        >
          <span>谁可以看</span>
          <span>
            {hidden.length ? `不给 ${hidden.length} 位好友看` : "公开"} ›
          </span>
        </button>
        {picker && (
          <fieldset className="moment-audience">
            <legend>{picker === "hidden" ? "不给谁看" : "提醒好友"}</legend>
            <input
              aria-label="搜索好友"
              placeholder="搜索好友…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <div>
              {state.contacts
                .filter(
                  (c) =>
                    c.kind === "friend" &&
                    `${c.name} ${c.remark}`.includes(search),
                )
                .map((c) => (
                  <label key={c.id}>
                    <input
                      type="checkbox"
                      checked={(picker === "hidden"
                        ? hidden
                        : mentioned
                      ).includes(c.id)}
                      onChange={(e) => {
                        const update =
                          picker === "hidden" ? setHidden : setMentioned;
                        update((list) =>
                          e.target.checked
                            ? [...list, c.id]
                            : list.filter((id) => id !== c.id),
                        );
                        if (e.target.checked)
                          (picker === "hidden" ? setMentioned : setHidden)(
                            (list) => list.filter((id) => id !== c.id),
                          );
                      }}
                    />
                    {c.remark || c.name}
                  </label>
                ))}
            </div>
          </fieldset>
        )}
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {uncertain && (
          <p className="muted">请关闭编辑页，先在微信核对发布结果后再操作。</p>
        )}
        <footer>
          <button
            type="button"
            className="button"
            disabled={busy}
            onClick={close}
          >
            取消
          </button>
          <button className="button primary" disabled={busy || uncertain}>
            {busy ? "正在发表…" : "发表"}
          </button>
        </footer>
      </form>
    </Modal>
  );
}

export function PersonPopover(
  props: PageProps & {
    person: { id: string; name: string; anchor: HTMLElement };
    onClose: () => void;
    onChat: (c: Contact) => void;
  },
) {
  const { state, person, onClose, onChat } = props;
  const root = useRef<HTMLDivElement>(null),
    [showMoments, setShowMoments] = useState(false);
  const [detail, setDetail] = useState<Contact>();
  const [detailError, setDetailError] = useState("");
  const [editingRemark, setEditingRemark] = useState(false);
  const [remarkDraft, setRemarkDraft] = useState("");
  const [remarkBusy, setRemarkBusy] = useState(false);
  const c = detail || state.contacts.find((c) => c.id === person.id);
  useEffect(() => {
    let active = true;
    if (person.id)
      request<Contact>(`/api/person?id=${encodeURIComponent(person.id)}`)
        .then((c) => {
          if (active) setDetail(c);
        })
        .catch((e) => {
          if (active) setDetailError(e.message);
        });
    return () => {
      active = false;
    };
  }, [person.id]);
  const self = person.id === state.account.id;
  const recent = state.moments
    .filter((m) => m.author_id === person.id)
    .flatMap((m) => m.images)
    .slice(0, 3);
  useEffect(() => {
    const node = root.current;
    if (!node || showMoments) return;
    node.showPopover();
    const rect = person.anchor.getBoundingClientRect();
    node.style.left = `${Math.max(12, Math.min(rect.right + 10, window.innerWidth - 332))}px`;
    node.style.top = `${Math.max(12, Math.min(rect.top, window.innerHeight - node.offsetHeight - 12))}px`;
    node.showPopover();
    node.querySelector<HTMLButtonElement>("button")?.focus();
    const close = (e: ToggleEvent) => {
      if (e.newState === "closed") onClose();
    };
    node.addEventListener("toggle", close);
    return () => {
      node.removeEventListener("toggle", close);
      if (person.anchor.isConnected) person.anchor.focus();
    };
  }, [person, showMoments, detail]);
  return (
    <>
      {!showMoments &&
        createPortal(
          <div
            ref={root}
            popover="auto"
            className="person-popover"
            role="dialog"
            aria-label="人员资料"
          >
            <button
              className="icon-button person-close"
              aria-label="关闭人员资料"
              onClick={onClose}
            >
              <X size={17} />
            </button>
            <header>
              <Avatar
                src={self ? state.account.photo : c?.avatar}
                name={c?.name || person.name}
                size="large"
              />
              <div>
                <h3>{c?.remark || c?.name || person.name}</h3>
                <p>
                  {self
                    ? "我"
                    : c?.enterprise
                      ? "企业微信联系人"
                      : c?.kind === "friend"
                        ? "微信好友"
                        : "未确认好友关系"}
                </p>
              </div>
            </header>
            {c?.profile_note && <p role="status">{c.profile_note}</p>}
            {detailError && <p role="status">{detailError}，展示已归档资料</p>}
            <dl>
              <dt>昵称</dt>
              <dd>{c?.name || person.name}</dd>
              <dt>微信号</dt>
              <dd>{c?.alias || person.id || "暂未获取"}</dd>
              {c?.region && (
                <>
                  <dt>地区</dt>
                  <dd>{c.region}</dd>
                </>
              )}
              {c?.remark && (
                <>
                  <dt>备注</dt>
                  <dd>{c.remark}</dd>
                </>
              )}
              {c?.signature && (
                <>
                  <dt>签名</dt>
                  <dd>{c.signature}</dd>
                </>
              )}
            </dl>
            {c?.kind === "friend" &&
              !self &&
              (editingRemark ? (
                <form
                  className="form-stack"
                  onSubmit={async (e) => {
                    e.preventDefault();
                    if (remarkBusy) return;
                    setRemarkBusy(true);
                    try {
                      await props.act({
                        kind: "remark",
                        target: person.id,
                        content: remarkDraft,
                      });
                      setDetail({ ...c, remark: remarkDraft });
                      setEditingRemark(false);
                      props.notify("备注已保存");
                    } catch (e) {
                      props.notify((e as Error).message);
                    } finally {
                      setRemarkBusy(false);
                    }
                  }}
                >
                  <label>
                    备注名称
                    <input
                      autoFocus
                      value={remarkDraft}
                      onChange={(e) => setRemarkDraft(e.target.value)}
                    />
                  </label>
                  <div className="actions">
                    <button
                      type="button"
                      className="button small"
                      disabled={remarkBusy}
                      onClick={() => setEditingRemark(false)}
                    >
                      取消
                    </button>
                    <button
                      className="button primary small"
                      disabled={remarkBusy}
                    >
                      {remarkBusy ? "保存中…" : "保存备注"}
                    </button>
                  </div>
                </form>
              ) : (
                <button
                  className="button small"
                  onClick={() => {
                    setRemarkDraft(c.remark || "");
                    setEditingRemark(true);
                  }}
                >
                  设置备注
                </button>
              ))}
            {!c?.enterprise && (
              <button
                className="person-moments"
                disabled={!person.id}
                onClick={() => setShowMoments(true)}
              >
                <span>朋友圈</span>
                <span className="person-recent">
                  {recent.map((src, i) => (
                    <img key={i} src={photo(src)} alt="近期朋友圈" />
                  ))}
                </span>
                <span>›</span>
              </button>
            )}
            {c?.kind === "friend" && (
              <button className="button primary" onClick={() => onChat(c)}>
                发送消息
              </button>
            )}
          </div>,
          document.body,
        )}
      {showMoments && (
        <Modal
          open
          onClose={onClose}
          title={`${c?.remark || c?.name || person.name}的朋友圈`}
          description="仅展示对方当前允许查看的动态"
          wide
        >
          <Moments {...props} author={person.id} />
        </Modal>
      )}
    </>
  );
}

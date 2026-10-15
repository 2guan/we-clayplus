// 文件名：Chat.tsx
// 主要功能：实现微信式桌面会话与手机独立聊天页面，支持人工接管、媒体和专属策略。
// 作者：2guan
import {
  useChatMentions,
  MentionList,
  AvatarMentionMenu,
} from "./ChatMentions";
import type { GroupMember } from "./useGroupAvatars";
import { useGroupAvatars } from "./useGroupAvatars";
import { RoomProfile } from "./RoomProfile";
import { Moments, PersonPopover } from "./Moments";
export { Moments } from "./Moments";
import { PaymentDialog } from "./PaymentDialog";
import { MessageMenu, type MessageMenuTarget } from "./MessageMenu";
import { StickerLibrary, StickerPicker } from "./StickerLibrary";
import { WechatMessage } from "./WechatMessage";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  MagnifyingGlass,
  ChatCircleDots,
  Aperture,
  Newspaper,
  DotsThree,
  ArrowLeft,
  Smiley,
  Sticker,
  Microphone,
  Image,
  Paperclip,
  PaperPlaneTilt,
  HandPalm,
  PushPin,
  FileText,
  SlidersHorizontal,
  X,
  DownloadSimple,
  ArrowsOutSimple,
  ArrowsInSimple,
} from "@phosphor-icons/react";
import type { Contact, Message, PageProps } from "./types";
import { Avatar, Empty, Modal, Toggle } from "./ui";
import { clock, dateTime, readFile } from "./api";
import { EmojiPicker, AttachmentPreview, VoiceComposer } from "./ChatTools";
import { mergeFileMessages } from "./fileMessages";
import { compareContacts, matchesContact } from "./contactSearch";
import { ConversationScroller } from "./ConversationScroller";

function useScrollActivity() {
  const [scrolling, setScrolling] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const onScroll = () => {
    setScrolling(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setScrolling(false), 700);
  };
  return { scrolling, onScroll };
}

// 桌面保留三栏，手机以 mobileOpen 切换列表与聊天；会话草稿按目标 ID 隔离。
export default function Chat(props: PageProps) {
  const { state, act, notify } = props;
  const [focused, setFocused] = useState(false);
  const messageScroll = useScrollActivity();
  const chatMessages = useMemo(() => mergeFileMessages(state.messages), [state.messages]);
  // 分类写入地址，刷新或从外部进入公众号会话时仍落在正确列表。
  const initialParams = new URLSearchParams(location.hash.split("?")[1]);
  const [section, setSection] = useState<"chats" | "official">(() =>
    initialParams.get("tab") === "official" ||
    state.contacts.some(
      (c) => c.id === initialParams.get("target") && c.kind === "official",
    )
      ? "official"
      : "chats",
  );
  const [selected, select] = useState<string>(
    () =>
      new URLSearchParams(location.hash.split("?")[1]).get("target") ||
      state.contacts.find(
        (c) => (c.kind === "official") === (section === "official"),
      )?.id ||
      "",
  );
  const [mobileOpen, setMobileOpen] = useState(
    () => !!new URLSearchParams(location.hash.split("?")[1]).get("target"),
  );
  const [search, setSearch] = useState("");
  const searchInput = useRef<HTMLInputElement>(null);
  const [text, setText] = useState("");
  const [details, setDetails] = useState(false);
  const [roomOpen, setRoomOpen] = useState(false);
  const [moments, setMoments] = useState(false);
  const [person, setPerson] = useState<{
    id: string;
    name: string;
    anchor: HTMLElement;
  } | null>(null);
  const submitting = useRef(false); // 仅防止同一轮事件重复提交，不等待微信回执。
  const [payment, setPayment] = useState<Message | null>(null);
  const [preview, setPreview] = useState("");
  const [menu, setMenu] = useState<MessageMenuTarget | null>(null);
  const [attachments, setAttachments] = useState<File[]>([]);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [stickerOpen, setStickerOpen] = useState(false);
  const [stickerManagerOpen, setStickerManagerOpen] = useState(false);
  const [voiceOpen, setVoiceOpen] = useState(false);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const cursor = useRef({ start: 0, end: 0 });
  const selectedRef = useRef(selected);
  const [quote, setQuote] = useState<Message | null>(null);
  const [quotePreview, setQuotePreview] = useState<Message | null>(null);
  const [downloads, setDownloads] = useState<Set<string>>(new Set());
  const downloadFile = async (message: Message) => {
    const key = `${message.target}:${message.id}`;
    if (downloads.has(key)) return;
    setDownloads((current) => new Set(current).add(key));
    try {
      const response = await fetch(
        `/api/message-media?target=${encodeURIComponent(message.target)}&id=${encodeURIComponent(message.id)}`,
      );
      if (!response.ok) throw new Error("文件下载失败，请稍后重试或在微信中打开");
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement("a");
      link.href = url;
      link.download = message.content;
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch {
      notify("文件下载失败，请稍后重试或在微信中打开");
    } finally {
      setDownloads((current) => {
        const next = new Set(current);
        next.delete(key);
        return next;
      });
    }
  };
  const openQuote = (message: Message) => {
    const original = chatMessages.find(
      (item) =>
        item.target === message.target && (item.id === message.rich?.quote_id || item.displayID === message.rich?.quote_id),
    );
    if (!original || original.status === "revoked") {
      notify("引用原消息未归档或已撤回，暂时无法打开");
      return;
    }
    setQuotePreview(original);
  };
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const end = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const contactKinds: Record<string, string> = {
    friend: "微信好友",
    group: "群聊",
    official: "公众号 / 订阅号",
    system: "系统账号",
    contact: "未确认关系",
    enterprise: "企业微信",
  };
  const current = state.contacts.find(
    (c) =>
      c.id === selected && (c.kind === "official") === (section === "official"),
  );
  // 两个入口共用归档和聊天窗，仅按已识别的联系人类型分开呈现。
  const conversations = state.contacts
    .filter((c) => (c.kind === "official") === (section === "official"))
    .filter((c) => !!search || !!c.time || !!c.preview || c.id === selected)
    .sort(
      (a, b) =>
        Number(b.pinned) - Number(a.pinned) || b.time.localeCompare(a.time) || compareContacts(a, b),
    )
    .filter((c) => matchesContact(c, search, c.preview));
  const officialUnread = state.contacts
    .filter((c) => c.kind === "official")
    .reduce((total, c) => total + c.unread, 0);
  // 仅显示当前会话的真实自动回复任务，模型测试和其他会话不触发提示。
  const replying =
    current?.mode === "auto" &&
    state.settings["reply.enabled"] === true &&
    state.tasks.some(
      (t) =>
        t.target === selected &&
        t.source === "wechat" &&
        t.kind === "chat" &&
        ["queued", "running"].includes(t.status),
    );
  const messages = chatMessages
    .filter((m) => m.target === selected)
    .slice(-100);
  const group = useGroupAvatars(current?.kind === "group" ? current.id : "");
  const groupAvatars = group.avatars;
  const mentionMembers = group.members
    .filter((m) => m.id !== state.account.id)
    .map((m) => ({
      ...m,
      remark: state.contacts.find((c) => c.id === m.id)?.remark,
    }));
  const mentions = useChatMentions(
    current?.kind === "group" ? current.id : "",
    text,
    setText,
    textarea,
    mentionMembers,
  );
  const [avatarMenu, setAvatarMenu] = useState<{
    member: GroupMember;
    x: number;
    y: number;
  } | null>(null);
  useEffect(() => {
    setAvatarMenu(null);
  }, [selected]);
  useEffect(() => {
    const list = end.current?.parentElement;
    if (list) list.scrollTop = list.scrollHeight;
    // 列表最多显示 100 条，满额后条数不变，需跟随最后一条消息更新。
  }, [selected, messages.at(-1)?.id]);
  useEffect(() => {
    const target = new URLSearchParams(location.hash.split("?")[1]).get(
      "target",
    );
    const params = new URLSearchParams(location.hash.split("?")[1]);
    const targetContact = state.contacts.find((c) => c.id === target);
    setSection(
      targetContact
        ? targetContact.kind === "official"
          ? "official"
          : "chats"
        : params.get("tab") === "official"
          ? "official"
          : "chats",
    );
    if (target && target !== selected) {
      // 浏览器历史或外部入口切换会话时，同样隔离文字、附件和引用。
      setDrafts((previous) => ({ ...previous, [selected]: text }));
      const draft = drafts[target] || "";
      setText(draft);
      cursor.current = { start: draft.length, end: draft.length };
      setAttachments([]);
      setQuote(null);
      selectedRef.current = target;
      select(target);
      setMobileOpen(true);
      setVoiceOpen(false);
      setStickerOpen(false);
      setStickerManagerOpen(false);
      setEmojiOpen(false);
    }
  }, [location.hash, state.contacts]);
  const choose = (c: Contact) => {
    setDrafts({ ...drafts, [selected]: text });
    selectedRef.current = c.id;
    select(c.id);
    setText(drafts[c.id] || "");
    // 恢复会话草稿时同步光标，避免把新选表情插到另一会话的旧位置。
    const position = (drafts[c.id] || "").length;
    cursor.current = { start: position, end: position };
    setMobileOpen(true);
    setAttachments([]);
    setVoiceOpen(false);
    setStickerOpen(false);
    setStickerManagerOpen(false);
    setEmojiOpen(false);
    setQuote(null);
    void act({ kind: "target", target: c.id, values: { read: true } }).catch(
      (e) => notify(e.message),
    );
    history.replaceState(
      null,
      "",
      `#wechat?${c.kind === "official" ? "tab=official&" : ""}target=${encodeURIComponent(c.id)}`,
    );
  };
  const switchSection = (next: "chats" | "official") => {
    if (next === section) {
      setMobileOpen(false);
      return;
    }
    setDrafts((previous) => ({ ...previous, [selected]: text }));
    setSection(next);
    select("");
    selectedRef.current = "";
    setText("");
    setSearch("");
    setAttachments([]);
    setQuote(null);
    setVoiceOpen(false);
    setEmojiOpen(false);
    setDetails(false);
    setMobileOpen(false);
    history.replaceState(
      null,
      "",
      next === "official" ? "#wechat?tab=official" : "#wechat",
    );
  };
  // 文件选择与粘贴统一进入待发送队列，不在粘贴时自动发送。
  const addAttachments = (files: File[]) => {
    if (files.some((file) => file.size > 16 * 1024 * 1024 || file.size === 0)) {
      notify("附件不能为空，且每个文件不超过 16 MB");
      return;
    }
    if (attachments.length + files.length > 8) {
      notify("一次最多添加 8 个附件，请分批发送");
      return;
    }
    setAttachments((previous) => [...previous, ...files]);
  };
  const insertEmoji = (emoji: string) => {
    const { start, end } = cursor.current;
    setText((value) => value.slice(0, start) + emoji + value.slice(end));
    const position = start + emoji.length;
    cursor.current = { start: position, end: position };
    setEmojiOpen(false);
    requestAnimationFrame(() => {
      textarea.current?.focus();
      textarea.current?.setSelectionRange(position, position);
    });
  };
  const send = async () => {
    // 键盘事件紧随输入事件时，以编辑框当前可见正文为准，避免读取到上一轮渲染的草稿。
    const content = textarea.current?.value ?? text;
    if (
      (!content.trim() && !attachments.length) ||
      !current ||
      submitting.current
    )
      return;
    submitting.current = true;
    // 捕获本条内容后立即释放编辑器，旧请求完成时绝不修改下一条草稿。
    const target = selected;
    // 发送时再次用本条正文核对 @ 绑定，避免快速编辑后沿用前一条消息的成员 ID。
    const mentionIDs = mentions.idsFor(content);
    const files = attachments;
    const quoteID = quote?.id;
    setText("");
    setAttachments([]);
    setQuote(null);
    setEmojiOpen(false);
    setDrafts((previous) => ({ ...previous, [target]: "" }));
    requestAnimationFrame(() => {
      submitting.current = false;
      textarea.current?.focus();
    });
    // 各条独立等待回执；上一条未知或失败都不能阻止后续手动发送。
    const jobs = files.map(async (file) => {
      let media: string;
      try {
        media = await readFile(file);
      } catch (e) {
        if (selectedRef.current === target)
          setAttachments((items) => [...items, file]);
        throw e;
      }
      await act({
        kind: "send",
        target,
        content: file.name,
        group:
          file.type.startsWith("image/") && !file.type.includes("svg")
            ? "image"
            : "file",
        media,
        mime: file.type || "application/octet-stream",
      });
    });
    if (content.trim())
      jobs.push(
        act({
          kind: "send",
          target,
          content,
          quote_id: quoteID,
          mentions: mentionIDs,
          group: "text",
        }),
      );
    await Promise.all(
      jobs.map((job) => job.catch((e) => notify((e as Error).message))),
    );
  };
  return (
    <div className={`wechat-page ${mobileOpen ? "conversation-open" : ""} ${focused ? "wechat-focused" : ""}`}>
      {avatarMenu && (
        <AvatarMentionMenu
          target={avatarMenu}
          onClose={() => setAvatarMenu(null)}
          onPick={(m) => mentions.pick(m, false)}
        />
      )}
      <div className="wechat-rail">
        <Avatar
          src={state.account.photo}
          blank={state.account.status !== "online"}
          name={state.account.avatar || state.account.name}
        />
        <button
          className={section === "chats" ? "selected" : ""}
          onClick={() => switchSection("chats")}
          title="聊天"
          aria-label="聊天"
          aria-pressed={section === "chats"}
        >
          <ChatCircleDots size={23} />
        </button>
        <button
          className={section === "official" ? "selected" : ""}
          onClick={() => switchSection("official")}
          title="公众号 / 订阅号"
          aria-label="公众号 / 订阅号"
          aria-pressed={section === "official"}
        >
          <Newspaper size={23} />
          {officialUnread > 0 && (
            <span
              className="official-unread"
              aria-label={`${officialUnread} 条未读消息`}
            />
          )}
        </button>
        <button onClick={() => setMoments(true)} aria-label="朋友圈">
          <Aperture size={23} />
        </button>
        <button
          className="wechat-focus-toggle"
          onClick={() => setFocused((value) => !value)}
          aria-label={focused ? "退出微信专注模式" : "进入微信专注模式"}
          title={focused ? "退出微信专注模式" : "进入微信专注模式"}
          aria-pressed={focused}
        >
          {focused ? <ArrowsInSimple size={20} /> : <ArrowsOutSimple size={20} />}
        </button>
      </div>
      <aside className="conversation-list">
        <div className="conversation-header">
          <div className="conversation-categories" aria-label="会话分类">
            <button
              className={section === "chats" ? "selected" : ""}
              aria-pressed={section === "chats"}
              onClick={() => switchSection("chats")}
            >
              微信
            </button>
            <button
              className={section === "official" ? "selected" : ""}
              aria-pressed={section === "official"}
              onClick={() => switchSection("official")}
            >
              公众号 / 订阅号
              {officialUnread > 0 && <span className="status-dot" />}
            </button>
          </div>
          <button
            className="icon-button"
            onClick={() => setMoments(true)}
            aria-label="打开朋友圈"
          >
            <Aperture size={22} />
          </button>
        </div>
        <div className="search compact">
          <MagnifyingGlass size={17} />
          <input
            ref={searchInput}
            aria-label="搜索会话"
            placeholder="搜索会话…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search && (
            <button
              type="button"
              className="icon-button conversation-search-clear"
              aria-label="清空搜索"
              title="清空搜索"
              onClick={() => {
                setSearch("");
                searchInput.current?.focus();
              }}
            >
              <X size={16} />
            </button>
          )}
        </div>
        <ConversationScroller>
          {conversations.map((c) => (
            <button
              key={c.id}
              className={`conversation ${selected === c.id ? "active" : ""} ${c.pinned ? "pinned" : ""}`}
              onClick={() => choose(c)}
            >
              <div className="avatar-wrap">
                <Avatar
                  src={c.avatar}
                  name={c.name}
                  group={c.kind === "group"}
                />
                {/* 自动模式标记独立于未读状态，固定在头像左上角。 */}
                {c.mode === "auto" && (
                  <span
                    className="auto-mode-dot"
                    role="img"
                    aria-label="自动模式"
                    title="自动模式"
                  />
                )}
                {c.unread > 0 &&
                  (c.kind === "group" && c.muted ? (
                    <span
                      className="unread-dot"
                      aria-label="免打扰群有未读消息"
                    />
                  ) : (
                    <span className="unread">{c.unread}</span>
                  ))}
              </div>
              <div className="conversation-text">
                <div>
                  <strong>
                    {c.remark || c.name || c.id}
                    {(c.kind === "official" ||
                      c.kind === "system" ||
                      c.enterprise) && (
                      <small className="contact-kind">
                        {c.enterprise
                          ? c.kind === "group"
                            ? "企业微信群"
                            : "企业微信"
                          : contactKinds[c.kind]}
                      </small>
                    )}
                  </strong>
                  <small>{clock(c.time)}</small>
                </div>
                <div>
                  <p>
                    {c.unread > 0 &&
                      (() => {
                        // 仅标记尚未读到的归档支付通知，读完会话后自然消失。
                        const unread = state.messages
                          .filter((m) => m.target === c.id && !m.self)
                          .slice(-c.unread);
                        const labels = [
                          unread.some((m) => m.kind === "redpacket")
                            ? "红包"
                            : "",
                          unread.some((m) => m.kind === "transfer")
                            ? "转账"
                            : "",
                        ].filter(Boolean);
                        return labels.length ? (
                          <span className="conversation-payment">
                            [{labels.join("，")}]{" "}
                          </span>
                        ) : null;
                      })()}
                    {c.preview || "还没有消息"}
                  </p>
                  {c.mode === "manual" ? (
                    <HandPalm size={12} />
                  ) : c.pinned ? (
                    <PushPin size={12} />
                  ) : null}
                </div>
              </div>
            </button>
          ))}
          {!conversations.length && (
            <Empty
              title={
                search
                  ? "未找到会话"
                  : section === "official"
                    ? "暂无公众号消息"
                    : "暂无会话"
              }
              text={
                search
                  ? "试试其他名称或关键词。"
                  : "收到消息后，会话会出现在这里。"
              }
            />
          )}
        </ConversationScroller>
        <div className="conversation-foot">
          <span className="status-dot" />
          {conversations.length} 个会话 · {state.mock ? "虚构演示" : "本机同步"}
        </div>
      </aside>
      <section className="chat-window">
        {!current ? (
          <Empty title="开始一段沟通" text="从左侧选择一个会话。" />
        ) : (
          <>
            <div className="chat-title">
              <button
                className="icon-button mobile-back"
                aria-label="返回会话列表"
                onClick={() => {
                  setMobileOpen(false);
                  history.replaceState(
                    null,
                    "",
                    section === "official" ? "#wechat?tab=official" : "#wechat",
                  );
                }}
              >
                <ArrowLeft size={22} />
              </button>
              <div>
                <h2>
                  <button
                    className="chat-title-profile"
                    onClick={(e) => {
                      if (current.kind === "group") setRoomOpen(true);
                      else
                        setPerson({
                          id: current.id,
                          name: current.name,
                          anchor: e.currentTarget,
                        });
                    }}
                  >
                    {current.remark || current.name}
                  </button>
                  {current.kind === "group" && current.members > 0 && (
                    <span>({current.members})</span>
                  )}
                </h2>
              </div>
              <div className="chat-title-actions">
                {/* 服务端统一解析默认模式；点击标签后为该会话设置专属模式。 */}
                <div
                  className="chat-mode-tabs"
                  role="group"
                  aria-label="回复模式"
                >
                  {(
                    [
                      ["manual", "手动"],
                      ["auto", "自动"],
                    ] as const
                  ).map(([mode, label]) => (
                    <button
                      key={mode}
                      data-mode={mode}
                      type="button"
                      aria-pressed={current.mode === mode}
                      onClick={() => {
                        if (current.mode !== mode || current.mode_inherited)
                          act({
                            kind: "target",
                            target: selected,
                            values: { mode },
                          }).catch((e) => notify(e.message));
                      }}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <button
                  className="icon-button"
                  aria-label="会话资料与专属策略"
                  onClick={() => setDetails(true)}
                >
                  <DotsThree size={27} />
                </button>
              </div>
            </div>
            <div
              className={`message-list scroll-on-activity${messageScroll.scrolling ? " is-scrolling" : ""}`}
              onScroll={messageScroll.onScroll}
            >
              <div className="history-label">
                {state.mock ? "以下为虚构演示消息" : "仅展示接入后的已归档消息"}
              </div>
              {messages.map((m, index) => (
                <div key={m.displayID || m.id}>
                  {(index === 0 || index % 5 === 0) && (
                    <div className="message-time">{dateTime(m.time)}</div>
                  )}
                  {m.status === "revoked" ? (
                    <div className="system-message">你撤回了一条消息</div>
                  ) : m.kind === "system" ? (
                    <div className="system-message">{m.content}</div>
                  ) : (
                    <div className={`message ${m.self ? "self" : ""}`}>
                      <button
                        className="person-avatar-button"
                        onContextMenu={(e) => {
                          if (
                            current.kind !== "group" ||
                            m.self ||
                            !m.sender_id
                          )
                            return;
                          e.preventDefault();
                          setMenu(null);
                          setPerson(null);
                          const rect = e.currentTarget.getBoundingClientRect();
                          setAvatarMenu({
                            member: mentionMembers.find(
                              (p) => p.id === m.sender_id,
                            ) || { id: m.sender_id, name: m.sender },
                            x: e.clientX || rect.left,
                            y: e.clientY || rect.bottom,
                          });
                        }}
                        aria-label={`查看${m.self ? state.account.name : m.sender}的资料`}
                        onClick={(e) =>
                          setPerson({
                            id: m.self
                              ? state.account.id
                              : m.sender_id ||
                                (current.kind !== "group" ? current.id : ""),
                            name: m.self ? state.account.name : m.sender,
                            anchor: e.currentTarget,
                          })
                        }
                      >
                        <Avatar
                          src={
                            m.self
                              ? state.account.photo
                              : m.sender_avatar ||
                                groupAvatars[m.sender_id || ""] ||
                                state.contacts.find((c) => c.id === m.sender_id)
                                  ?.avatar ||
                                (current.kind !== "group"
                                  ? current.avatar
                                  : undefined)
                          }
                          name={m.self ? state.account.name : m.sender}
                          priority
                          size="small"
                        />
                      </button>
                      <div className="message-body">
                        {!m.self && current.kind === "group" && (
                          <span className="sender">{m.sender}</span>
                        )}
                        <div className="message-bubble-line">
                          <div
                            tabIndex={0}
                            className={`bubble bubble-${m.kind} ${m.kind === "image" ? "image-bubble" : ""}`}
                            onContextMenu={(e) => {
                              e.preventDefault();
                              const rect =
                                e.currentTarget.getBoundingClientRect();
                              setMenu({
                                message: m,
                                x: e.clientX || rect.left,
                                y: e.clientY || rect.bottom,
                                trigger: e.currentTarget,
                              });
                            }}
                          >
                            {/* 来源折角只标记自己发出的消息，不占正文和下方状态行的空间。 */}
                            {m.self && (
                              <span
                                className={`message-source-corner ${m.source === "ai" ? "automatic" : "manual"}`}
                                role="img"
                                aria-label={
                                  m.source === "ai" ? "自动发送" : "人工发送"
                                }
                                title={
                                  m.source === "ai" ? "自动发送" : "人工发送"
                                }
                              />
                            )}
                            {m.articles?.length ? (
                              <div className="message-articles">
                                {m.articles
                                  .filter((a) => /^https?:\/\//i.test(a.url))
                                  .map((article, i) => (
                                    <a
                                      className={`message-article${article.image?.startsWith("/api/message-thumbnail?id=") ? " message-article-compact" : ""}`}
                                      key={`${article.url}-${i}`}
                                      href={article.url}
                                      target="_blank"
                                      rel="noopener noreferrer"
                                    >
                                      {article.image && (
                                        <img
                                          loading="lazy"
                                          src={
                                            article.image.startsWith(
                                              "/api/message-thumbnail?id=",
                                            )
                                              ? article.image
                                              : `/api/wechat-image?url=${encodeURIComponent(article.image)}`
                                          }
                                          alt=""
                                          onError={(e) => {
                                            e.currentTarget.hidden = true;
                                          }}
                                        />
                                      )}
                                      <strong>
                                        {article.title || "查看文章"}
                                      </strong>
                                      {article.description && (
                                        <p>{article.description}</p>
                                      )}
                                      <small>
                                        {m.publisher || "网页链接"} · 打开阅读
                                      </small>
                                    </a>
                                  ))}
                              </div>
                            ) : m.kind === "video" && m.media ? (
                              <video
                                controls
                                preload="metadata"
                                width="320"
                                height="180"
                                src={m.media}
                              />
                            ) : m.kind === "file" ? (
                              <div className="file-message">
                                <FileText size={33} />
                                <span>
                                  {m.content}
                                  <small>{m.rich?.description || "文件附件"}</small>
                                </span>
                                {m.rich?.status !== "uploading" && (
                                  <a
                                    href={`/api/message-media?target=${encodeURIComponent(m.target)}&id=${encodeURIComponent(m.id)}`}
                                    download={m.content}
                                    aria-label="下载文件"
                                    aria-busy={downloads.has(`${m.target}:${m.id}`)}
                                    aria-disabled={downloads.has(`${m.target}:${m.id}`)}
                                    title={downloads.has(`${m.target}:${m.id}`) ? "正在下载…" : "下载文件"}
                                    onClick={(event) => {
                                      event.preventDefault();
                                      void downloadFile(m);
                                    }}
                                  >
                                    <DownloadSimple size={20} />
                                  </a>
                                )}
                              </div>
                            ) : (
                              <WechatMessage
                                key={m.id}
                                message={m}
                                openPayment={
                                  props.user.role === "admin"
                                    ? setPayment
                                    : undefined
                                }
                                openQuote={openQuote}
                                preview={setPreview}
                              />
                            )}
                          </div>
                          <button
                            className="message-menu-trigger"
                            aria-label="消息操作"
                            onClick={(e) => {
                              const rect =
                                e.currentTarget.getBoundingClientRect();
                              setMenu({
                                message: m,
                                x: rect.left,
                                y: rect.bottom,
                                trigger: e.currentTarget,
                              });
                            }}
                          >
                            <DotsThree size={16} />
                          </button>
                        </div>
                        {/* 仅发送中和异常保留状态提示，正常消息不再占用底部一行。 */}
                        {m.self &&
                          ["sending", "unknown", "failed"].includes(
                            m.status,
                          ) && (
                            <div className="message-meta" role="status">
                              {m.status === "sending" ? (
                                "发送中…"
                              ) : (
                                <span className="error-text">
                                  {m.status === "unknown"
                                    ? "结果未知，请核对"
                                    : "发送失败"}
                                </span>
                              )}
                            </div>
                          )}
                      </div>
                    </div>
                  )}
                </div>
              ))}
              {!messages.length && (
                <Empty
                  title="还没有消息"
                  text="发送第一条消息，开始这段对话。"
                />
              )}
              <div ref={end} />
            </div>
            {replying && (
              <div
                className="chat-reply-status"
                role="status"
                aria-live="polite"
              >
                <span aria-hidden="true" />
                正在回复…
              </div>
            )}
            <div className="composer">
              {mentions.open && (
                <MentionList
                  mention={mentions}
                  loading={group.loading}
                  error={group.error}
                  retry={group.retry}
                />
              )}
              <div className="composer-toolbar">
                <div className="emoji-anchor">
                  <button
                    className="icon-button"
                    aria-label="插入表情"
                    aria-expanded={emojiOpen}
                    aria-haspopup="dialog"
                    disabled={false}
                    onClick={() => {
                      setStickerOpen(false);
                      setEmojiOpen(!emojiOpen);
                    }}
                  >
                    <Smiley size={21} />
                  </button>
                  {emojiOpen && (
                    <EmojiPicker
                      onSelect={insertEmoji}
                      onClose={() => {
                        setEmojiOpen(false);
                        textarea.current?.focus();
                      }}
                    />
                  )}
                </div>
                <div className="emoji-anchor">
                  <button
                    className="icon-button"
                    aria-label="打开表情库"
                    title="表情包"
                    aria-haspopup="dialog"
                    aria-expanded={stickerOpen}
                    disabled={false}
                    onClick={() => {
                      setEmojiOpen(false);
                      setStickerOpen(!stickerOpen);
                    }}
                  >
                    <Sticker size={21} />
                  </button>
                  {stickerOpen && (
                    <StickerPicker
                      key={selected}
                      disabled={state.account.status !== "online"}
                      onClose={() => {
                        setStickerOpen(false);
                        textarea.current?.focus();
                      }}
                      onManage={() => {
                        setStickerOpen(false);
                        setStickerManagerOpen(true);
                      }}
                      onSend={async (id) => {
                        await act({
                          kind: "send",
                          target: selected,
                          sticker_id: id,
                        });
                      }}
                    />
                  )}
                </div>

                <button
                  className="icon-button"
                  aria-label="发送图片"
                  onClick={() => {
                    if (fileInput.current) {
                      fileInput.current.accept = "image/*";
                      fileInput.current.click();
                    }
                  }}
                >
                  <Image size={21} />
                </button>
                <button
                  className="icon-button"
                  aria-label="添加附件"
                  onClick={() => {
                    if (fileInput.current) {
                      fileInput.current.accept = "*/*";
                      fileInput.current.click();
                    }
                  }}
                >
                  <Paperclip size={21} />
                </button>
                <button
                  className="icon-button"
                  aria-label="文字转语音"
                  disabled={false}
                  onClick={() => {
                    setEmojiOpen(false);
                    setVoiceOpen(true);
                  }}
                >
                  <Microphone size={21} />
                </button>
                <span />
                <button
                  className="icon-button"
                  aria-label="专属策略"
                  onClick={() => setDetails(true)}
                >
                  <SlidersHorizontal size={20} />
                </button>
              </div>
              <input
                hidden
                ref={fileInput}
                type="file"
                multiple
                onChange={(e) => {
                  addAttachments(Array.from(e.target.files || []));
                  e.target.value = "";
                }}
              />
              {quote && (
                <div className="attachment-preview">
                  引用：{quote.content.slice(0, 80)}
                  <button aria-label="取消引用" onClick={() => setQuote(null)}>
                    <X size={15} />
                  </button>
                </div>
              )}
              {!!attachments.length && (
                <div className="attachment-queue">
                  {attachments.map((file, index) => (
                    <AttachmentPreview
                      key={`${file.name}-${index}`}
                      file={file}
                      disabled={false}
                      onRemove={() =>
                        setAttachments((items) =>
                          items.filter((_, i) => i !== index),
                        )
                      }
                    />
                  ))}
                </div>
              )}
              <textarea
                aria-label="消息内容"
                aria-autocomplete="list"
                aria-controls={
                  mentions.open ? "chat-mention-options" : undefined
                }
                aria-activedescendant={
                  mentions.open && mentions.options.length
                    ? `mention-${mentions.index}`
                    : undefined
                }
                onBlur={() => mentions.close()}
                ref={textarea}
                onSelect={(event) => {
                  mentions.move(event.currentTarget.selectionStart);
                  cursor.current = {
                    start: event.currentTarget.selectionStart,
                    end: event.currentTarget.selectionEnd,
                  };
                }}
                onPaste={(event) => {
                  const files = Array.from(event.clipboardData.files);
                  if (files.length) {
                    event.preventDefault();
                    addAttachments(files);
                  }
                }}
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  mentions.move(e.target.selectionStart);
                }}
                placeholder="输入消息…"
                onKeyDown={(e) => {
                  if (mentions.keydown(e)) return;
                  if (
                    e.key === "Enter" &&
                    !e.shiftKey &&
                    !e.ctrlKey &&
                    !e.metaKey &&
                    !e.nativeEvent.isComposing &&
                    window.innerWidth >= 768
                  ) {
                    e.preventDefault();
                    void send();
                  }
                }}
              />
              <div className="composer-footer">
                <small>Enter 发送 · Shift + Enter 换行</small>
                <button
                  className="button primary"
                  disabled={!text.trim() && !attachments.length}
                  onClick={send}
                >
                  发送
                  <PaperPlaneTilt size={16} />
                </button>
              </div>
            </div>
          </>
        )}
      </section>
      {stickerManagerOpen && (
        <StickerLibrary
          {...props}
          target={selected}
          onClose={() => setStickerManagerOpen(false)}
        />
      )}
      {voiceOpen && current && (
        <VoiceComposer
          key={current.id}
          contact={current}
          {...props}
          onClose={() => setVoiceOpen(false)}
        />
      )}
      {payment && (
        <PaymentDialog
          key={payment.id}
          {...props}
          message={payment}
          onClose={() => setPayment(null)}
        />
      )}
      {roomOpen && current && (
        <RoomProfile
          key={current.id}
          {...props}
          contact={current}
          onClose={() => setRoomOpen(false)}
        />
      )}
      <Modal
        open={details && !!current}
        onClose={() => setDetails(false)}
        title="会话资料与专属策略"
        description="专属设置只影响当前会话；留空使用全局默认。"
      >
        {current && (
          <TargetForm key={current.id} contact={current} {...props} />
        )}
      </Modal>
      {person && (
        <PersonPopover
          {...props}
          person={person}
          onClose={() => setPerson(null)}
          onChat={(c) => {
            setPerson(null);
            setSection("chats");
            choose(c);
          }}
        />
      )}
      <Modal
        open={moments}
        onClose={() => setMoments(false)}
        title="朋友圈"
        description="动态与互动"
        wide
      >
        <Moments {...props} />
      </Modal>
      <Modal
        open={!!quotePreview}
        onClose={() => setQuotePreview(null)}
        title="引用原消息"
      >
        {quotePreview && (
          <div className="quote-original">
            <p>{quotePreview.sender}</p>
            {quotePreview.articles?.length ? (
              quotePreview.articles.map((article, i) => (
                <a
                  key={i}
                  className="message-article"
                  href={article.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {article.title || "打开链接"}
                </a>
              ))
            ) : quotePreview.kind === "file" ? (
              <>
                <p>{quotePreview.content}</p>
                {quotePreview.rich?.status !== "uploading" && (
                  <a href={`/api/message-media?target=${encodeURIComponent(quotePreview.target)}&id=${encodeURIComponent(quotePreview.id)}`} download={quotePreview.content}
                    aria-disabled={downloads.has(`${quotePreview.target}:${quotePreview.id}`)}
                    onClick={(event) => {
                      event.preventDefault();
                      void downloadFile(quotePreview);
                    }}>
                    {downloads.has(`${quotePreview.target}:${quotePreview.id}`) ? "正在下载…" : "下载文件"}
                  </a>
                )}
              </>
            ) : (
              <WechatMessage
                key={quotePreview.id}
                message={quotePreview}
                preview={setPreview}
                openQuote={openQuote}
              />
            )}
          </div>
        )}
      </Modal>
      <Modal
        open={!!preview}
        onClose={() => setPreview("")}
        title="图片预览"
        description="点击下载可保存原始文件。"
        wide
      >
        {preview && (
          <>
            <img className="lightbox" src={preview} alt="聊天图片大图" />
            <a className="button" href={preview} download>
              下载图片
            </a>
          </>
        )}
      </Modal>
      {menu && (
        <MessageMenu
          target={menu}
          onClose={() => setMenu(null)}
          onAction={(action) => {
            const m = menu.message;
            setMenu(null);
            if (action === "resend") {
              if (
                m.status === "unknown" &&
                !confirm(
                  "这条消息可能已经送达。重发可能产生重复消息，是否继续？",
                )
              )
                return;
              void act({
                kind: "resend",
                id: m.id,
                target: m.target,
                enabled: m.status === "unknown",
              })
                .then(() => notify("已重发，原未成功记录已删除"))
                .catch((e) => notify(e.message));
            } else if (action === "copy") {
              navigator.clipboard
                .writeText(m.content)
                .then(() => notify("已复制"))
                .catch(() => notify("当前浏览器无法访问剪贴板"));
            } else if (action === "quote") {
              setQuote(m);
              textarea.current?.focus();
            } else {
              void act({
                kind: action === "delete" ? "delete-message" : "revoke",
                id: m.id,
                target: m.target,
              })
                .then(() => {
                  if (action === "delete") {
                    setQuote((old) => (old?.id === m.id ? null : old));
                    notify("已删除后台消息记录");
                  }
                })
                .catch((e) => notify(e.message));
            }
          }}
        />
      )}
    </div>
  );
}
// 专属策略只覆写当前目标；清空记忆改变上下文起点，不删除归档。
export function TargetForm({
  contact,
  state,
  act,
  notify,
}: PageProps & { contact: Contact }) {
  const old = state.overrides[contact.id] || {};
  const [prompt, setPrompt] = useState(String(old.prompt || ""));
  // 库中人设载入为当前会话的副本，继续编辑不会修改共享人设库。
  const [promptSource, setPromptSource] = useState(() => {
    if (!old.prompt) return "default";
    const persona = state.prompts.find((item) => item.content === old.prompt);
    return persona ? `persona:${persona.id}` : "custom";
  });
  const [model, setModel] = useState(String(old.model || ""));
  const [remark, setRemark] = useState(contact.remark || "");
  const remarkInput = useRef<HTMLInputElement>(null);
  const [remarkOpen, setRemarkOpen] = useState(false);
  const [remarkBusy, setRemarkBusy] = useState(false);
  const globalProbability = Number(state.settings["reply.probability"] || 0);
  // 用独立开关区分“跟随全局”和明确的 0%，全局数值随状态刷新实时展示。
  const [inheritProbability, setInheritProbability] = useState(
    old.probability == null,
  );
  const [probability, setProbability] = useState(
    String(old.probability ?? globalProbability),
  );
  const [busy, setBusy] = useState(false);
  return (
    <>
      <form
        className="form-stack target-settings"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await act({
              kind: "target",
              target: contact.id,
              values: {
                prompt: prompt.trim() || null,
                model: model || null,
                ...(contact.kind === "group"
                  ? {
                      probability: inheritProbability
                        ? null
                        : Number(probability),
                    }
                  : {}),
              },
            });
            notify("专属策略已保存");
          } catch (e) {
            notify((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="profile-line">
          <Avatar
            src={contact.avatar}
            name={contact.name}
            size="large"
            group={contact.kind === "group"}
          />
          <div>
            <h3>{contact.remark || contact.name}</h3>
            <code>{contact.id}</code>
          </div>
          <button
            type="button"
            className="button small target-remark-button"
            onClick={() => {
              setRemark(contact.remark || "");
              setRemarkOpen(true);
            }}
          >
            设置备注
          </button>
        </div>
        <div className="field-row">
          <span>
            {contact.wechat_pinned !== undefined
              ? "微信置顶（同步）"
              : "后台本地置顶"}
          </span>
          {contact.wechat_pinned !== undefined ? (
            <span className="muted">
              {contact.wechat_pinned ? "已置顶" : "未置顶"} · 请在微信中修改
            </span>
          ) : (
            <Toggle
              label="后台本地置顶"
              checked={contact.pinned}
              onChange={(pinned) =>
                act({
                  kind: "target",
                  target: contact.id,
                  values: { pinned },
                }).catch((e) => notify(e.message))
              }
            />
          )}
        </div>
        {contact.kind === "group" && (
          <div className="target-probability">
            <label>
              群回复概率
              <select
                aria-label="群回复概率来源"
                value={inheritProbability ? "default" : "custom"}
                onChange={(e) =>
                  setInheritProbability(e.target.value === "default")
                }
              >
                <option value="default">跟随全局 · {globalProbability}%</option>
                <option value="custom">单独设置</option>
              </select>
            </label>
            <label>
              当前概率（%）
              <input
                aria-label="群回复概率"
                type="number"
                min={0}
                max={100}
                step={1}
                required
                disabled={inheritProbability}
                value={inheritProbability ? globalProbability : probability}
                onChange={(e) => setProbability(e.target.value)}
              />
            </label>
            <small>
              普通群消息的回复概率，仍受自动模式和全局生效规则限制。
            </small>
          </div>
        )}
        <label>
          专属模型
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="">继承全局默认</option>
            {state.models
              .filter((m) => m.capabilities.includes("chat"))
              .map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
          </select>
        </label>
        <label>
          提示词来源
          <select
            value={promptSource}
            onChange={(event) => {
              const source = event.target.value;
              setPromptSource(source);
              if (source === "default") setPrompt("");
              else if (source.startsWith("persona:")) {
                const persona = state.prompts.find(
                  (item) => `persona:${item.id}` === source,
                );
                if (persona) setPrompt(persona.content);
              }
            }}
          >
            <option value="default">继承全局人设</option>
            <option value="custom">自定义输入</option>
            <optgroup label="人设库">
              {state.prompts.map((persona) => (
                <option key={persona.id} value={`persona:${persona.id}`}>
                  {persona.name}
                </option>
              ))}
            </optgroup>
          </select>
        </label>
        <label>
          专属人设提示词
          <textarea
            rows={5}
            placeholder="留空继承全局人设…"
            value={prompt}
            onChange={(e) => {
              setPrompt(e.target.value);
              setPromptSource("custom");
            }}
          />
        </label>
        <p className="muted">
          从人设库载入后可继续修改，保存仅影响当前会话；留空继承全局人设。
        </p>

        <div className="form-footer">
          <button
            type="button"
            className="button ghost"
            title="仅清空 AI 上下文，保留聊天记录"
            onClick={() => {
              if (confirm("清空此会话的 AI 记忆？聊天记录将保留。"))
                void act({ kind: "clear-memory", target: contact.id })
                  .then(() => notify("AI 记忆已清空"))
                  .catch((e) => notify(e.message));
            }}
          >
            清空记忆
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? "保存中…" : "保存专属策略"}
          </button>
        </div>
      </form>
      {/* 备注独立保存，取消不影响已有备注，也不会提交下方专属策略草稿。 */}
      <Modal
        open={remarkOpen}
        initialFocus={() => remarkInput.current?.focus()}
        onClose={() => {
          if (!remarkBusy) setRemarkOpen(false);
        }}
        title="设置备注"
        description="留空可清除备注。"
      >
        <form
          className="form-stack target-remark-editor"
          onSubmit={async (e) => {
            e.preventDefault();
            if (remarkBusy) return;
            setRemarkBusy(true);
            try {
              await act({
                kind: "remark",
                target: contact.id,
                content: remark,
              });
              setRemarkOpen(false);
              notify("备注已保存");
            } catch (error) {
              notify((error as Error).message);
            } finally {
              setRemarkBusy(false);
            }
          }}
        >
          <label>
            备注名称
            <input
              ref={remarkInput}
              value={remark}
              disabled={remarkBusy}
              onChange={(e) => setRemark(e.target.value)}
            />
          </label>
          <div className="form-footer">
            <button
              type="button"
              className="button"
              disabled={remarkBusy}
              onClick={() => setRemarkOpen(false)}
            >
              取消
            </button>
            <button className="button primary" disabled={remarkBusy}>
              {remarkBusy ? "保存中…" : "保存备注"}
            </button>
          </div>
        </form>
      </Modal>
    </>
  );
}
// Mock 动态也经过同一个 Action API，便于验证持久化和错误反馈。

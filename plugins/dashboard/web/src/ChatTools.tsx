// 文件名：ChatTools.tsx
// 主要功能：提供本机表情选择、附件预览和按已配置模型合成并发送语音的聊天工具。
// 作者：2guan
import { useEffect, useRef, useState } from "react";
import { X, Paperclip, MagnifyingGlass } from "@phosphor-icons/react";
import type { Contact, PageProps } from "./types";
import { Modal } from "./ui";
import { request } from "./api";
import { wechatEmojiPaths } from "./wechatEmoji";

type EmojiItem = {
  emoji: string;
  name: string;
  path?: string;
};

// 微信原生表情：使用内置切图，选中后插入如 [微笑] 等微信标准中文表情码。
const wechatEmojiGroup: { name: string; items: EmojiItem[] } = {
  name: "微信",
  items: Object.entries(wechatEmojiPaths).map(([code, path]) => ({
    emoji: code,
    name: code.slice(1, -1),
    path,
  })),
};

// 表情数据随前端内嵌，不访问第三方表情服务；选择结果为标准 Unicode 文本。
const unicodeEmojiGroups: { name: string; items: EmojiItem[] }[] = [
  {
    name: "表情",
    items:
      "😊微笑 😄开心 😁露齿笑 😂笑哭 🤣大笑 🙂友好 😉眨眼 😍喜欢 🥰爱慕 😘飞吻 😎酷 🤔思考 🙄白眼 😮惊讶 😅尴尬 😌放松 😴困倦 😭哭泣 😢难过 😤生气 🤗拥抱 🤭偷笑 🤫安静 🥳庆祝 🥹感动 😇天使 🤩崇拜 😋美味 😜调皮 😱惊恐",
  },
  {
    name: "手势",
    items:
      "👍赞 👎不赞同 👏鼓掌 🙌欢呼 🤝握手 🙏感谢 👌好的 ✌️胜利 💪加油 👋再见 🤞祝福 🫶比心 👊碰拳 🤟爱你 🫡敬礼",
  },
  {
    name: "心情",
    items:
      "❤️红心 💚绿心 💙蓝心 💛黄心 💜紫心 🤍白心 💔心碎 💕爱心 💯满分 ✨闪亮 🎉庆祝 🎊彩带 🔥火热 ✅完成 ❓疑问 💡灵感",
  },
  {
    name: "日常",
    items:
      "☀️太阳 🌙月亮 ⭐星星 🌈彩虹 🌸花朵 🌹玫瑰 🍀幸运 🌻向日葵 🐱猫 🐶狗 🐼熊猫 🦊狐狸 ☕咖啡 🍵茶 🍰蛋糕 🍎苹果 🍉西瓜 🍻干杯 🎂生日 🎁礼物 📚读书 🎵音乐 🏃跑步 ✈️旅行",
  },
].map((group) => ({
  name: group.name,
  items: group.items.split(" ").map((item) => {
    const split = item.search(/[\u4e00-\u9fff]/);
    return { emoji: item.slice(0, split), name: item.slice(split) };
  }),
}));

const emojiGroups = [wechatEmojiGroup, ...unicodeEmojiGroups];

export function EmojiPicker({
  onSelect,
  onClose,
}: {
  onSelect: (emoji: string) => void;
  onClose: () => void;
}) {
  const [category, setCategory] = useState("表情");
  const [query, setQuery] = useState("");
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (!root.current?.parentElement?.contains(event.target as Node))
        onClose();
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [onClose]);

  const isWechat = category === "微信" || query.trim().startsWith("[");
  const searchGroups = isWechat ? [wechatEmojiGroup] : unicodeEmojiGroups;
  const targetGroups = query
    ? searchGroups
    : emojiGroups.filter((group) => group.name === category);

  const items = targetGroups
    .flatMap((group) => group.items)
    .filter(
      (item) =>
        !query ||
        item.name.includes(query.trim()) ||
        item.emoji.includes(query.trim()),
    );
  return (
    <div
      ref={root}
      className="emoji-picker"
      role="dialog"
      aria-label="选择表情"
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      <div className="emoji-picker-header">
        <label className="search compact">
          <MagnifyingGlass size={16} />
          <input
            autoFocus
            aria-label="搜索表情"
            placeholder="搜索表情…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        <button
          className="icon-button"
          aria-label="关闭表情选择器"
          onClick={onClose}
        >
          <X size={18} />
        </button>
      </div>
      <div className="emoji-categories">
        {emojiGroups.map((group) => (
          <button
            key={group.name}
            aria-pressed={category === group.name && !query}
            onClick={() => {
              setCategory(group.name);
              setQuery("");
            }}
          >
            {group.name}
          </button>
        ))}
      </div>
      <div className="emoji-grid">
        {items.map((item) => (
          <button
            key={item.emoji}
            aria-label={`表情：${item.name}`}
            title={item.name}
            onClick={() => onSelect(item.emoji)}
          >
            {item.path ? (
              <img
                src={item.path}
                alt={item.name}
                className="wechat-picker-emoji"
                loading="lazy"
              />
            ) : (
              item.emoji
            )}
          </button>
        ))}
      </div>
      {!items.length && <p className="emoji-empty">没有找到匹配的表情</p>}
    </div>
  );
}

export function AttachmentPreview({
  file,
  disabled,
  onRemove,
}: {
  file: File;
  disabled: boolean;
  onRemove: () => void;
}) {
  const [url, setURL] = useState("");
  useEffect(() => {
    setURL("");
    if (!file.type.startsWith("image/") || file.type.includes("svg")) return;
    const value = URL.createObjectURL(file);
    setURL(value);
    return () => URL.revokeObjectURL(value);
  }, [file]);
  return (
    <div className="attachment-preview">
      {url ? <img src={url} alt="待发送图片" /> : <Paperclip size={18} />}
      <span>
        {file.name}
        <small>{Math.max(1, Math.ceil(file.size / 1024))} KB</small>
      </span>
      <button
        disabled={disabled}
        aria-label={`移除附件 ${file.name}`}
        onClick={onRemove}
      >
        <X size={16} />
      </button>
    </div>
  );
}

// 合成与发送分成两步；目标 ID 在打开时固定，切换聊天不会把语音发给其他人。
export function VoiceComposer({
  contact,
  onClose,
  ...props
}: PageProps & { contact: Contact; onClose: () => void }) {
  const { state, refresh, act, notify } = props;
  const [text, setText] = useState("");
  const [taskID, setTaskID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const task = state.tasks.find((item) => item.id === taskID);
  const running =
    busy || task?.status === "queued" || task?.status === "running";
  const candidates = state.models.filter(
    (model) => model.enabled && model.capabilities.includes("tts"),
  );
  const configured = String(state.settings["routing.tts"] || "");
  const model = configured
    ? candidates.find((item) => item.id === configured)
    : candidates[0];
  return (
    <Modal
      open
      onClose={onClose}
      title="发送语音"
      description={`发送给 ${contact.remark || contact.name}；先合成试听，再确认发送。`}
    >
      <form
        className="form-stack"
        onSubmit={async (event) => {
          event.preventDefault();
          if (running || !text.trim()) return;
          setBusy(true);
          setError("");
          try {
            const result = await request<{ id: string }>("/api/tasks", {
              kind: "tts",
              prompt: text.trim(),
              target: contact.id,
            });
            setTaskID(result.id);
            await refresh();
          } catch (error) {
            setError((error as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="hint">
          {state.mock
            ? "脱机演示：使用示例音频，不调用真实模型。"
            : `模型：${model?.name || "尚未配置"} · 音色：${state.settings["tts.voice"] || "默认"}`}
        </div>
        {!model && (
          <p>
            请先在
            <a href="#models" onClick={onClose}>
              模型中心
            </a>
            配置语音合成模型。
          </p>
        )}
        <label>
          语音文字
          <textarea
            rows={4}
            required
            maxLength={Number(state.settings["tts.max_chars"] || 500)}
            value={text}
            onChange={(event) => setText(event.target.value)}
            placeholder="输入希望朗读的内容…"
          />
        </label>
        <button className="button" disabled={running || !text.trim() || !model}>
          {running ? "正在合成…" : "合成试听"}
        </button>
        {task?.status === "completed" && (
          <div className="voice-preview">
            <audio
              controls
              preload="metadata"
              src={task.result}
              aria-label="合成语音试听"
            />
            <p className="muted">合成内容：{task.input}</p>
            <button
              type="button"
              className="button primary"
              disabled={busy || !!task.delivery}
              onClick={async () => {
                setBusy(true);
                setError("");
                try {
                  await act({
                    kind: "send-voice",
                    id: task.id,
                    target: contact.id,
                  });
                  notify("语音已发送");
                  onClose();
                } catch (error) {
                  setError((error as Error).message);
                  await refresh().catch(() => {});
                } finally {
                  setBusy(false);
                }
              }}
            >
              {task.delivery === "sent"
                ? "已发送"
                : task.delivery
                  ? "已提交发送"
                  : "发送语音"}
            </button>
          </div>
        )}
        {(error || task?.error) && (
          <p className="form-error" role="alert">
            {error || task?.error}
          </p>
        )}
        {task?.delivery === "unknown" && (
          <p className="muted">
            发送结果未确认，请先核对聊天记录，避免重复发送。
          </p>
        )}
      </form>
    </Modal>
  );
}

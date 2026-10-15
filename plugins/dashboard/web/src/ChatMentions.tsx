// 文件名：ChatMentions.tsx
// 主要功能：群成员模糊选择、头像右键入口及精确绑定 @ 文本与成员 ID。
// 作者：2guan
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import PinyinMatch from "pinyin-match/src/simplified.js";
import { Avatar } from "./ui";
import type { GroupMember } from "./useGroupAvatars";
import "./chat-mentions.css";
type Span = { start: number; end: number; id: string };
type MentionDraft = { text: string; spans: Span[] };

// 草稿改变时同步清理被编辑或删除的 @ 绑定，发送前也复用这一步避免带入旧成员 ID。
function reconcileDraft(draft: MentionDraft, text: string) {
  if (draft.text === text) return;
  let start = 0;
  while (
    start < draft.text.length &&
    start < text.length &&
    draft.text[start] === text[start]
  )
    start++;
  let oldEnd = draft.text.length,
    newEnd = text.length;
  while (
    oldEnd > start &&
    newEnd > start &&
    draft.text[oldEnd - 1] === text[newEnd - 1]
  ) {
    oldEnd--;
    newEnd--;
  }
  draft.spans = draft.spans.flatMap((span) =>
    span.end <= start
      ? [span]
      : span.start >= oldEnd
        ? [
            {
              ...span,
              start: span.start + newEnd - oldEnd,
              end: span.end + newEnd - oldEnd,
            },
          ]
        : [],
  );
  draft.text = text;
}
export function useChatMentions(
  group: string,
  text: string,
  setText: (s: string) => void,
  input: RefObject<HTMLTextAreaElement | null>,
  members: GroupMember[],
) {
  const drafts = useRef(new Map<string, MentionDraft>());
  const [caret, setCaret] = useState(0);
  const [dismissed, setDismissed] = useState("");
  const [active, setActive] = useState(0);
  const draft = drafts.current.get(group) || { text: "", spans: [] };
  // 只移动编辑区外的绑定；修改/删除 @ 名字立即取消该绑定，避免误提醒同名人员。
  reconcileDraft(draft, text);
  drafts.current.set(group, draft);
  const match = group ? text.slice(0, caret).match(/@([^@\n\u2005]*)$/) : null;
  const start = match ? caret - match[0].length : -1;
  const key = `${group}:${text}:${caret}`;
  const open =
    !!match &&
    dismissed !== key &&
    !draft.spans.some((s) => start >= s.start && start < s.end);
  const query = (match?.[1] || "").trim();
  const options = members
    .filter(
      (m) =>
        !query ||
        `${m.display_name || ""} ${m.remark || ""} ${m.name || ""} ${m.id}`
          .toLowerCase()
          .includes(query.toLowerCase()) ||
        PinyinMatch.match(
          `${m.display_name || ""} ${m.remark || ""} ${m.name || ""}`,
          query,
        ),
    )
    .slice(0, 50);
  const index = Math.min(active, Math.max(0, options.length - 1));
  const pick = (m: GroupMember, replace = true) => {
    const from =
      replace && open ? start : (input.current?.selectionStart ?? text.length);
    const to =
      replace && open ? caret : (input.current?.selectionEnd ?? text.length);
    const label = `@${m.display_name || m.remark || m.name || m.id}`;
    const inserted = label + "\u2005";
    const next = text.slice(0, from) + inserted + text.slice(to);
    const spans = draft.spans.flatMap((s) =>
      s.end <= from
        ? [s]
        : s.start >= to
          ? [
              {
                ...s,
                start: s.start + inserted.length - (to - from),
                end: s.end + inserted.length - (to - from),
              },
            ]
          : [],
    );
    drafts.current.set(group, {
      text: next,
      spans: [...spans, { start: from, end: from + label.length, id: m.id }],
    });
    setText(next);
    setCaret(from + inserted.length);
    setActive(0);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(
        from + inserted.length,
        from + inserted.length,
      );
    });
  };
  return {
    open,
    options,
    index,
    pick,
    idsFor: (content: string) => {
      if (!group) return [];
      const current = drafts.current.get(group);
      if (!current) return [];
      reconcileDraft(current, content);
      // 即使快速替换文本让渲染尚未清掉旧 span，也不能把非 @ 正文绑定到旧成员。
      return [
        ...new Set(
          current.spans
            .filter((span) =>
              content.slice(span.start, span.end).startsWith("@"),
            )
            .map((span) => span.id),
        ),
      ];
    },
    move: (n: number) => {
      if (n !== caret) setActive(0);
      setCaret(n);
      setDismissed("");
    },
    close: () => setDismissed(key),
    keydown: (e: React.KeyboardEvent) => {
      if (!open || e.nativeEvent.isComposing) return false;
      if (e.key === "Escape") {
        setDismissed(key);
        e.preventDefault();
        return true;
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        setActive(
          (index +
            (e.key === "ArrowDown" ? 1 : -1) +
            Math.max(1, options.length)) %
            Math.max(1, options.length),
        );
        e.preventDefault();
        return true;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        if (options[index]) pick(options[index]);
        return true;
      }
      return false;
    },
  };
}
export function MentionList({
  mention,
  loading,
  error,
  retry,
}: {
  mention: ReturnType<typeof useChatMentions>;
  loading: boolean;
  error: string;
  retry: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current
      ?.querySelector('[aria-selected="true"]')
      ?.scrollIntoView({ block: "nearest" });
  }, [mention.index]);
  return (
    <div
      className="chat-mention-list"
      ref={root}
      onMouseDown={(e) => e.preventDefault()}
    >
      <div
        role="listbox"
        id="chat-mention-options"
        aria-label="选择要提醒的群成员"
      >
        {mention.options.map((m, i) => (
          <button
            type="button"
            id={`mention-${i}`}
            role="option"
            aria-selected={i === mention.index}
            key={m.id}
            onClick={() => mention.pick(m)}
          >
            <Avatar src={m.avatar} name={m.name} size="small" />
            <span>
              {m.display_name || m.remark || m.name || m.id}
              <small>
                {m.display_name && m.display_name !== m.name ? m.name : m.id}
              </small>
            </span>
          </button>
        ))}
      </div>
      {loading ? (
        <p role="status">正在获取群成员…</p>
      ) : error ? (
        <p role="alert">
          成员读取失败 <button onClick={retry}>重试</button>
        </p>
      ) : !mention.options.length ? (
        <p>未找到匹配的群成员</p>
      ) : null}
    </div>
  );
}
export function AvatarMentionMenu({
  target,
  allowMention,
  onPick,
  onClose,
}: {
  target: { member: GroupMember; x: number; y: number; trigger: HTMLElement };
  allowMention: boolean;
  onPick: (m: GroupMember) => void;
  onClose: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  useLayoutEffect(() => {
    const node = root.current!;
    const rect = node.getBoundingClientRect();
    node.style.left = `${Math.max(8, Math.min(target.x, window.innerWidth - rect.width - 8))}px`;
    node.style.top = `${Math.max(8, Math.min(target.y, window.innerHeight - rect.height - 8))}px`;
    (node.querySelector<HTMLButtonElement>("button:not(:disabled)") || node).focus();
    const outside = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) close.current();
    };
    document.addEventListener("pointerdown", outside);
    return () => {
      document.removeEventListener("pointerdown", outside);
      if (target.trigger.isConnected) target.trigger.focus();
    };
  }, [target]);
  return createPortal(
    <div
      ref={root}
      className="message-context-menu avatar-context-menu"
      role="menu"
      tabIndex={-1}
      aria-label="人员快捷菜单"
      style={{
        position: "fixed",
        left: Math.max(8, Math.min(target.x, window.innerWidth - 150)),
        top: Math.max(8, Math.min(target.y, window.innerHeight - 64)),
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {allowMention && <button
        role="menuitem"
        onClick={() => {
          onPick(target.member);
          onClose();
        }}
      >
        @ {target.member.display_name || target.member.remark || target.member.name || target.member.id}
      </button>}
      <button role="menuitem" disabled title="当前 core 暂未提供拍一拍发送接口">
        <span>拍一拍</span><small>暂不可用</small>
      </button>
    </div>,
    document.body,
  );
}

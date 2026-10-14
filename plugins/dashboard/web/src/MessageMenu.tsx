// 文件名：MessageMenu.tsx
// 主要功能：提供贴近消息位置的快捷菜单，支持键盘、点击外部关闭和视口避让。
// 作者：2guan
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Copy,
  ArrowCounterClockwise,
  ChatCircleDots,
  Trash,
} from "@phosphor-icons/react";
import type { Message } from "./types";
export type MessageMenuTarget = {
  message: Message;
  x: number;
  y: number;
  trigger: HTMLElement;
};
export function MessageMenu({
  target,
  onClose,
  onAction,
}: {
  target: MessageMenuTarget;
  onClose: () => void;
  onAction: (action: "copy" | "revoke" | "quote" | "delete" | "resend") => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(Date.now());
  const sentAt = Date.parse(target.message.time);
  const canRevoke = target.message.self && target.message.status === "sent" &&
    Number.isFinite(sentAt) && now >= sentAt && now - sentAt <= 120000;
  // 菜单保持打开时也在两分钟边界隐藏撤回，避免一直显示过期入口。
  useEffect(() => {
    const current = Date.now(); setNow(current);
    if (!Number.isFinite(sentAt) || current < sentAt || current - sentAt > 120000) return;
    const timer = window.setTimeout(() => setNow(Date.now()), sentAt + 120001 - current);
    return () => window.clearTimeout(timer);
  }, [target, sentAt]);
  const close = useRef(onClose);
  close.current = onClose;
  useLayoutEffect(() => {
    const el = root.current!;
    const rect = el.getBoundingClientRect();
    el.style.left = `${Math.max(8, Math.min(target.x, window.innerWidth - rect.width - 8))}px`;
    el.style.top = `${Math.max(8, Math.min(target.y, window.innerHeight - rect.height - 8))}px`;
    el.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    const outside = (e: PointerEvent) => {
      if (!(e.target instanceof Node) || !el.contains(e.target)) close.current();
    };
    const move = (e: Event) => {
      if (!(e.target instanceof Node) || !el.contains(e.target)) close.current();
    };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", move);
    window.addEventListener("scroll", move, true);
    return () => {
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", move);
      window.removeEventListener("scroll", move, true);
    };
  }, [target]);
  // 禁用不适用的操作；撤回的最终权限和回执仍由服务端判断。
  const items = [
    {
      id: "copy",
      label: "复制",
      icon: Copy,
      disabled: !target.message.content,
    },
    {
      id: "revoke",
      label: "撤回",
      icon: ArrowCounterClockwise,
      disabled: !target.message.self || target.message.status !== "sent",
    },
    {
      id: "quote",
      label: "引用",
      icon: ChatCircleDots,
      disabled: ["sending", "unknown", "revoked"].includes(
        target.message.status,
      ),
    },
    {
      id: "delete",
      label: "删除",
      icon: Trash,
      disabled: target.message.status === "sending",
    },
  ] as const;
  return createPortal(
    <div
      ref={root}
      className="message-context-menu"
      role="menu"
      aria-label="消息快捷菜单"
      onContextMenu={(e) => e.preventDefault()}
      onKeyDown={(e) => {
        const buttons = Array.from(
          root.current!.querySelectorAll<HTMLButtonElement>(
            "button:not(:disabled)",
          ),
        );
        const index = buttons.indexOf(
          document.activeElement as HTMLButtonElement,
        );
        if (["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) {
          e.preventDefault();
          buttons[
            e.key === "Home"
              ? 0
              : e.key === "End"
                ? buttons.length - 1
                : (index + (e.key === "ArrowDown" ? 1 : -1) + buttons.length) %
                  buttons.length
          ]?.focus();
        } else if (e.key === "Escape" || e.key === "Tab") {
          e.preventDefault();
          onClose();
          target.trigger.focus();
        }
      }}
    >
      {target.message.self && ["unknown", "failed"].includes(target.message.status) && (
        <button role="menuitem" onClick={() => onAction("resend")}><ArrowCounterClockwise size={16}/><span>重发</span></button>
      )}
      {items.filter(item => item.id !== "revoke" || canRevoke).map(({ id, label, icon: Icon, disabled }) => (
        <button
          key={id}
          role="menuitem"
          disabled={disabled}
          title={id === "delete" ? "仅删除后台中的消息记录" : undefined}
          onClick={() => onAction(id)}
        >
          <Icon size={16} />
          <span>{label}</span>
        </button>
      ))}
    </div>,
    document.body,
  );
}

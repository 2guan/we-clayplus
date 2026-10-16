// 文件名：ui.tsx
// 主要功能：提供统一的可访问弹窗、状态标签、头像、空状态与设置表单。
// 作者：2guan
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  X,
  Check,
  ArrowCounterClockwise,
  Tray,
  CircleNotch,
} from "@phosphor-icons/react";
import type { Group, PageProps } from "./types";

export const AvatarCacheContext = createContext("");

export function Avatar({
  name,
  size = "",
  group = false,
  blank = false,
  src,
  priority = false,
}: {
  name: string;
  size?: string;
  group?: boolean;
  blank?: boolean;
  src?: string;
  priority?: boolean;
}) {
  const photo = /^https?:\/\//.test(src || "") ? src! : (/^https?:\/\//.test(name) ? name : "");
  const version=useContext(AvatarCacheContext);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [photo,version]);
  const colors = ["sage", "sand", "sky", "rose", "ink", "lavender"];
  const index =
    [...name].reduce((v, c) => v + c.charCodeAt(0), 0) % colors.length;
  return (
    <span
      className={`avatar ${size} ${colors[index]} ${group ? "group-avatar" : ""} ${blank ? "blank-avatar" : ""}`}
      aria-hidden="true"
    >
      {blank ? null : photo && !failed ? (
        <img loading={priority ? "eager" : "lazy"} fetchPriority={priority ? "high" : "auto"} src={`/api/wechat-image?url=${encodeURIComponent(photo)}&v=${encodeURIComponent(version)}`} alt="" referrerPolicy="no-referrer" onError={() => setFailed(true)} />
      ) : group ? (
        <span className="mini-avatars">
          {["山", "林", "禾", "序"].map((n) => (
            <i key={n}>{n}</i>
          ))}
        </span>
      ) : (
        [...name][0] || "友"
      )}
    </span>
  );
}
export function Badge({
  children,
  tone = "",
}: {
  children: ReactNode;
  tone?: string;
}) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
export function Empty({
  title = "这里还没有内容",
  text = "数据准备好后会显示在这里。",
  action,
}: {
  title?: string;
  text?: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <span className="empty-icon">
        <Tray size={30} weight="light" />
      </span>
      <h3>{title}</h3>
      <p>{text}</p>
      {action}
    </div>
  );
}
export function Busy() {
  return (
    <span className="busy">
      <CircleNotch className="spin" size={16} /> 正在处理…
    </span>
  );
}
export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  wide = false,
  initialFocus,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: ReactNode;
  wide?: boolean;
  initialFocus?: () => void;
}) {
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className={`dialog ${wide ? "wide" : ""}`} onOpenAutoFocus={(event) => {
          // 输入型弹窗可指定初始焦点，其他弹窗继续使用默认的可访问焦点管理。
          if (initialFocus) { event.preventDefault(); initialFocus(); }
        }}>
          <div className="dialog-heading">
            <div>
              <Dialog.Title>{title}</Dialog.Title>
              <Dialog.Description>
                {description || "修改后请保存，取消不会改变已有配置。"}
              </Dialog.Description>
            </div>
            <Dialog.Close className="icon-button" aria-label="关闭">
              <X size={20} />
            </Dialog.Close>
          </div>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
export function Tabs({
  items,
  value,
  onChange,
}: {
  items: [string, string][];
  value: string;
  onChange: (id: string) => void;
}) {
  return (
    <div className="tabs" aria-label="页面分组">
      {items.map(([id, label]) => (
        <button
          type="button"
          key={id}
          className={id === value ? "active" : ""}
          aria-pressed={id === value}
          onClick={() => onChange(id)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
export function Toggle({
  checked,
  onChange,
  label,
  disabled = false,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={`toggle ${checked ? "on" : ""}`}
      onClick={() => onChange(!checked)}
    >
      <span />
    </button>
  );
}

// 表单草稿和服务器状态分离：SSE 刷新不覆盖正在编辑的字段。
export function SettingsForm({
  group,
  state,
  user,
  act,
  notify,
}: PageProps & { group: Group }) {
  const saved = () =>
    Object.fromEntries(
      group.fields.map((f) => [
        f.key,
        state.settings[`${group.id}.${f.key}`] ?? f.default,
      ]),
    );
  const [values, setValues] = useState(saved);
  const [revision, setRevision] = useState(state.config_revision);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!dirty) {
      setValues(saved());
      setRevision(state.config_revision);
    }
  }, [state.revision, group.id, dirty]);
  // 捕获站内跳转，避免修改到一半切换页面时静默丢弃草稿。
  useEffect(() => {
    const guard = (e: MouseEvent) => {
      const target = e.target as Element;
      if (
        dirty &&
        target.closest('a[href^="#"]') &&
        !confirm("有尚未保存的设置，确定离开？")
      ) {
        e.preventDefault();
        e.stopPropagation();
      }
    };
    document.addEventListener("click", guard, true);
    return () => document.removeEventListener("click", guard, true);
  }, [dirty]);
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) e.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  const readonly = group.admin && user.role !== "admin";
  return (
    <form
      className="settings-card"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try {
          await act({ kind: "settings", group: group.id, values, revision });
          setDirty(false);
          notify("设置已保存");
        } catch (e) {
          setError((e as Error).message);
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="section-heading">
        <div>
          <h3>{group.title}</h3>
          <p>{group.description}</p>
        </div>
        <Badge>{group.effect}</Badge>
      </div>
      <div className="fields">
        {group.fields.filter((f) => group.id !== "reply" ||
          (f.key !== "targets" || values.list_mode === "白名单") &&
          (f.key !== "blacklist" || values.list_mode !== "白名单")).map((f) => (
          <div
            className={`field-row ${f.type === "boolean" ? "switch-row" : ""}`}
            key={f.key}
          >
            <label htmlFor={`${group.id}-${f.key}`}>
              <strong>
                {f.label} {f.preview && <Badge>仅预览</Badge>}
              </strong>
              <small>
                {f.help}
                {f.preview && " 此版本可预览和保存，不执行对应自动化。"}
              </small>
            </label>
            <div className="field-input">
              {f.type === "boolean" ? (
                <Toggle
                  checked={Boolean(values[f.key])}
                  disabled={readonly || (!state.mock && f.preview)}
                  label={f.label}
                  onChange={(v) => {
                    setValues({ ...values, [f.key]: v });
                    setDirty(true);
                  }}
                />
              ) : f.type === "select" ? (
                <select
                  id={`${group.id}-${f.key}`}
                  aria-label={f.label}
                  value={String(values[f.key])}
                  disabled={readonly || (!state.mock && f.preview)}
                  onChange={(e) => {
                    setValues({ ...values, [f.key]: e.target.value });
                    setDirty(true);
                  }}
                >
                  {/* 默认模式仅调整显示名称，保留已保存的配置值兼容旧版本。 */}
                  {f.options?.map((o) => (
                    <option key={o} value={o}>{group.id === "reply" && ["default_friend", "default_group"].includes(f.key) && o === "人工" ? "手动" : o}</option>
                  ))}
                </select>
              ) : (
                <input
                  id={`${group.id}-${f.key}`}
                  aria-label={f.label}
                  type={f.type === "number" ? "number" : "text"}
                  step={f.key === "speed" ? 0.05 : 1}
                  min={f.min}
                  max={f.max}
                  value={String(values[f.key])}
                  disabled={readonly || (!state.mock && f.preview)}
                  onChange={(e) => {
                    setValues({
                      ...values,
                      [f.key]:
                        f.type === "number"
                          ? Number(e.target.value)
                          : e.target.value,
                    });
                    setDirty(true);
                  }}
                />
              )}
            </div>
          </div>
        ))}
      </div>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <div className="form-footer">
        <span>
          {readonly
            ? "此配置仅管理员可修改"
            : dirty
              ? "有尚未保存的更改"
              : "已与服务器同步"}
        </span>
        <div className="actions">
          <button
            type="button"
            className="button ghost"
            disabled={readonly || busy}
            onClick={() => {
              setValues(
                Object.fromEntries(group.fields.map((f) => [f.key, f.default])),
              );
              setDirty(true);
            }}
          >
            <ArrowCounterClockwise size={15} />
            恢复默认
          </button>
          <button
            className="button primary"
            disabled={readonly || busy || !dirty}
          >
            {busy ? (
              <Busy />
            ) : (
              <>
                <Check size={16} />
                保存设置
              </>
            )}
          </button>
        </div>
      </div>
    </form>
  );
}

// 多栏编辑器保留本地草稿；切换页签、对象或离开页面前提醒，避免静默丢失修改。
export function useUnsavedDraft(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const leave = (event: BeforeUnloadEvent) => event.preventDefault();
    const navigate = (event: MouseEvent) => {
      const element = event.target as Element;
      if (
        element.closest(
          'a[href^="#"], .tabs button, .persona-select, .behavior-item',
        ) &&
        !confirm("有尚未保存的修改，确定离开？")
      ) {
        event.preventDefault();
        event.stopPropagation();
      }
    };
    window.addEventListener("beforeunload", leave);
    document.addEventListener("click", navigate, true);
    return () => {
      window.removeEventListener("beforeunload", leave);
      document.removeEventListener("click", navigate, true);
    };
  }, [dirty]);
}

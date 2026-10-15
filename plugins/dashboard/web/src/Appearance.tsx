// 文件名：Appearance.tsx
// 主要功能：管理五种配色、亮暗与跟随系统模式，持久化本机偏好并提供统一设置入口。
// 作者：2guan
import { useEffect, useId, useRef, useState } from "react";
import { Check, Moon, Sun, Desktop, Palette } from "@phosphor-icons/react";

const colors = [
  ["green", "绿色", "#07834d"],
  ["blue", "蓝色", "#2563c5"],
  ["red", "红色", "#c33d43"],
  ["orange", "橙色", "#b65b0b"],
  ["gray", "灰色", "#62666d"],
] as const;
type Color = (typeof colors)[number][0];
type Mode = "light" | "dark" | "system";

// 无效或无法读取的本机偏好回退默认值，不阻塞登录和其他页面。
function readPreference(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
function initialColor(): Color {
  const saved = readPreference("weclay-color");
  return colors.find(([id]) => id === saved)?.[0] || "green";
}
function initialMode(): Mode {
  const saved = readPreference("weclay-theme");
  return saved === "dark" || saved === "system" ? saved : "light";
}
function applyAppearance(color: Color, mode: Mode) {
  const root = document.documentElement;
  const dark =
    mode === "dark" ||
    (mode === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  root.dataset.color = color;
  root.dataset.theme = dark ? "dark" : "light";
  root.style.colorScheme = dark ? "dark" : "light";
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute(
      "content",
      getComputedStyle(root).getPropertyValue("--bg").trim(),
    );
}

// 首次渲染前应用偏好，避免刷新暗色页面时先闪出明亮背景。
applyAppearance(initialColor(), initialMode());

export function useAppearance() {
  const [color, setColor] = useState<Color>(initialColor);
  const [mode, setMode] = useState<Mode>(initialMode);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => applyAppearance(color, mode);
    apply();
    media.addEventListener("change", apply);
    try {
      localStorage.setItem("weclay-color", color);
      localStorage.setItem("weclay-theme", mode);
    } catch {
      /* 浏览器禁用存储时，本次页面内仍可切换外观。 */
    }
    return () => media.removeEventListener("change", apply);
  }, [color, mode]);
  return { color, setColor, mode, setMode };
}

export function AppearancePicker({
  color,
  setColor,
  mode,
  setMode,
}: ReturnType<typeof useAppearance>) {
  // 仅在主题按钮下展开小浮层；选择、点击外部或按 Escape 都会关闭。
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const panelID = useId();
  useEffect(() => {
    if (!open) return;
    panel.current
      ?.querySelector<HTMLButtonElement>('[aria-pressed="true"]')
      ?.focus();
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        trigger.current?.focus();
      }
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);
  const modeName =
    mode === "light" ? "明亮" : mode === "dark" ? "暗色" : "跟随系统";
  return (
    <div className="appearance-controls" role="group" aria-label="主题外观">
      <div
        className="palette-anchor"
        ref={root}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null))
            setOpen(false);
        }}
      >
        <button
          ref={trigger}
          type="button"
          className="icon-button"
          aria-label="选择主题颜色"
          title="选择主题颜色"
          aria-expanded={open}
          aria-controls={panelID}
          onClick={() => setOpen(!open)}
        >
          <Palette size={20} />
        </button>
        {open && (
          <div
            ref={panel}
            id={panelID}
            className="palette-options"
            role="group"
            aria-label="主题配色"
          >
            {colors.map(([id, label, swatch]) => (
              <button
                key={id}
                type="button"
                className="palette-option"
                aria-label={label}
                title={label}
                aria-pressed={color === id}
                onClick={() => {
                  setColor(id);
                  setOpen(false);
                  trigger.current?.focus();
                }}
              >
                <span className="palette-swatch" style={{ background: swatch }}>
                  {color === id && <Check size={12} weight="bold" />}
                </span>
              </button>
            ))}
          </div>
        )}
      </div>
      <button
        type="button"
        className="icon-button appearance-mode"
        aria-label={`切换主题，当前${modeName}`}
        title={`当前${modeName}，点击切换显示模式`}
        onClick={() =>
          setMode(
            mode === "light" ? "dark" : mode === "dark" ? "system" : "light",
          )
        }
      >
        {mode === "light" ? (
          <Moon size={20} />
        ) : mode === "dark" ? (
          <Sun size={20} />
        ) : (
          <Desktop size={20} />
        )}
      </button>
    </div>
  );
}

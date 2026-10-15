// 文件名：PluginConfiguration.tsx
// 主要功能：使用同一份草稿提供插件可视化表单和 TOML 文本编辑，保留校验错误及并发版本。
// 作者：2guan
import { useEffect, useState } from "react";
import { ArrowClockwise, DownloadSimple } from "@phosphor-icons/react";
import type { Field, Group, PageProps, Plugin } from "./types";
import { Modal, Tabs, Toggle } from "./ui";
import { download, request } from "./api";

type Values = Record<string, unknown>;
type Document = {
  id: string;
  text: string;
  values: Values;
  revision: string;
  source: string;
  effect: string;
  groups: Group[];
};
const hostFields: Field[] = [
  { key: "enable", label: "启用插件", type: "boolean" },
  {
    key: "priority",
    label: "执行优先级",
    type: "number",
    help: "数值越小越先执行。",
  },
  { key: "next", label: "继续后续插件", type: "boolean" },
  { key: "always_run", label: "忽略会话劫持", type: "boolean" },
  {
    key: "mode",
    label: "名单模式",
    type: "select",
    options: ["blacklist", "whitelist"],
  },
  {
    key: "limits",
    label: "生效名单",
    help: '填写联系人或群聊 ID 的 JSON 数组，例如 ["群ID"]。',
  },
].map((f) => ({ preview: false, default: "", help: "", type: "text", ...f }));

// 通用扩展按现有值类型生成控件；复杂数组使用 JSON 编辑，未知字段通过 TOML 添加后也能回到表单。
function Fields({
  values,
  fields = [],
  path = [],
  onChange,
  arrays,
  setArray,
  mock,
}: {
  values: Values;
  fields?: Field[];
  path?: string[];
  onChange: (value: Values) => void;
  arrays: Record<string, string>;
  setArray: (path: string, value: string) => void;
  mock: boolean;
}) {
  return (
    <div className="plugin-config-fields">
      {Object.entries(values)
        .sort(([a], [b]) => {
          const order = (key: string) => {
            const index = fields.findIndex((field) => field.key === key);
            return index < 0 ? fields.length : index;
          };
          return order(a) - order(b);
        })
        .map(([key, value]) => {
          const field = fields.find((f) => f.key === key);
          const parts = [...path, key];
          const id = `plugin-field-${JSON.stringify(parts)}`;
          const commonLabels: Record<string, string> = {
            message: "消息内容",
            mention: "提及成员",
            delay_seconds: "延迟秒数",
            max_messages: "消息数量",
            title: "标题",
            time: "执行时间",
          };
          const label = field?.label || commonLabels[key] || key;
          const set = (next: unknown) => onChange({ ...values, [key]: next });
          if (
            value !== null &&
            typeof value === "object" &&
            !Array.isArray(value)
          )
            return (
              <fieldset className="plugin-config-group" key={key}>
                <legend>{key === "config" ? "插件专属设置" : label}</legend>
                <Fields
                  values={value as Values}
                  path={parts}
                  onChange={set}
                  arrays={arrays}
                  setArray={setArray}
                  mock={mock}
                />
              </fieldset>
            );
          const disabled = !mock && !!field?.preview;
          return (
            <div
              className={`plugin-config-field ${typeof value === "boolean" ? "is-switch" : ""}`}
              key={key}
            >
              <label htmlFor={id}>
                {label}
                {field?.preview && <small>仅预览</small>}
              </label>
              {field?.help && <p>{field.help}</p>}
              {Array.isArray(value) ? (
                <textarea
                  id={id}
                  rows={3}
                  spellCheck={false}
                  disabled={disabled}
                  value={
                    arrays[JSON.stringify(parts)] ??
                    JSON.stringify(value, null, 2)
                  }
                  onChange={(e) =>
                    setArray(JSON.stringify(parts), e.target.value)
                  }
                />
              ) : field?.type === "textarea" ? (
                <textarea
                  id={id}
                  rows={4}
                  disabled={disabled}
                  value={String(value)}
                  onChange={(e) => set(e.target.value)}
                />
              ) : typeof value === "boolean" ? (
                <Toggle
                  label={label}
                  checked={value}
                  disabled={disabled}
                  onChange={set}
                />
              ) : field?.type === "select" ? (
                <select
                  id={id}
                  value={String(value)}
                  disabled={disabled}
                  onChange={(e) => set(e.target.value)}
                >
                  {field.options?.map((option) => (
                    <option key={option} value={option}>
                      {option === "blacklist"
                        ? "黑名单（排除名单内对象）"
                        : option === "whitelist"
                          ? "白名单（仅名单内对象）"
                          : (
                              {
                                observe: "仅观察",
                                standard: "标准防护",
                                strict: "严格防护",
                                silent: "静默跳过",
                                notice: "发送固定说明",
                              } as Record<string, string>
                            )[option] || option}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  id={id}
                  disabled={disabled}
                  autoComplete="off"
                  spellCheck={false}
                  type={
                    typeof value === "number"
                      ? "number"
                      : /password|secret|token|api.?key/i.test(key)
                        ? "password"
                        : "text"
                  }
                  step="any"
                  min={field?.min}
                  max={field?.max}
                  value={String(value ?? "")}
                  onChange={(e) =>
                    set(
                      typeof value === "number"
                        ? Number(e.target.value)
                        : e.target.value,
                    )
                  }
                />
              )}
            </div>
          );
        })}
    </div>
  );
}

export function PluginConfiguration({
  plugin,
  state,
  refresh,
  notify,
  onClose,
}: PageProps & { plugin: Plugin; onClose: () => void }) {
  const [doc, setDoc] = useState<Document>();
  const [values, setValues] = useState<Values>({});
  const [text, setText] = useState("");
  const [mode, setMode] = useState("visual");
  const [group, setGroup] = useState("");
  const [arrays, setArrays] = useState<Record<string, string>>({});
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const apply = (next: Document) => {
    setDoc(next);
    setValues(next.values);
    setText(next.text);
    setArrays({});
  };
  const load = async () => {
    setBusy(true);
    setError("");
    try {
      const next = await request<Document>(
        `/api/plugin-config?id=${encodeURIComponent(plugin.id)}`,
      );
      apply(next);
      setGroup(next.groups[0]?.id || "");
      setDirty(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    void load();
  }, [plugin.id]);
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) e.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  const close = () => {
    if (!busy && (!dirty || confirm("配置尚未保存，确定放弃修改？"))) onClose();
  };
  const draftValues = () => {
    const next = structuredClone(values);
    for (const [path, raw] of Object.entries(arrays)) {
      const parts = JSON.parse(path) as string[];
      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        throw new Error(`${parts.join(".")} 必须是有效的 JSON 数组`);
      }
      if (!Array.isArray(parsed))
        throw new Error(`${parts.join(".")} 必须是数组`);
      let parent = next;
      for (const part of parts.slice(0, -1)) parent = parent[part] as Values;
      parent[parts.at(-1)!] = parsed;
    }
    return next;
  };
  // 切换编辑方式只转换草稿；绝不把服务器最新版本号当成此草稿的编辑基线。
  const submit = async (save: boolean, nextMode?: string) => {
    if (!doc || busy) return;
    setError("");
    setBusy(true);
    try {
      const next = await request<Document>("/api/plugin-config", {
        id: plugin.id,
        revision: doc.revision,
        save,
        ...(mode === "text" ? { text } : { values: draftValues() }),
      });
      apply(save ? next : { ...next, revision: doc.revision });
      if (nextMode) setMode(nextMode);
      if (save) {
        setDirty(false);
        await refresh();
        notify("插件配置已保存");
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const active = doc?.groups.find((g) => g.id === group);
  return (
    <Modal
      open
      onClose={close}
      title={`${plugin.name}配置`}
      wide
      description="两种方式编辑同一份配置，修改后请保存。"
    >
      <form
        className="plugin-config-editor"
        onSubmit={(e) => {
          e.preventDefault();
          void submit(true);
        }}
      >
        {doc && (
          <>
            <div className="plugin-config-toolbar">
              <Tabs
                value={mode}
                onChange={(next) => {
                  if (next !== mode) void submit(false, next);
                }}
                items={[
                  ["visual", "可视化"],
                  ["text", "配置文本"],
                ]}
              />
              <div className="actions">
                <button
                  type="button"
                  className="icon-button"
                  aria-label="重新读取配置"
                  title="重新读取配置"
                  disabled={busy}
                  onClick={() => {
                    if (
                      !dirty ||
                      confirm("重新读取将丢弃未保存的修改，是否继续？")
                    )
                      void load();
                  }}
                >
                  <ArrowClockwise size={18} />
                </button>
                <button
                  type="button"
                  className="icon-button"
                  aria-label="导出已保存配置"
                  title="导出已保存配置"
                  disabled={busy}
                  onClick={async () => {
                    try {
                      const saved = await request<Document>(
                        `/api/plugin-config?id=${encodeURIComponent(plugin.id)}`,
                      );
                      download(`${plugin.id}.toml`, saved.text);
                    } catch (e) {
                      setError((e as Error).message);
                    }
                  }}
                >
                  <DownloadSimple size={18} />
                </button>
              </div>
            </div>
            <fieldset disabled={busy} className="plugin-config-content">
              {mode === "text" ? (
                <label className="plugin-config-code-label">
                  TOML 配置
                  <textarea
                    className="plugin-config-code"
                    aria-label="TOML 配置"
                    rows={18}
                    value={text}
                    spellCheck={false}
                    onChange={(e) => {
                      setText(e.target.value);
                      setDirty(true);
                    }}
                  />
                </label>
              ) : (
                <>
                  {!!doc.groups.length && (
                    <label className="plugin-config-section">
                      配置分组
                      <select
                        value={group}
                        onChange={(e) => setGroup(e.target.value)}
                      >
                        {doc.groups.map((g) => (
                          <option key={g.id} value={g.id}>
                            {g.title}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  <Fields
                    values={
                      active ? ((values[active.id] || {}) as Values) : values
                    }
                    fields={active?.fields || hostFields}
                    path={active ? [active.id] : []}
                    arrays={arrays}
                    mock={state.mock}
                    setArray={(path, value) => {
                      setArrays((old) => ({ ...old, [path]: value }));
                      setDirty(true);
                    }}
                    onChange={(next) => {
                      setValues(
                        active ? { ...values, [active.id]: next } : next,
                      );
                      setDirty(true);
                    }}
                  />
                </>
              )}
            </fieldset>
            <p className="plugin-config-source">
              {doc.source}
              <br />
              {doc.effect}
            </p>
          </>
        )}
        {!doc && busy && <p className="empty">正在读取配置…</p>}
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="plugin-config-footer">
          <span>{dirty ? "有未保存的修改" : ""}</span>
          <button
            type="button"
            className="button"
            onClick={doc ? close : () => void load()}
            disabled={busy}
          >
            {doc ? "取消" : "重新读取"}
          </button>
          <button
            type="submit"
            className="button primary"
            disabled={!doc || busy}
          >
            {busy ? "处理中…" : "保存配置"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

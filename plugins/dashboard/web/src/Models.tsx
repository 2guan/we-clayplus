// 文件名：Models.tsx
// 主要功能：呈现模型、提示词、全局对话与行为页签，管理厂商和调用流水线。
// 作者：2guan
import { vendorPresets } from "./vendorPresets";
import { useState } from "react";
import {
  Plus,
  Cpu,
  PencilSimple,
  Trash,
  ArrowUp,
  ArrowDown,
  Play,
  X,
  ArrowRight,
  SlidersHorizontal,
} from "@phosphor-icons/react";
import {
  type Model,
  type Vendor,
  type PageProps,
  abilities,
  modelPipeline,
} from "./types";
import {
  Badge,
  Modal,
  Tabs,
  SettingsForm,
  Empty,
  Toggle,
  useUnsavedDraft,
} from "./ui";
import { request } from "./api";
import { TaskBadge, TaskList } from "./ModelTasks";
import { ModelPlayground, PersonaEditor, BehaviorEditor } from "./ModelStudio";
import "./models.css";
export { TaskList } from "./ModelTasks";

const tabs: [string, string][] = [
  ["chat", "对话模型"],
  ["tts", "语音模型"],
  ["image", "图片模型"],
  ["video", "视频模型"],
  ["prompts", "提示词管理"],
  ["global", "全局对话设置"],
  ["actions", "行为与任务"],
];
const blankVendor: Vendor = {
  id: "",
  name: "",
  provider: "openai",
  endpoint: "",
  configured: false,
};
export default function Models(props: PageProps) {
  const { state, user, notify } = props;
  const params = new URLSearchParams(location.hash.split("?")[1]);
  const initial = params.get("settings") === "reply" ? "global" : params.get("tab") || "chat";
  const [tab, setTab] = useState(
    tabs.some(([id]) => id === initial) ? initial : "chat",
  );
  const [vendor, setVendor] = useState<Vendor | null>(null);
  const [model, setModel] = useState<Model | null>(null);
  const [selectedModel, setSelectedModel] = useState<Model | undefined>();
  const [probe, setProbe] = useState<Record<string, string>>({});
  const admin = user.role === "admin";
  const changeTab = (id: string) => {
    setSelectedModel(undefined);
    setTab(id);
    history.replaceState(null, "", `#models?tab=${id}`);
  };
  const capability = tab === "video" ? "video_generation" : tab;
  const visible = state.models.filter((m) =>
    tab === "chat"
      ? m.capabilities.some((c) => ["chat", "vision", "audio"].includes(c))
      : m.capabilities.includes(capability),
  );
  const vendors = (state.vendors || []).filter(
    (v) =>
      visible.some((m) => m.vendor_id === v.id) ||
      !state.models.some((m) => m.vendor_id === v.id),
  );
  const test = async (m: Model) => {
    try {
      const t = await request<{ id: string }>("/api/tasks", {
        kind: "chat",
        model: m.id,
        prompt: "请仅回复 OK。",
      });
      setProbe({ ...probe, [m.id]: t.id });
      await props.refresh();
    } catch (e) {
      notify((e as Error).message);
    }
  };
  return (
    <div className="page model-center">
      <div className="page-toolbar">
        <h1 className="page-title">模型中心</h1>
        {["chat", "tts", "image", "video"].includes(tab) && (
          <div className="actions">
            <button
              className="button primary"
              disabled={!admin}
              onClick={() => setVendor({ ...blankVendor })}
            >
              <Plus size={17} />
              添加厂商
            </button>
          </div>
        )}
      </div>
      <Tabs items={tabs} value={tab} onChange={changeTab} />
      {["chat", "tts", "image", "video"].includes(tab) && (
        <>
          <Pipeline
            key={`pipeline-${capability}`}
            {...props}
            kind={capability}
          />
          <div className="section-caption">
            <span>
              模型厂商 <Badge>{vendors.length}</Badge>
            </span>
            <span className="muted">
              {state.mock
                ? "脱机模拟 · 不连接外部服务"
                : "密钥加密保存 · 不返回浏览器"}
            </span>
          </div>
          <div className="vendor-grid">
            {vendors.map((v) => (
              <section className="vendor-card" key={v.id}>
                <header>
                  <span className="vendor-symbol">
                    <Cpu size={22} />
                  </span>
                  <div>
                    <h3>{v.name}</h3>
                    <small>
                      {v.provider === "mock"
                        ? "Mock"
                        : v.provider.toUpperCase()}
                      <span className="vendor-endpoint">
                        {v.endpoint || "本机离线"}
                      </span>
                    </small>
                  </div>
                  <button
                    className="icon-button"
                    aria-label={`管理厂商 ${v.name}`}
                    disabled={!admin}
                    onClick={() => setVendor(v)}
                  >
                    <PencilSimple size={18} />
                  </button>
                </header>
                <div className="vendor-models">
                  {visible
                    .filter((m) => m.vendor_id === v.id)
                    .map((m) => {
                      const task = state.tasks.find(
                        (t) => t.id === probe[m.id],
                      );
                      return (
                        <div className="vendor-model" key={m.id}>
                          <div className="model-name">
                            <strong>{m.name}</strong>
                            <code>{m.model}</code>
                            <div>
                              {m.capabilities.map((c) => (
                                <Badge key={c}>{abilities[c]}</Badge>
                              ))}
                              {/* 在卡片直接显示推理设置，与模型独立参数保持一致。 */}
                              {m.capabilities.some((c) => ["chat", "vision", "audio"].includes(c)) && (
                                <Badge>{m.thinking === "on" ? "思维链开启" : m.thinking === "off" ? "思维链关闭" : "思维链默认"}</Badge>
                              )}
                              {!m.enabled && <Badge>停用</Badge>}
                            </div>
                          </div>
                          <div className="model-controls">
                            {task && <TaskBadge task={task} />}
                            <button
                              className="icon-button"
                              title={task?.error}
                              aria-label={`测活 ${m.name}`}
                              disabled={
                                !m.enabled ||
                                task?.status === "running" ||
                                task?.status === "queued"
                              }
                              onClick={() =>
                                tab === "chat" &&
                                m.capabilities.includes("chat")
                                  ? void test(m)
                                  : (setSelectedModel(m),
                                    document
                                      .querySelector(".studio-heading")
                                      ?.scrollIntoView({
                                        behavior: "smooth",
                                        block: "start",
                                      }))
                              }
                            >
                              <Play size={16} />
                            </button>
                            <button
                              className="icon-button"
                              aria-label={`配置 ${m.name}`}
                              disabled={!admin}
                              onClick={() => setModel(m)}
                            >
                              <SlidersHorizontal size={18} />
                            </button>
                          </div>
                        </div>
                      );
                    })}
                </div>
                <footer>
                  <span className="muted">
                    {v.configured ? "凭据已配置" : "待配置凭据"}
                  </span>
                  <button
                    className="text-link"
                    disabled={!admin}
                    onClick={() => setVendor(v)}
                  >
                    <Plus size={14} />
                    获取 / 添加模型
                  </button>
                </footer>
              </section>
            ))}
          </div>
          {!vendors.length && (
            <Empty
              title="添加模型厂商"
              text="填写 API 地址与密钥，获取模型后选择所需能力。"
            />
          )}
          <ModelPlayground
            key={`studio-${tab}`}
            {...props}
            kind={capability}
            selectedModel={selectedModel}
          />
        </>
      )}
      {tab === "prompts" && <PersonaEditor {...props} />}
      {tab === "global" && (
            <div className="model-global">
              {props.schema
                .filter((g) => ["reply", "memory", "routing"].includes(g.id))
                .map((g) => (
                  <SettingsForm
                    key={g.id}
                    {...props}
                    group={{
                      ...g,
                      fields: g.fields.filter(
                        (f) =>
                          !f.preview &&
                          ![
                            "chat",
                            "video",
                            "video_generation",
                            "tts",
                            "image",
                          ].includes(f.key),
                      ),
                    }}
                  />
                ))}
            </div>
      )}
      {tab === "actions" && (
        <>
          <BehaviorEditor {...props} />
          <TaskList {...props} />
        </>
      )}
      <Modal
        open={!!vendor}
        onClose={() => setVendor(null)}
        title={vendor?.id ? "管理模型厂商" : "添加模型厂商"}
        wide
      >
        {vendor && (
          <VendorForm
            key={vendor.id}
            initial={vendor}
            capability={capability}
            {...props}
            onClose={() => setVendor(null)}
          />
        )}
      </Modal>
      <Modal open={!!model} onClose={() => setModel(null)} title="模型参数">
        {model && (
          <ModelSettings
            key={model.id}
            model={model}
            {...props}
            onClose={() => setModel(null)}
          />
        )}
      </Modal>
    </div>
  );
}

// 顺序通过上移/下移和删除明确表达，不依赖精细拖拽，手机和键盘也能完成编排。
function Pipeline({
  state,
  act,
  notify,
  user,
  kind,
}: PageProps & { kind: string }) {
  const saved = modelPipeline(state, kind);
  const [draft, setDraft] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = draft ?? saved;
  useUnsavedDraft(
    draft !== null && JSON.stringify(draft) !== JSON.stringify(saved),
  );
  const save = async () => {
    setBusy(true);
    try {
      await act({ kind: "pipeline", group: kind, pipeline: ids });
      setDraft(null);
      notify("流水线已保存");
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const move = (i: number, delta: number) => {
    const next = [...ids];
    [next[i], next[i + delta]] = [next[i + delta], next[i]];
    setDraft(next);
  };
  return (
    <section className="panel pipeline-panel">
      <div className="section-heading">
        <div>
          <h3>模型调用流水线</h3>
          <p>
            {kind === "tts"
              ? "按顺序尝试同一语音模式，失败后切换，成功即停止。"
              : "从左到右依次尝试，失败或超时后切换，成功即停止。"}
            {kind === "video_generation" && " 已受理但结果未知时停止切换。"}
          </p>
        </div>
        <Badge>
          {state.settings["routing.fallback"] ? "故障切换" : "自动切换已关闭"}
        </Badge>
      </div>
      <div className="pipeline-track">
        {ids.map((id, i) => {
          const m = state.models.find((m) => m.id === id);
          return (
            <div className="pipeline-step" key={id}>
              <span className="step-number">
                {String(i + 1).padStart(2, "0")}
              </span>
              <div>
                <strong>{m?.name || "模型已移除"}</strong>
                <small>
                  {m?.timeout || state.settings["routing.timeout"]} 秒 ·{" "}
                  {kind === "chat"
                    ? `${m?.context || state.settings["memory.messages"]} 条上下文`
                    : m?.enabled
                      ? abilities[kind]
                      : "已停用"}
                </small>
              </div>
              <div className="pipeline-controls">
                <button
                  className="icon-button"
                  aria-label={`前移 ${m?.name}`}
                  disabled={i === 0 || user.role !== "admin"}
                  onClick={() => move(i, -1)}
                >
                  <ArrowUp size={14} />
                </button>
                <button
                  className="icon-button"
                  aria-label={`后移 ${m?.name}`}
                  disabled={i === ids.length - 1 || user.role !== "admin"}
                  onClick={() => move(i, 1)}
                >
                  <ArrowDown size={14} />
                </button>
                <button
                  className="icon-button"
                  aria-label={`移出流水线 ${m?.name}`}
                  disabled={user.role !== "admin"}
                  onClick={() => setDraft(ids.filter((x) => x !== id))}
                >
                  <X size={14} />
                </button>
              </div>
              {i < ids.length - 1 && (
                <ArrowRight className="pipeline-arrow" size={17} />
              )}
            </div>
          );
        })}
      </div>
      <div className="pipeline-footer">
        <select
          aria-label="添加流水线模型"
          value=""
          disabled={user.role !== "admin"}
          onChange={(e) => setDraft([...ids, e.target.value])}
        >
          <option value="">＋ 添加模型到流水线</option>
          {state.models
            .filter(
              (m) =>
                m.enabled &&
                m.capabilities.includes(kind) &&
                !ids.includes(m.id),
            )
            .map((m) => (
              <option key={m.id} value={m.id}>
                {m.name}
              </option>
            ))}
        </select>
        <button
          className="button small"
          disabled={busy || draft === null || user.role !== "admin"}
          onClick={save}
        >
          {busy ? "保存中…" : "保存顺序"}
        </button>
      </div>
    </section>
  );
}

// 获取目录与保存厂商分开：用户可以检查返回的模型，不会把目录中的所有模型自动注册。
function VendorForm({
  initial,
  capability,
  act,
  notify,
  onClose,
}: PageProps & { initial: Vendor; capability: string; onClose: () => void }) {
  const [v, setV] = useState({ ...initial, key: "" });
  const [list, setList] = useState<string[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [manual, setManual] = useState("");
  const [cap, setCap] = useState([capability]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const discover = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await request<{ models: string[] }>(
        "/api/models/discover",
        v,
      );
      setList(result.models);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="form-stack"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try {
          const models = Array.from(
            new Set([
              ...selected,
              ...manual
                .split(/[,\n]/)
                .map((s) => s.trim())
                .filter(Boolean),
            ]),
          ).map((model) => ({
            model,
            name: model,
            capabilities: cap,
            timeout: cap.includes("video_generation") ? 600 : 0,
            enabled: true,
          }));
          await act({ kind: "vendor", vendor: v, models });
          notify("厂商与模型已保存");
          onClose();
        } catch (e) {
          setError((e as Error).message);
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="form-two">
        <label>
          厂商名称
          <input
            required
            value={v.name}
            list="model-vendor-presets"
            autoComplete="off"
            onChange={(e) => {
              const name = e.target.value;
              const preset = vendorPresets.find((item) => item.name === name);
              // 命中预设才填地址；手动名称不会覆盖已有连接信息。
              if (preset) {
                const changed = preset.endpoint !== v.endpoint || preset.provider !== v.provider;
                setV({ ...v, name, endpoint: preset.endpoint, provider: preset.provider,
                  key: changed ? "" : v.key, configured: changed ? false : v.configured });
                setList([]); setSelected([]); setManual(""); setError("");
              } else setV({ ...v, name });
            }}
            placeholder="选择厂商或手动输入…"
          />
          <datalist id="model-vendor-presets">
            {vendorPresets.map((preset) => <option key={preset.name} value={preset.name} />)}
          </datalist>
        </label>
        <label>
          API 协议
          <select
            aria-label="API 协议"
            value={v.provider}
            onChange={(e) =>
              setV({
                ...v,
                provider: e.target.value,
                endpoint:
                  {
                    mimo: "https://api.xiaomimimo.com/v1",
                    gemini: "https://generativelanguage.googleapis.com/v1beta",
                    anthropic: "https://api.anthropic.com/v1",
                    openai:
                      capability === "video_generation"
                        ? ""
                        : "https://api.openai.com/v1",
                  }[e.target.value] || "",
              })
            }
          >
            {[
              ["openai", "OpenAI 兼容"],
              ["mimo", "小米 MiMo"],
              ["gemini", "Gemini 原生"],
              ["anthropic", "Anthropic 原生"],
              ...(v.provider === "mock" ? [["mock", "脱机模拟"]] : []),
            ].map(([id, name]) => (
              <option key={id} value={id}>
                {name}
              </option>
            ))}
          </select>
        </label>
      </div>
      <label>
        API 地址
        <input
          type="url"
          required={v.provider !== "mock"}
          value={v.endpoint}
          onChange={(e) => setV({ ...v, endpoint: e.target.value })}
          placeholder="https://api.example.com/v1"
        />
      </label>
      {vendorPresets.filter((preset) => preset.name === v.name).map((preset) => (
        <small key={preset.name} className="muted">
          {preset.note} <a href={preset.docs} target="_blank" rel="noreferrer">官方 API 文档</a>
        </small>
      ))}
      <label>
        API Key
        <input
          type="password"
          autoComplete="new-password"
          value={v.key}
          onChange={(e) => setV({ ...v, key: e.target.value })}
          placeholder={
            v.configured
              ? "留空保留已保存密钥；修改地址需重新输入"
              : "输入厂商提供的密钥…"
          }
        />
      </label>
      <div className="field-row">
        <strong>可用模型</strong>
        <button
          type="button"
          className="button small"
          disabled={busy}
          onClick={discover}
        >
          {busy ? "处理中…" : "获取模型"}
        </button>
      </div>
      {list.length > 0 && (
        <div className="discovered-models">
          {list.map((id) => (
            <label key={id}>
              <input
                type="checkbox"
                checked={selected.includes(id)}
                onChange={(e) =>
                  setSelected(
                    e.target.checked
                      ? [...selected, id]
                      : selected.filter((x) => x !== id),
                  )
                }
              />
              <code>{id}</code>
            </label>
          ))}
        </div>
      )}
      <label>
        手动添加模型 ID
        <textarea
          rows={2}
          value={manual}
          onChange={(e) => setManual(e.target.value)}
          placeholder="目录不可用时填写，多个 ID 用逗号或换行分隔…"
        />
      </label>
      <fieldset>
        <legend>为新添加的模型开放能力</legend>
        <div className="checkbox-grid">
          {Object.entries(abilities)
            .filter(([id]) => !["insight"].includes(id))
            .map(([id, name]) => (
              <label key={id}>
                <input
                  type="checkbox"
                  checked={cap.includes(id)}
                  onChange={(e) =>
                    setCap(
                      e.target.checked
                        ? [...cap, id]
                        : cap.filter((x) => x !== id),
                    )
                  }
                />
                {name}
              </label>
            ))}
        </div>
      </fieldset>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="form-footer">
        {v.id ? (
          <button
            type="button"
            className="button danger"
            disabled={busy}
            onClick={async () => {
              if (confirm("删除厂商？需先移除其模型。")) {
                try {
                  await act({ kind: "delete-vendor", id: v.id });
                  onClose();
                } catch (e) {
                  setError((e as Error).message);
                }
              }
            }}
          >
            删除厂商
          </button>
        ) : (
          <span />
        )}
        <button className="button primary" disabled={busy}>
          {busy ? "处理中…" : "保存厂商"}
        </button>
      </div>
    </form>
  );
}
function ModelSettings({
  model,
  act,
  notify,
  onClose,
}: PageProps & { model: Model; onClose: () => void }) {
  const [m, set] = useState(model);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <form
      className="form-stack"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        try {
          await act({ kind: "model", model: m });
          notify("模型参数已保存");
          onClose();
        } catch (e) {
          setError((e as Error).message);
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        显示名称
        <input
          required
          value={m.name}
          onChange={(e) => set({ ...m, name: e.target.value })}
        />
      </label>
      <code>{m.model}</code>
      <div className="form-two">
        <label>
          超时时间（秒，0 继承）
          <input
            type="number"
            min={0}
            max={600}
            value={m.timeout || 0}
            onChange={(e) => set({ ...m, timeout: Number(e.target.value) })}
          />
        </label>
        <label>
          温度（留空由厂商决定）
          <input
            type="number"
            min={0}
            max={2}
            step={0.1}
            value={m.temperature ?? ""}
            onChange={(e) =>
              set({
                ...m,
                temperature:
                  e.target.value === "" ? undefined : Number(e.target.value),
              })
            }
          />
        </label>
        <label>
          上下文条数（0 继承）
          <input
            type="number"
            min={0}
            max={500}
            value={m.context || 0}
            onChange={(e) => set({ ...m, context: Number(e.target.value) })}
          />
        </label>
        <label>
          最大输出 Token（0 继承）
          <input
            type="number"
            min={0}
            max={32768}
            value={m.max_output || 0}
            onChange={(e) => set({ ...m, max_output: Number(e.target.value) })}
          />
        </label>
      </div>
      <label>
        思维链
        <select
          aria-label="思维链"
          value={m.thinking || "inherit"}
          onChange={(e) => set({ ...m, thinking: e.target.value })}
        >
          <option value="inherit">使用厂商默认值</option>
          <option value="on">开启</option>
          <option value="off">关闭</option>
        </select>
        <small className="muted">
          控制模型是否启用推理；需厂商和模型支持，不代表会返回推理全文。部分模型开启后不接受温度设置。
        </small>
      </label>
      <fieldset>
        <legend>模型能力</legend>
        <div className="checkbox-grid">
          {Object.entries(abilities)
            .filter(([id]) => !["insight"].includes(id))
            .map(([id, name]) => (
              <label key={id}>
                <input
                  type="checkbox"
                  checked={m.capabilities.includes(id)}
                  onChange={(e) =>
                    set({
                      ...m,
                      capabilities: e.target.checked
                        ? [...m.capabilities, id]
                        : m.capabilities.filter((x) => x !== id),
                    })
                  }
                />
                {name}
              </label>
            ))}
        </div>
      </fieldset>
      <div className="field-row">
        <span>启用模型</span>
        <Toggle
          label="启用模型"
          checked={m.enabled}
          onChange={(enabled) => set({ ...m, enabled })}
        />
      </div>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <div className="form-footer">
        <button
          type="button"
          className="button danger"
          disabled={busy}
          onClick={async () => {
            if (confirm(`删除模型“${m.name}”？`)) {
              try {
                await act({ kind: "delete-model", id: m.id });
                onClose();
              } catch (e) {
                setError((e as Error).message);
              }
            }
          }}
        >
          <Trash size={15} />
          删除模型
        </button>
        <button className="button primary" disabled={busy}>
          {busy ? "保存中…" : "保存参数"}
        </button>
      </div>
    </form>
  );
}

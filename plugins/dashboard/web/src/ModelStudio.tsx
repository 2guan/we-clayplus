import { BehaviorRecords } from "./BehaviorRecords";
// 文件名：ModelStudio.tsx
// 主要功能：提供对话测活、语音试听、图片预览、人设全文编辑和逐项行为权限设置。
// 作者：2guan
import { useEffect, useState } from "react";
import { Plus, Play, Star, Trash, UploadSimple } from "@phosphor-icons/react";
import {
  type PageProps,
  type Prompt,
  type BehaviorPolicy,
  type Model,
  modelPipeline,
} from "./types";
import { Badge, Empty, SettingsForm, Toggle, useUnsavedDraft } from "./ui";
import { readFile, request } from "./api";
import { TaskBadge, TaskResult } from "./ModelTasks";

// 浏览器只提交任务一次；结果持续从 SSE 状态读取，刷新列表不会重复调用厂商。
export function ModelPlayground(
  props: PageProps & { kind: string; selectedModel?: Model },
) {
  const { state, kind, act, notify, selectedModel } = props;
  const isVideo = kind === "video_generation";
  const isVisual = kind === "image" || isVideo;
  const [size, setSize] = useState(
    String(state.settings["video_generation.size"] || "1280x720"),
  );
  const [seconds, setSeconds] = useState(
    String(state.settings["video_generation.seconds"] || "4"),
  );
  const [ability, setAbility] = useState(kind);
  const [model, setModel] = useState("");
  const [text, setText] = useState(
    kind === "tts"
      ? "把细节慢慢做好，就是最好的设计。"
      : kind === "image"
        ? "窗边的一束白色小花，自然光，细腻的胶片质感。"
        : isVideo
          ? "清晨的湖面泛起涟漪，镜头缓慢前移，柔和自然光。"
          : "你好，请简单介绍一下自己。",
  );
  const [file, setFile] = useState<File | null>(null);
  const [voice, setVoice] = useState(
    String(state.settings["tts.voice"] || "mimo_default"),
  );
  const [design, setDesign] = useState(
    String(state.settings["tts.design"] || ""),
  );
  const [mode, setMode] = useState(() => {
    const modelID =
      state.models.find((m) => m.id === state.settings["routing.tts"])?.model ||
      "";
    return modelID.endsWith("-voicedesign")
      ? "design"
      : modelID.endsWith("-voiceclone")
        ? "clone"
        : "tts";
  });
  // 修改默认视频参数后同步工作台；单次试验仍可独立覆盖。
  useEffect(() => {
    setSize(String(state.settings["video_generation.size"] || "1280x720"));
    setSeconds(String(state.settings["video_generation.seconds"] || "4"));
  }, [
    state.settings["video_generation.size"],
    state.settings["video_generation.seconds"],
  ]);
  const [selfie, setSelfie] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [taskID, setTaskID] = useState("");
  const task = state.tasks.find((t) => t.id === taskID);
  const pipelineModels = modelPipeline(state, ability)
    .map((id) => state.models.find((m) => m.id === id))
    .filter((m) => m?.enabled);
  const matchesMode = (m: Model) =>
    mode === "clone"
      ? m.model.endsWith("-voiceclone")
      : mode === "design"
        ? m.model.endsWith("-voicedesign")
        : !m.model.endsWith("-voiceclone") && !m.model.endsWith("-voicedesign");
  const current = model
    ? state.models.find((m) => m.id === model)
    : pipelineModels.find((m) => m && (kind !== "tts" || matchesMode(m)));
  const isMimo = current?.provider === "mimo";
  useEffect(() => {
    if (selectedModel) {
      setModel(selectedModel.id);
      setAbility(
        selectedModel.capabilities.includes(kind)
          ? kind
          : selectedModel.capabilities[0],
      );
      if (selectedModel.model.endsWith("-voicedesign")) setMode("design");
      else if (selectedModel.model.endsWith("-voiceclone")) setMode("clone");
      else setMode("tts");
    }
  }, [selectedModel]);
  const start = async () => {
    setBusy(true);
    setError("");
    try {
      if (!text.trim()) throw Error("请输入内容");
      if (kind === "tts" && mode !== "tts" && !current)
        throw Error("请先添加并选择此模式的语音模型");
      if (kind === "tts" && mode === "design" && !design.trim())
        throw Error("请输入声音设计提示词");
      if (
        kind === "tts" &&
        mode === "clone" &&
        !file &&
        !state.settings["tts.reference"]
      )
        throw Error("请上传音色样本");
      const media = file ? await readFile(file) : "";
      const result = await request<{ id: string }>("/api/tasks", {
        kind: ability,
        model,
        prompt: text,
        media,
        mime: file?.type || "",
        filename: file?.name || "",
        voice,
        design,
        selfie,
        speech_mode: kind === "tts" ? mode : "",
        size: isVideo ? size : "",
        seconds: isVideo ? seconds : "",
      });
      setTaskID(result.id);
      await props.refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const setDefault = async () => {
    try {
      await act({
        kind: "pipeline",
        group: ability,
        pipeline: [
          model,
          ...modelPipeline(state, ability).filter((id) => id !== model),
        ],
      });
      notify("默认模型已更新");
    } catch (e) {
      notify((e as Error).message);
    }
  };
  const chooseMode = (value: string) => {
    setMode(value);
    setFile(null);
    setTaskID("");
    const suffix =
      value === "design"
        ? "-voicedesign"
        : value === "clone"
          ? "-voiceclone"
          : "-tts";
    const match = state.models.find(
      (m) =>
        m.enabled && m.capabilities.includes("tts") && m.model.endsWith(suffix),
    );
    const inPipeline = modelPipeline(state, "tts").some((id) =>
      state.models.some(
        (m) =>
          m.id === id &&
          m.enabled &&
          (value === "tts"
            ? !m.model.endsWith("-voiceclone") &&
              !m.model.endsWith("-voicedesign")
            : m.model.endsWith(suffix)),
      ),
    );
    setModel(inPipeline ? "" : match?.id || "");
  };
  const options = state.models.filter(
    (m) =>
      m.enabled &&
      m.capabilities.includes(ability) &&
      (kind !== "tts" ||
        (mode === "tts"
          ? !m.model.endsWith("-voicedesign") &&
            !m.model.endsWith("-voiceclone")
          : m.model.endsWith(
              mode === "design" ? "-voicedesign" : "-voiceclone",
            ))),
  );
  return (
    <>
      <div className="studio-heading">
        <h2>
          {kind === "tts"
            ? "语音试听"
            : kind === "image"
              ? "图片工作台"
              : isVideo
                ? "视频工作台"
                : "模型测试"}
        </h2>
        <span className="muted">
          {state.mock ? "固定示例结果 · 不消耗额度" : "结果仅在后台预览"}
        </span>
      </div>
      <div className="lab-grid model-lab">
        <section className="panel">
          <div className="form-stack">
            {kind === "chat" && (
              <label>
                测试能力
                <select
                  value={ability}
                  onChange={(e) => {
                    setAbility(e.target.value);
                    setModel("");
                    setFile(null);
                    setTaskID("");
                  }}
                >
                  {[
                    ["chat", "对话"],
                    ["vision", "图片理解"],
                    ["audio", "音频识别"],
                  ].map(([id, name]) => (
                    <option key={id} value={id}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {kind === "tts" && (
              <div className="studio-segments" aria-label="语音模式">
                {[
                  ["tts", "TTS"],
                  ["design", "VoiceDesign"],
                  ["clone", "VoiceClone"],
                ].map(([id, label]) => (
                  <button
                    key={id}
                    className={mode === id ? "active" : ""}
                    onClick={() => chooseMode(id)}
                  >
                    {label}
                  </button>
                ))}
              </div>
            )}
            <label>
              执行模型
              <select value={model} onChange={(e) => setModel(e.target.value)}>
                <option value="">使用默认流水线</option>
                {options.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.model}
                  </option>
                ))}
              </select>
            </label>
            {kind !== "chat" && (
              <button
                className="text-link"
                disabled={!model || props.user.role !== "admin"}
                onClick={setDefault}
              >
                设为{kind === "tts" ? "语音" : isVideo ? "视频" : "图片"}
                默认模型
              </button>
            )}
            {isVideo && (
              <>
                <div className="form-two">
                  <label>
                    本次视频尺寸
                    <select
                      aria-label="本次视频尺寸"
                      value={size}
                      onChange={(e) => setSize(e.target.value)}
                    >
                      {[
                        ["1280x720", "横屏 · 1280 × 720"],
                        ["720x1280", "竖屏 · 720 × 1280"],
                        ["1792x1024", "横屏 · 1792 × 1024"],
                        ["1024x1792", "竖屏 · 1024 × 1792"],
                      ].map(([id, label]) => (
                        <option key={id} value={id}>
                          {label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    本次视频时长
                    <select
                      aria-label="本次视频时长"
                      value={seconds}
                      onChange={(e) => setSeconds(e.target.value)}
                    >
                      {["4", "8", "12"].map((value) => (
                        <option key={value} value={value}>
                          {value} 秒
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                <p className="muted">
                  支持 OpenAI Videos
                  兼容接口。参考图作为首帧，尺寸需与输出一致。
                </p>
              </>
            )}
            {kind === "tts" && (
              <>
                {mode === "tts" && (
                  <label>
                    发言人
                    <input
                      list="voice-presets"
                      value={voice}
                      onChange={(e) => setVoice(e.target.value)}
                      placeholder="选择或填写音色 ID…"
                    />
                    <datalist id="voice-presets">
                      {(isMimo
                        ? [
                            "mimo_default",
                            "冰糖",
                            "茉莉",
                            "苏打",
                            "白桦",
                            "Mia",
                            "Chloe",
                            "Milo",
                            "Dean",
                          ]
                        : [
                            "alloy",
                            "ash",
                            "coral",
                            "echo",
                            "fable",
                            "nova",
                            "onyx",
                            "sage",
                            "shimmer",
                          ]
                      ).map((v) => (
                        <option key={v} value={v} />
                      ))}
                    </datalist>
                  </label>
                )}
                {mode !== "clone" && (
                  <label>
                    {mode === "design"
                      ? "声音设计提示词"
                      : "语气与风格（MiMo）"}
                    <textarea
                      aria-label={
                        mode === "design"
                          ? "声音设计提示词"
                          : "语气与风格（MiMo）"
                      }
                      rows={3}
                      value={design}
                      onChange={(e) => setDesign(e.target.value)}
                      placeholder="描述音色、节奏和情绪…"
                    />
                  </label>
                )}
                <button
                  className="text-link"
                  onClick={async () => {
                    try {
                      await act({
                        kind: "settings",
                        group: "tts",
                        revision: state.config_revision,
                        values: { voice, design },
                      });
                      notify("默认语音配置已保存");
                    } catch (e) {
                      notify((e as Error).message);
                    }
                  }}
                >
                  保存为默认语音参数
                </button>
              </>
            )}
            {(ability === "vision" ||
              ability === "audio" ||
              isVisual ||
              (kind === "tts" && mode === "clone")) && (
              <label className="studio-upload">
                <UploadSimple size={20} />
                <span>
                  {file
                    ? file.name
                    : isVisual
                      ? isVideo
                        ? "添加本次参考图（可选）"
                        : "添加本次垫图（可选）"
                      : mode === "clone"
                        ? "上传音色样本 · WAV / MP3"
                        : "上传识别文件"}
                </span>
                <input
                  type="file"
                  accept={
                    ability === "audio" || (kind === "tts" && mode === "clone")
                      ? "audio/wav,audio/mpeg"
                      : "image/png,image/jpeg,image/webp"
                  }
                  onChange={(e) => setFile(e.target.files?.[0] || null)}
                />
              </label>
            )}
            {file && (
              <button className="text-link" onClick={() => setFile(null)}>
                移除本次附件
              </button>
            )}
            {kind === "tts" && mode === "clone" && (
              <button
                className="text-link"
                disabled={!file}
                onClick={async () => {
                  if (file) {
                    try {
                      await act({
                        kind: "reference-audio",
                        media: await readFile(file),
                      });
                      notify("默认音色样本已保存");
                    } catch (e) {
                      notify((e as Error).message);
                    }
                  }
                }}
              >
                保存为默认克隆样本
              </button>
            )}
            {kind === "image" && (
              <label className="inline-check">
                <input
                  type="checkbox"
                  checked={selfie}
                  onChange={(e) => setSelfie(e.target.checked)}
                />
                自拍 / 自己的照片（无本次垫图时使用默认垫图或微信头像）
              </label>
            )}
            <label>
              {kind === "tts"
                ? "试听文案"
                : isVisual
                  ? "画面提示词"
                  : "输入内容"}
              <textarea
                rows={5}
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
            </label>
            {error && (
              <p role="alert" className="form-error">
                {error}
              </p>
            )}
            <button
              className="button primary"
              disabled={
                busy || task?.status === "running" || task?.status === "queued"
              }
              onClick={start}
            >
              <Play size={16} />
              {busy
                ? "提交中…"
                : kind === "tts"
                  ? "生成试听"
                  : isVisual
                    ? "生成预览"
                    : "开始测试"}
            </button>
          </div>
        </section>
        <section className="panel result-panel">
          <div className="section-heading">
            <h3>
              {kind === "tts"
                ? "试听结果"
                : kind === "image"
                  ? "图片预览"
                  : isVideo
                    ? "视频预览"
                    : "测试结果"}
            </h3>
            {task && (
              <div className="actions">
                <TaskBadge task={task} />
                {["queued", "running"].includes(task.status) && (
                  <button
                    className="text-link"
                    onClick={() =>
                      act({ kind: "cancel-task", id: task.id }).catch((e) =>
                        notify(e.message),
                      )
                    }
                  >
                    停止等待
                  </button>
                )}
              </div>
            )}
          </div>
          {task ? (
            <TaskResult task={task} />
          ) : (
            <Empty
              title={
                kind === "tts"
                  ? "等待生成音频"
                  : kind === "image"
                    ? "等待生成图片"
                    : isVideo
                      ? "等待生成视频"
                      : "等待测试结果"
              }
            />
          )}
        </section>
      </div>
      {isVisual && (
        <div className="image-defaults">
          <section className="panel form-stack">
            <h3>{isVideo ? "默认参考图" : "默认垫图"}</h3>
            <p className="muted">
              {isVideo
                ? "本次未上传参考图时使用；留空即为文生视频。"
                : "用于自拍与自己的照片。未上传时读取当前微信头像。"}
            </p>
            {state.settings[`${kind}.reference`] && (
              <img
                className="reference-thumb"
                src={String(state.settings[`${kind}.reference`])}
                alt={isVideo ? "默认参考图" : "默认垫图"}
              />
            )}
            <label className="studio-upload">
              <UploadSimple size={20} />
              {isVideo ? "上传默认参考图" : "上传默认垫图"}
              <input
                type="file"
                accept="image/png,image/jpeg,image/webp"
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (file) {
                    try {
                      await act({
                        kind: isVideo ? "reference-video" : "reference-image",
                        media: await readFile(file),
                      });
                      notify(isVideo ? "默认参考图已保存" : "默认垫图已保存");
                    } catch (e) {
                      notify((e as Error).message);
                    }
                  }
                }}
              />
            </label>
            <button
              className="text-link"
              disabled={!state.settings[`${kind}.reference`]}
              onClick={() =>
                act({
                  kind: isVideo ? "reference-video" : "reference-image",
                }).catch((e) => notify(e.message))
              }
            >
              {isVideo ? "移除参考图，恢复文生视频" : "移除垫图，恢复微信头像"}
            </button>
          </section>
          {props.schema
            .filter((g) => g.id === kind)
            .map((g) => (
              <SettingsForm
                key={g.id}
                {...props}
                group={{
                  ...g,
                  title: "默认生成参数",
                  fields: g.fields.filter((f) =>
                    [
                      "aspect",
                      "resolution",
                      "quality",
                      "style",
                      ...(isVideo ? ["size", "seconds"] : []),
                    ].includes(f.key),
                  ),
                }}
              />
            ))}
        </div>
      )}
    </>
  );
}

export function PersonaEditor(props: PageProps) {
  const { state, act, notify } = props;
  const [selected, setSelected] = useState(state.prompts[0]?.id || "");
  const [creating, setCreating] = useState(false);
  const change = (id: string) => {
    setSelected(id);
    setCreating(false);
  };
  return (
    <div className="persona-workspace">
      <aside className="persona-list">
        <div className="section-heading">
          <h3>人设库</h3>
          <button
            className="icon-button"
            aria-label="创建人设"
            onClick={() => {
              setCreating(true);
              setSelected("");
            }}
          >
            <Plus size={20} />
          </button>
        </div>
        {state.prompts.map((p, i) => (
          <article
            className={`persona-item ${p.id === selected ? "active" : ""}`}
            key={p.id}
          >
            <button className="persona-select" onClick={() => change(p.id)}>
              <strong>{p.name}</strong>
              <small>{p.description || "自定义人设"}</small>
              {i === 0 && <Badge tone="green">默认生效</Badge>}
            </button>
            <div className="actions">
              <button
                className="icon-button"
                aria-label={`设为默认 ${p.name}`}
                disabled={i === 0 || props.user.role !== "admin"}
                onClick={() =>
                  act({ kind: "default-prompt", id: p.id }).catch((e) =>
                    notify(e.message),
                  )
                }
              >
                <Star size={16} weight={i === 0 ? "fill" : "regular"} />
              </button>
              <button
                className="icon-button"
                aria-label={`删除人设 ${p.name}`}
                onClick={async () => {
                  if (confirm(`删除人设“${p.name}”？`)) {
                    try {
                      await act({ kind: "delete-prompt", id: p.id });
                      if (selected === p.id) setSelected("");
                    } catch (e) {
                      notify((e as Error).message);
                    }
                  }
                }}
              >
                <Trash size={16} />
              </button>
            </div>
          </article>
        ))}
      </aside>
      {creating || state.prompts.some((p) => p.id === selected) ? (
        <PromptDetail
          key={creating ? "new" : selected}
          {...props}
          initial={
            state.prompts.find((p) => p.id === selected) || {
              id: "",
              name: "",
              description: "",
              content: "",
            }
          }
          onSaved={() => {
            setCreating(false);
            if (creating) setSelected("");
          }}
        />
      ) : (
        <section className="panel">
          <Empty title="选择或创建一个人设" />
        </section>
      )}
    </div>
  );
}
function PromptDetail({
  initial,
  onSaved,
  ...props
}: PageProps & { initial: Prompt; onSaved: () => void }) {
  const [p, set] = useState(initial);
  const [instruction, setInstruction] = useState("");
  const [taskID, setTaskID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [optimizeError, setOptimizeError] = useState("");
  const task = props.state.tasks.find((t) => t.id === taskID);
  useUnsavedDraft(
    JSON.stringify(p) !==
      JSON.stringify(
        props.state.prompts.find((item) => item.id === p.id) || initial,
      ),
  );
  // 优化结果先预览，经用户选择后替换草稿，避免模型自动覆盖已保存人设。
  return (
    <section className="panel prompt-detail">
      <form
        className="form-stack"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError("");
          try {
            await props.act({ kind: "prompt", prompt: p });
            props.notify("人设已保存");
            onSaved();
          } catch (e) {
            setError((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="form-two">
          <label>
            人设名称
            <input
              required
              value={p.name}
              onChange={(e) => set({ ...p, name: e.target.value })}
            />
          </label>
          <label>
            简短说明
            <input
              value={p.description}
              onChange={(e) => set({ ...p, description: e.target.value })}
            />
          </label>
        </div>
        <label>
          人设提示词全文
          <textarea
            className="code-editor prompt-full"
            required
            rows={15}
            value={p.content}
            onChange={(e) => set({ ...p, content: e.target.value })}
          />
        </label>
        {error && (
          <p role="alert" className="form-error">
            {error}
          </p>
        )}
        <div className="form-footer">
          <span className="muted">{p.content.length} 字</span>
          <button className="button primary" disabled={busy}>
            {busy ? "保存中…" : "保存人设"}
          </button>
        </div>
      </form>
      {/* 优化区域复用表单间距与主题色；请求错误和候选结果留在当前操作区域。 */}
      <form
        className="prompt-optimize form-stack"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setOptimizeError("");
          try {
            if (!p.content.trim() || !instruction.trim())
              throw Error("请填写原提示词和修改要求");
            const result = await request<{ id: string }>(
              "/api/prompts/optimize",
              {
                original: p.content,
                instruction,
              },
            );
            setTaskID(result.id);
            await props.refresh();
          } catch (e) {
            setOptimizeError((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="prompt-optimize-heading">
          <h3>提示词优化</h3>
          <span className="muted">默认对话模型</span>
        </div>
        <label>
          修改要求
          <textarea
            aria-label="修改要求"
            rows={3}
            value={instruction}
            onChange={(e) => setInstruction(e.target.value)}
            placeholder="例如：保留温和语气，回复更简洁，减少反问…"
          />
        </label>
        {optimizeError && (
          <p className="form-error" role="alert">
            {optimizeError}
          </p>
        )}
        <div className="prompt-optimize-actions">
          <span className="muted">先预览，采用后再保存</span>
          <button
            className="button primary"
            aria-label="使用默认模型优化"
            disabled={
              busy || task?.status === "running" || task?.status === "queued"
            }
          >
            {task?.status === "running" || task?.status === "queued"
              ? "正在优化…"
              : "开始优化"}
          </button>
        </div>
      </form>
      {/* 候选版本与输入区分开呈现，采用只更新草稿，不直接覆盖已保存人设。 */}
      {task && (
        <section
          className="prompt-candidate"
          aria-label="优化结果"
          aria-busy={task.status === "running" || task.status === "queued"}
        >
          <header>
            <h3>优化结果</h3>
            <TaskBadge task={task} />
          </header>
          {task.status === "completed" ? (
            <>
              <div className="prompt-candidate-text prose">{task.result}</div>
              <footer>
                <button className="button small" onClick={() => setTaskID("")}>
                  放弃结果
                </button>
                <button
                  className="button primary small"
                  onClick={() => {
                    set({ ...p, content: task.result });
                    setTaskID("");
                  }}
                >
                  采用优化结果
                </button>
              </footer>
            </>
          ) : (
            <p
              className={`prompt-candidate-status ${task.error ? "form-error" : "muted"}`}
              role="status"
            >
              {task.error ||
                (["running", "queued"].includes(task.status)
                  ? "正在根据修改要求生成候选版本…"
                  : "优化已停止，可重新提交。")}
            </p>
          )}
        </section>
      )}
    </section>
  );
}

const operationLabels: Record<string, string> = {
  Send: "发送消息",
  Forward: "转发消息",
  Revoke: "撤回消息",
  Download: "下载媒体",
  Get: "查询联系人",
  List: "获取列表",
  GetSelf: "当前账号",
  SetRemark: "修改备注",
  AddFriend: "发起好友申请",
  VerifyFriend: "处理好友申请",
  Search: "搜索联系人",
  GetOwner: "查询管理者",
  Create: "创建群聊",
  Quit: "退出群聊",
  RemoveMember: "移除群成员",
  AddMember: "直接添加成员",
  RemoveAdmin: "取消管理员",
  TransferOwner: "转让群主",
  BlacklistAdd: "加入黑名单",
  BlacklistRemove: "移出黑名单",
  FacingCreate: "面对面建群",
  DeleteComment: "删除评论",
  SetPrivacy: "修改隐私设置",
  GetInfo: "查询详情",
  GetQRCode: "获取二维码",
  InviteMember: "邀请成员",
  SetName: "修改群名",
  SetAnnouncement: "修改群公告",
  Save: "保存到通讯录",
  SetAdmin: "设置管理员",
  ListMembers: "群成员列表",
  GetMember: "查询群成员",
  GetMembersDetail: "成员详细资料",
  Timeline: "朋友圈时间线",
  UserPage: "好友朋友圈",
  Detail: "动态详情",
  Comment: "评论动态",
  Like: "点赞",
  Unlike: "取消点赞",
  Post: "发布动态",
  Upload: "上传媒体",
  Sync: "同步记录",
  UploadImage: "上传图片",
  UploadVideo: "上传视频",
  DownloadImage: "下载图片",
  DownloadVideo: "下载视频",
  UploadMomentsImage: "上传朋友圈图片",
  DownloadVideoCover: "下载视频封面",
  Add: "新增标签",
  Delete: "删除标签",
  Update: "更新标签",
  ModifyContactLabels: "修改联系人标签",
  GetRecord: "使用记录",
  StartTyping: "开始输入状态",
  StopTyping: "停止输入状态",
  GetProfile: "个人资料",
  GetSafetyInfo: "安全信息",
  GetBoundHardDevices: "绑定设备",
  UploadAvatar: "上传头像",
};
type ActionMeta = {
  operation: string;
  write: boolean;
  scope: string;
  legacy?: boolean;
  parameters: {
    properties?: Record<string, { description?: string; type?: string }>;
    required?: string[];
  };
};
type ActionCatalog = {
  actions: Record<string, ActionMeta>;
  backend: Record<string, boolean>;
};
type Capability = {
  label: string;
  enabled: string[];
  placeholder: string[];
  evidence: string;
};
export function BehaviorEditor(props: PageProps) {
  const [catalog, setCatalog] = useState<Record<string, Capability>>({});
  const [actions, setActions] = useState<ActionCatalog>({
    actions: {},
    backend: {},
  });
  const [selected, setSelected] = useState("");
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    request<ActionCatalog>("/api/ai-action-catalog")
      .then(setActions)
      .catch((e) => setError(e.message));
    request<Record<string, Capability>>("/api/behaviors")
      .then((v) => {
        setCatalog(v || {});
        const first = Object.entries(v || {}).find(([, g]) => g.enabled.length);
        if (v?.message?.enabled.includes("Send")) setSelected("message.Send");
        else if (first) setSelected(`${first[0]}.${first[1].enabled[0]}`);
      })
      .catch((e) => setError(e.message));
  }, []);
  const saved = props.state.policies?.find((p) => p.operation === selected);
  return (
    <>
      <div className="section-caption">
        <span>操作权限</span>
        <span className="muted">
          按能力配置发起人与目标权限 · 已保存配置保留
        </span>
      </div>
      {error && <p role="alert">{error}</p>}
      <div className="behavior-workspace">
        <aside className="behavior-list">
          <input
            aria-label="搜索行为"
            placeholder="搜索操作…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {Object.entries(catalog)
            .filter(([domain, g]) =>
              [...g.enabled, ...g.placeholder].some((op) =>
                `${g.label} ${operationLabels[op] || op} ${domain}.${op}`
                  .toLowerCase()
                  .includes(search.toLowerCase()),
              ),
            )
            .map(([domain, g]) => (
              <div key={domain}>
                <h4>{g.label}</h4>
                {[...g.enabled, ...g.placeholder]
                  .filter((op) =>
                    `${g.label} ${operationLabels[op] || op} ${domain}.${op}`
                      .toLowerCase()
                      .includes(search.toLowerCase()),
                  )
                  .map((op) => {
                    const id = `${domain}.${op}`;
                    return (
                      <button
                        key={id}
                        className={`behavior-item ${id === selected ? "active" : ""}`}
                        onClick={() => setSelected(id)}
                      >
                        <span>
                          {operationLabels[op] || op}
                          <small>
                            {op}
                            {g.placeholder.includes(op)
                              ? " · 暂不支持"
                              : actions.backend[id]
                                ? " · 后台配套"
                                : !actions.actions[id]
                                  ? " · 人工操作"
                                  : ""}
                          </small>
                        </span>
                        <span
                          className={`policy-dot ${props.state.policies?.some((p) => p.operation === id && p.enabled) ? "enabled" : ""}`}
                        />
                      </button>
                    );
                  })}
              </div>
            ))}
        </aside>
        {selected ? (
          <BehaviorDetail
            key={selected}
            {...props}
            evidence={catalog[selected.split(".")[0]]?.evidence || ""}
            meta={actions.actions[selected]}
            backend={!!actions.backend[selected]}
            supported={
              !!catalog[selected.split(".")[0]]?.enabled.includes(
                selected.split(".")[1],
              )
            }
            initial={
              saved || {
                operation: selected,
                enabled: false,
                prompt: "",
                scope: "all",
                list_mode: "deny",
                targets: [],
              }
            }
          />
        ) : (
          <Empty
            title="暂无操作目录"
            text="能力目录由 gateway 在启动时注入。"
          />
        )}
      </div>
      <BehaviorRecords {...props} names={operationLabels} domains={catalog} />
    </>
  );
}
function BehaviorDetail({
  initial,
  evidence,
  meta,
  backend,
  supported,
  ...props
}: PageProps & {
  initial: BehaviorPolicy;
  evidence: string;
  meta?: ActionMeta;
  backend: boolean;
  supported: boolean;
}) {
  const [p, set] = useState(initial);
  const [targets, setTargets] = useState(initial.targets.join(", "));
  const [actors, setActors] = useState((initial.actors || []).join(", "));
  const normalizedActors = actors
    .split(/[,，\n]/)
    .map((s) => s.trim())
    .filter(Boolean);
  const normalizedTargets = targets
    .split(/[,，\n]/)
    .map((s) => s.trim())
    .filter(Boolean);
  useUnsavedDraft(
    JSON.stringify({
      ...p,
      actors: normalizedActors,
      targets: normalizedTargets,
    }) !== JSON.stringify({ ...initial, actors: initial.actors || [] }),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <form
      className="panel form-stack behavior-detail"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try {
          await props.act({
            kind: "behavior-policy",
            policy: {
              ...p,
              actors: normalizedActors,
              targets: targets
                .split(/[,，\n]/)
                .map((s) => s.trim())
                .filter(Boolean),
            },
          });
          props.notify("行为权限已保存");
        } catch (e) {
          setError((e as Error).message);
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="section-heading">
        <h3>{p.operation}</h3>
        <Badge>
          {!supported
            ? "底层暂不支持"
            : backend
              ? "后台配套能力"
              : meta
                ? "对话工具已接通"
                : "仅限人工操作"}
        </Badge>
      </div>
      <p className="muted">{evidence}</p>
      <p className="muted">
        {!supported
          ? "现有接口尚未实现，此项不会提供给模型。"
          : backend
            ? "由对应消息、媒体或任务流程调用，不作为模型独立工具。"
            : !meta
              ? "此能力不向聊天模型开放。"
              : "支持 OpenAI 兼容、MiMo、Anthropic 和 Gemini 的工具调用。服务端逐步校验权限并保存真实操作回执。"}
      </p>
      <div className="field-row">
        <span>允许大模型操作</span>
        <Toggle
          label="允许大模型操作"
          checked={p.enabled}
          disabled={
            props.user.role !== "admin" || (!meta && !backend) || !supported
          }
          onChange={(enabled) => set({ ...p, enabled })}
        />
      </div>
      {meta && (
        <>
          <label>
            允许发起操作的成员 ID
            <textarea
              rows={2}
              value={actors}
              onChange={(e) => setActors(e.target.value)}
              placeholder="填写实际发言成员的微信 ID，以逗号分隔…"
            />
          </label>
          <p className="muted">
            {meta.legacy
              ? "留空沿用现有当前会话授权；跨会话操作仍需明确填写发起人。"
              : "新增能力需填写允许发起人的微信 ID；留空不会自动授权群内成员或私聊联系人。"}
          </p>
          {meta.scope === "target" && (
            <div className="field-row">
              <span>允许操作当前会话以外的目标</span>
              <Toggle
                label="允许跨会话操作"
                checked={!!p.allow_cross}
                disabled={props.user.role !== "admin"}
                onChange={(allow_cross) => set({ ...p, allow_cross })}
              />
            </div>
          )}
          {meta.write && (
            <div className="field-row">
              <span>执行前由后台确认</span>
              <Toggle
                label="执行前由后台确认"
                checked={!!p.confirm}
                disabled={props.user.role !== "admin"}
                onChange={(confirm) => set({ ...p, confirm })}
              />
            </div>
          )}
          {meta.scope === "self" && (
            <p className="muted">
              此能力作用于本人账号，影响范围请选择“当前账号”或“全部范围”。
            </p>
          )}
          <details>
            <summary>调用参数说明</summary>
            <dl className="behavior-preview">
              {Object.entries(meta.parameters.properties || {}).map(
                ([key, def]) => (
                  <div key={key}>
                    <dt>
                      {key}
                      {meta.parameters.required?.includes(key)
                        ? "（必填）"
                        : ""}
                    </dt>
                    <dd>{def.description || def.type}</dd>
                  </div>
                ),
              )}
            </dl>
          </details>
        </>
      )}
      <label>
        此操作的系统提示词
        <textarea
          rows={6}
          value={p.prompt}
          onChange={(e) => set({ ...p, prompt: e.target.value })}
          placeholder="描述适用场景、执行前提和限制…"
        />
      </label>
      <div className="form-two">
        <label>
          影响范围
          <select
            value={p.scope}
            onChange={(e) => set({ ...p, scope: e.target.value })}
          >
            {[
              ["all", "全部范围"],
              ["private", "私聊"],
              ["group", "群聊"],
              ["self", "当前账号"],
            ].map(([id, name]) => (
              <option key={id} value={id}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label>
          名单生效方式
          <select
            value={p.list_mode}
            onChange={(e) => set({ ...p, list_mode: e.target.value })}
          >
            <option value="allow">白名单：仅允许名单内目标</option>
            <option value="deny">黑名单：排除名单内目标</option>
          </select>
        </label>
      </div>
      <label>
        目标 ID
        <textarea
          rows={3}
          value={targets}
          onChange={(e) => setTargets(e.target.value)}
          placeholder="填写好友、群或当前账号的唯一 ID，以逗号分隔…"
        />
      </label>
      <p className="muted">
        发起人、目标范围和名单必须同时通过。发起人名单用于工具操作；普通自动回复仍由会话回复设置决定。跨会话发送不会改变收件会话的自动回复设置。
      </p>
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      <button
        className="button primary"
        disabled={
          busy ||
          props.user.role !== "admin" ||
          !supported ||
          (!meta && !backend)
        }
      >
        {busy ? "保存中…" : "保存行为配置"}
      </button>
    </form>
  );
}

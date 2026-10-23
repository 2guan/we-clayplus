// 海龟汤管理员工作台，复用控制台表单、弹窗及明暗主题。
import { useEffect, useRef, useState } from "react";
import { ArrowClockwise, Plus } from "@phosphor-icons/react";
import { Empty, Modal, Tabs } from "./ui";
import { dateTime, download, request } from "./api";
import type { PageProps } from "./types";

type Puzzle = {
  id: string;
  title: string;
  theme: string;
  difficulty: string;
  surface: string;
  bottom: string;
  facts: { id: string; text: string }[];
  required: string[];
  hints: string[];
  enabled: boolean;
};
type Game = {
  id: string;
  target: string;
  owner: string;
  theme: string;
  status: string;
  source: string;
  puzzle: Puzzle;
  hints: number;
  known: string[] | null;
  updated: string;
  revision: number;
};
type Verdict = {
  intent: string;
  answer: string;
  matched_fact_ids: string[];
  solved: boolean;
};
type Turn = {
  id: string;
  game_id: string;
  message: { sender: string; content: string; target: string };
  command: string;
  status: string;
  task_id: string;
  output: string;
  error: string;
  retryable: boolean;
  updated: string;
  verdict?: Verdict;
};
type Data = { puzzles: Puzzle[]; games: Game[]; turns: Turn[] };
const status: Record<string, string> = {
  generating: "出题中",
  playing: "进行中",
  paused: "已暂停",
  failed: "失败",
  ended: "已结束",
  revealed: "已揭晓",
  solved: "已通关",
  queued: "等待中",
  running: "处理中",
  prepared: "准备发送",
  sending: "发送中",
  completed: "已完成",
  unknown: "发送结果未知",
};
const emptyPuzzle = (): Puzzle => ({
  id: "",
  title: "",
  theme: "",
  difficulty: "中等",
  surface: "",
  bottom: "",
  facts: [
    { id: "f1", text: "" },
    { id: "f2", text: "" },
  ],
  required: ["f1", "f2"],
  hints: [""],
  enabled: true,
});

export function TurtleSoupWorkbench({
  state,
  notify,
  onClose,
}: PageProps & { onClose: () => void }) {
  const [tab, setTab] = useState(
    () =>
      new URLSearchParams(location.hash.split("?")[1]).get("soup") || "games",
  );
  const [data, setData] = useState<Data>({ puzzles: [], games: [], turns: [] });
  const [target, setTarget] = useState("");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<Puzzle | null>(null);
  const [dirty, setDirty] = useState(false);
  const [page, setPage] = useState(0);
  const [sample, setSample] = useState("来碗海龟汤 职场");
  const [sampleTarget, setSampleTarget] = useState(state.contacts[0]?.id || "");
  const mounted = useRef(false);
  const requestEpoch = useRef(0);
  async function reload() {
    const epoch = requestEpoch.current;
    try {
      const next = await request<Data>(
        `/api/turtlesoup?target=${encodeURIComponent(target)}`,
      );
      if (mounted.current && epoch === requestEpoch.current) {
        setData(next);
        setLoadError("");
      }
    } catch (e) {
      if (mounted.current && epoch === requestEpoch.current) setLoadError((e as Error).message);
    }
  }
  useEffect(() => {
    requestEpoch.current++;
    mounted.current = true;
    setData({ puzzles: [], games: [], turns: [] });
    void reload();
    const timer = setInterval(() => void reload(), 5000);
    return () => {
      mounted.current = false;
      requestEpoch.current++;
      clearInterval(timer);
    };
  }, [target, state.account.id]);
  useEffect(() => {
    const guard = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);
  function changeTab(value: string) {
    if (dirty && !confirm("题目还未保存，确定离开？")) return;
    setDraft(null);
    setDirty(false);
    setTab(value);
    setPage(0);
    const params = new URLSearchParams(location.hash.split("?")[1]);
    params.set("soup", value);
    history.replaceState(null, "", `#plugins?${params}`);
  }
  function close() {
    if (dirty && !confirm("题目还未保存，确定关闭？")) return;
    const params = new URLSearchParams(location.hash.split("?")[1]);
    params.delete("soup");
    history.replaceState(
      null,
      "",
      `#plugins${params.size ? `?${params}` : ""}`,
    );
    onClose();
  }
  async function mutate(body: unknown, success = "已更新") {
    setBusy(true);
    setError("");
    try {
      await request("/api/turtlesoup", body);
      notify(success);
      await reload();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  }
  const name = (id: string) => {
    const c = state.contacts.find((c) => c.id === id);
    return c?.remark || c?.name || id;
  };
  const active = data.games.filter((g) =>
    ["playing", "paused", "generating", "failed"].includes(g.status),
  );
  const shownGames = data.games.slice(page * 20, page * 20 + 20);
  const shownTurns = data.turns.slice(page * 20, page * 20 + 20);
  return (
    <Modal
      open
      onClose={close}
      title="海龟汤"
      description="一句话开局，按主题出题。题库、对局和问答由当前微信账号独立保存。"
      wide
    >
      <div className="soup-workbench">
        <div className="soup-intro">
          <span className="soup-mark" aria-hidden="true">
            🍲
          </span>
          <div>
            <strong>来碗海龟汤 职场，难一点</strong>
            <p>
              模型优先出题，固定题库兜底；主持使用当前微信昵称或配置的自称。
            </p>
          </div>
          <span className="badge">{active.length} 局进行中</span>
        </div>
        <Tabs
          value={tab}
          onChange={changeTab}
          items={[
            ["games", "对局"],
            ["puzzles", "题库"],
            ["turns", "问答与重试"],
            ...(state.mock ? [["demo", "脱机体验"] as [string, string]] : []),
          ]}
        />
        <div className="soup-toolbar">
          <label>
            会话筛选
            <select
              aria-label="海龟汤会话筛选"
              value={target}
              onChange={(e) => {
                setTarget(e.target.value);
                setPage(0);
              }}
            >
              <option value="">全部会话</option>
              {state.contacts
                .filter((c) => ["friend", "group"].includes(c.kind))
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.remark || c.name}
                  </option>
                ))}
            </select>
          </label>
          <button
            type="button"
            className="button"
            onClick={() => void reload()}
          >
            <ArrowClockwise size={16} />
            刷新
          </button>
        </div>
        {(error || loadError) && (
          <p className="form-error" role="alert">
            {error || loadError}
          </p>
        )}
        {tab === "games" && (
          <div className="soup-list">
            {!data.games.length && (
              <Empty
                title="还没有开局"
                text="启用插件并配置生效名单后，在私聊或群里发送“来碗海龟汤”，也可以加上想玩的主题。"
              />
            )}
            {shownGames.map((g) => (
              <article className="soup-card" key={g.id}>
                <div className="soup-heading">
                  <div>
                    <strong>{g.puzzle.title || "正在准备这碗汤"}</strong>
                    <p>
                      {name(g.target)} · {dateTime(g.updated)}
                    </p>
                  </div>
                  <span className="badge">{status[g.status] || g.status}</span>
                </div>
                <p className="soup-meta">
                  {g.theme || g.puzzle.theme || "自由主题"} ·{" "}
                  {g.source === "bank" ? "固定题库" : "模型出题"} · 已用{" "}
                  {g.hints} 次提示
                </p>
                <p className="soup-text">
                  {g.puzzle.surface ||
                    "题目尚未生成，可在“问答与重试”中查看结果。"}
                </p>
                {!!g.puzzle.bottom && (
                  <details className="soup-secret">
                    <summary>查看汤底与关键事实（管理员）</summary>
                    <p className="soup-text">{g.puzzle.bottom}</p>
                    <ul>
                      {g.puzzle.facts.map((f) => (
                        <li key={f.id}>
                          {f.id}：{f.text}
                          {g.known?.includes(f.id) ? " · 已猜中" : ""}
                        </li>
                      ))}
                    </ul>
                    <button
                      type="button"
                      className="button"
                      onClick={() => {
                        changeTab("puzzles");
                        setDraft({ ...g.puzzle, id: "", enabled: false });
                      }}
                    >
                      另存为题库草稿
                    </button>
                  </details>
                )}
                {["playing", "paused", "generating", "failed"].includes(
                  g.status,
                ) && (
                  <div className="actions">
                    {g.status === "playing" && (
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() =>
                          void mutate({
                            operation: "pause",
                            id: g.id,
                            revision: g.revision,
                          })
                        }
                      >
                        暂停
                      </button>
                    )}
                    {g.status === "paused" && !!g.puzzle.surface && (
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() =>
                          void mutate({
                            operation: "resume",
                            id: g.id,
                            revision: g.revision,
                          })
                        }
                      >
                        恢复
                      </button>
                    )}
                    <button
                      className="button"
                      disabled={busy}
                      onClick={() => {
                        if (
                          confirm(
                            "结束当前对局？此操作不会发送微信消息或公开汤底。",
                          )
                        )
                          void mutate({
                            operation: "end",
                            id: g.id,
                            revision: g.revision,
                          });
                      }}
                    >
                      结束对局…
                    </button>
                  </div>
                )}
              </article>
            ))}
          </div>
        )}
        {tab === "puzzles" &&
          (draft ? (
            <PuzzleEditor
              key={draft.id || "new"}
              puzzle={draft}
              busy={busy}
              onDirty={() => setDirty(true)}
              onCancel={() => {
                if (!dirty || confirm("放弃未保存的题目？")) {
                  setDraft(null);
                  setDirty(false);
                }
              }}
              onSave={async (p) => {
                if (
                  await mutate(
                    { operation: "save-puzzle", puzzle: p },
                    "题目已保存，正在进行的对局保持原题",
                  )
                ) {
                  setDraft(null);
                  setDirty(false);
                }
              }}
            />
          ) : (
            <>
              <div className="soup-toolbar">
                <p className="muted">
                  {data.puzzles.length} 道题 · 停用不影响进行中的对局
                </p>
                <div className="actions">
                  <button
                    className="button"
                    onClick={() => {
                      setDraft(emptyPuzzle());
                      setDirty(false);
                    }}
                  >
                    <Plus size={16} />
                    新增题目
                  </button>
                  <label className="button">
                    导入 JSON
                    <input
                      type="file"
                      className="sr-only"
                      aria-label="导入海龟汤题库"
                      accept="application/json,.json"
                      disabled={busy}
                      onChange={async (e) => {
                        const file = e.target.files?.[0];
                        e.target.value = "";
                        if (!file) return;
                        if (file.size > 256 * 1024) {
                          setError("题库文件不能超过 256 KiB");
                          return;
                        }
                        try {
                          const puzzles = JSON.parse(await file.text());
                          await mutate(
                            { operation: "import", puzzles },
                            "题库已导入",
                          );
                        } catch {
                          setError("请输入有效的 JSON 题目数组");
                        }
                      }}
                    />
                  </label>
                  <button
                    className="button"
                    onClick={() =>
                      download(
                        "海龟汤题库.json",
                        JSON.stringify(data.puzzles, null, 2),
                        "application/json",
                      )
                    }
                  >
                    导出
                  </button>
                </div>
              </div>
              <div className="soup-list">
                {data.puzzles.slice(page * 20, page * 20 + 20).map((p) => (
                  <article className="soup-card" key={p.id}>
                    <div className="soup-heading">
                      <strong>{p.title}</strong>
                      <span className="badge">
                        {p.enabled ? "已启用" : "已停用"}
                      </span>
                    </div>
                    <p className="soup-meta">
                      {p.theme} · {p.difficulty}
                    </p>
                    <p className="soup-text">{p.surface}</p>
                    <div className="actions">
                      <button
                        className="button"
                        onClick={() => {
                          setDraft(p);
                          setDirty(false);
                        }}
                      >
                        编辑题目
                      </button>
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() =>
                          void mutate({
                            operation: "save-puzzle",
                            puzzle: { ...p, enabled: !p.enabled },
                          })
                        }
                      >
                        {p.enabled ? "停用" : "启用"}
                      </button>
                    </div>
                  </article>
                ))}
              </div>
            </>
          ))}
        {tab === "turns" && (
          <div className="soup-list">
            {!data.turns.length && (
              <Empty
                title="还没有问答记录"
                text="这里会显示玩家问题、主持裁定和失败原因；失败且未发送的回合可以重试。"
              />
            )}
            {shownTurns.map((t) => (
              <article className="soup-card" key={t.id}>
                <div className="soup-heading">
                  <div>
                    <strong>{t.message.sender || "玩家"}</strong>
                    <p>
                      {name(t.message.target)} · {dateTime(t.updated)}
                    </p>
                  </div>
                  <span className="badge">{status[t.status] || t.status}</span>
                </div>
                <p className="soup-text">{t.message.content}</p>
                {t.output && <p className="soup-answer">{t.output}</p>}
                {t.error && <p className="form-error">{t.error}</p>}
                <div className="actions">
                  {t.retryable && t.status === "failed" && (
                    <button
                      className="button primary"
                      disabled={busy}
                      onClick={() =>
                        void mutate(
                          { operation: "retry", id: t.id },
                          "已提交重试",
                        )
                      }
                    >
                      {busy ? "提交中…" : "重试这一回合"}
                    </button>
                  )}
                  {t.verdict && active.some((g) => g.id === t.game_id) && (
                    <details className="soup-correction">
                      <summary>纠正裁定</summary>
                      <form
                        onSubmit={(e) => {
                          e.preventDefault();
                          const form = new FormData(e.currentTarget);
                          const g = active.find((g) => g.id === t.game_id)!;
                          void mutate(
                            {
                              operation: "correct",
                              id: t.id,
                              revision: g.revision,
                              verdict: {
                                intent: "question",
                                answer: form.get("answer"),
                                matched_fact_ids: [],
                                solved: false,
                              },
                            },
                            "已纠正记录，不会重复发送微信消息",
                          );
                        }}
                      >
                        <select
                          name="answer"
                          aria-label={`纠正 ${t.message.content}`}
                          defaultValue={t.verdict.answer}
                        >
                          <option value="yes">是</option>
                          <option value="no">不是</option>
                          <option value="irrelevant">无关</option>
                          <option value="unknown">信息不足</option>
                          <option value="split">拆分问题</option>
                        </select>
                        <button className="button" disabled={busy}>
                          保存纠正
                        </button>
                      </form>
                    </details>
                  )}
                </div>
              </article>
            ))}
          </div>
        )}
        {tab === "demo" && state.mock && (
          <form
            className="soup-editor"
            onSubmit={(e) => {
              e.preventDefault();
              void mutate(
                { operation: "simulate", target: sampleTarget, text: sample },
                "已提交脱机消息，不连接真实微信或模型",
              );
            }}
          >
            <p className="hint">
              脱机模型只演示流程，裁定固定为“信息不足”。真实模式没有这个入口。
            </p>
            <label>
              演示会话
              <select
                value={sampleTarget}
                onChange={(e) => setSampleTarget(e.target.value)}
              >
                {state.contacts
                  .filter((c) => ["friend", "group"].includes(c.kind))
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </label>
            <label>
              模拟消息
              <input
                value={sample}
                onChange={(e) => setSample(e.target.value)}
                maxLength={1000}
              />
            </label>
            <button className="button primary" disabled={busy}>
              {busy ? "提交中…" : "提交模拟消息"}
            </button>
          </form>
        )}
        {!draft && tab !== "demo" && (
          <div className="soup-pagination">
            <button
              className="button"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              上一页
            </button>
            <span>
              第 {page + 1} 页 · 最近{" "}
              {tab === "games"
                ? data.games.length
                : tab === "turns"
                  ? data.turns.length
                  : data.puzzles.length}{" "}
              条
            </span>
            <button
              className="button"
              disabled={
                (page + 1) * 20 >=
                (tab === "games"
                  ? data.games.length
                  : tab === "turns"
                    ? data.turns.length
                    : data.puzzles.length)
              }
              onClick={() => setPage(page + 1)}
            >
              下一页
            </button>
          </div>
        )}
      </div>
    </Modal>
  );
}

function PuzzleEditor({
  puzzle,
  busy,
  onDirty,
  onCancel,
  onSave,
}: {
  puzzle: Puzzle;
  busy: boolean;
  onDirty: () => void;
  onCancel: () => void;
  onSave: (p: Puzzle) => Promise<void>;
}) {
  const [p, setP] = useState(puzzle);
  const [facts, setFacts] = useState(
    puzzle.facts.map((f) => `${f.id}|${f.text}`).join("\n"),
  );
  const [required, setRequired] = useState(puzzle.required.join(","));
  const [hints, setHints] = useState(puzzle.hints.join("\n"));
  const update = (key: keyof Puzzle, value: unknown) => {
    setP({ ...p, [key]: value });
    onDirty();
  };
  return (
    <form
      className="soup-editor"
      onSubmit={(e) => {
        e.preventDefault();
        void onSave({
          ...p,
          facts: facts
            .split("\n")
            .filter((s) => s.trim())
            .map((s) => {
              const index = s.indexOf("|");
              return {
                id: index < 0 ? "" : s.slice(0, index).trim(),
                text: index < 0 ? s : s.slice(index + 1).trim(),
              };
            }),
          required: required.split(/[,，\s]+/).filter(Boolean),
          hints: hints.split("\n").filter((s) => s.trim()),
        });
      }}
    >
      <div className="soup-heading">
        <strong>{puzzle.id ? "编辑题目" : "新增题目"}</strong>
        <button type="button" className="button" onClick={onCancel}>
          返回题库
        </button>
      </div>
      <div className="soup-columns">
        <label>
          标题
          <input
            value={p.title}
            required
            maxLength={80}
            onChange={(e) => update("title", e.target.value)}
          />
        </label>
        <label>
          主题
          <input
            value={p.theme}
            required
            maxLength={100}
            onChange={(e) => update("theme", e.target.value)}
            placeholder="例如：职场…"
          />
        </label>
        <label>
          难度
          <select
            value={p.difficulty}
            onChange={(e) => update("difficulty", e.target.value)}
          >
            <option>简单</option>
            <option>中等</option>
            <option>困难</option>
          </select>
        </label>
      </div>
      <label>
        汤面
        <textarea
          value={p.surface}
          rows={4}
          required
          onChange={(e) => update("surface", e.target.value)}
        />
      </label>
      <label>
        汤底
        <textarea
          value={p.bottom}
          rows={5}
          required
          onChange={(e) => update("bottom", e.target.value)}
        />
      </label>
      <label>
        关键事实
        <textarea
          aria-label="关键事实"
          aria-describedby="soup-facts-help"
          value={facts}
          rows={4}
          required
          onChange={(e) => {
            setFacts(e.target.value);
            onDirty();
          }}
        />
      </label>
      <p className="hint" id="soup-facts-help">
        每行一项，格式为 f1|事实内容；至少两项。
      </p>
      <label>
        通关要点
        <input
          value={required}
          required
          onChange={(e) => {
            setRequired(e.target.value);
            onDirty();
          }}
        />
      </label>
      <p className="hint">填写关键事实 ID，以逗号分隔，例如 f1,f2。</p>
      <label>
        分级提示
        <textarea
          value={hints}
          rows={3}
          required
          onChange={(e) => {
            setHints(e.target.value);
            onDirty();
          }}
        />
      </label>
      <p className="hint">每行一条，由浅入深，最多五条。</p>
      <label className="soup-checkbox">
        <input
          type="checkbox"
          checked={p.enabled}
          onChange={(e) => update("enabled", e.target.checked)}
        />
        启用此题，允许作为出题失败后的备选
      </label>
      <div className="actions">
        <button className="button primary" disabled={busy}>
          {busy ? "保存中…" : "保存题目"}
        </button>
        <button type="button" className="button" onClick={onCancel}>
          取消
        </button>
      </div>
    </form>
  );
}

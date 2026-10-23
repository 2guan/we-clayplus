// 互动插件工作台：对局、失败重试、秘密发放核对与群梗审核。
import { useEffect, useRef, useState } from "react";
import { ArrowClockwise } from "@phosphor-icons/react";
import { Empty, Modal, Tabs } from "./ui";
import { dateTime, request } from "./api";
import type { PageProps, Plugin } from "./types";
export const playroomIDs = [
  "undercover",
  "werewolf",
  "avalon",
  "murder",
  "chemistry",
  "romance",
  "radio",
  "dungeon",
  "trpg",
  "fortune",
  "personality",
  "group_digest",
  "group_memes",
];
type Player = {
  id: string;
  name: string;
  ready: boolean;
  alive: boolean;
  role?: string;
  word?: string;
  hp: number;
};
type Game = {
  id: string;
  code: string;
  kind: string;
  target: string;
  status: string;
  phase: string;
  round: number;
  revision: number;
  updated: string;
  players: Player[];
  theme: string;
  pack: { title: string; text: string; solution?: string };
};
type Turn = {
  id: string;
  plugin: string;
  target: string;
  game_id: string;
  status: string;
  error: string;
  retryable: boolean;
  updated: string;
  cursor: number;
  message: { sender: string; content: string };
  deliveries?: {
    target: string;
    text: string;
    private: boolean;
    sent: boolean;
  }[];
};
type Meme = {
  id: string;
  target: string;
  source_id: string;
  text: string;
  explanation: string;
  status: string;
  created: string;
  source_available: boolean;
};
type Data = { games: Game[]; turns: Turn[]; memes: Meme[] };
const status: Record<string, string> = {
  lobby: "报名中",
  playing: "进行中",
  paused: "已暂停",
  ended: "已结束",
  queued: "等待中",
  running: "处理中",
  prepared: "准备发送",
  sending: "发送中",
  completed: "已完成",
  failed: "失败",
  unknown: "发送结果未知",
  pending: "待审核",
  confirmed: "已确认",
  rejected: "已拒绝",
  deleted: "已删除",
  cancelled: "已取消",
};
const phases: Record<string, string> = {
  speech: "轮流发言",
  vote: "投票",
  guess: "出局猜词",
  wolves: "狼人行动",
  night: "夜间技能",
  hunter: "猎人开枪",
  day: "白天讨论",
  team: "队长组队",
  approval: "组队审批",
  quest: "秘密任务",
  assassinate: "最终刺杀",
  investigate: "搜证讨论",
  accuse: "指认凶手",
  quiz: "答题",
  story: "剧情互动",
  explore: "探索",
  done: "结束",
};
export function PlayroomWorkbench({
  state,
  notify,
  plugin,
  onClose,
}: PageProps & { plugin: Plugin; onClose: () => void }) {
  const [tab, setTab] = useState(
    new URLSearchParams(location.hash.split("?")[1]).get("tab") || "games",
  );
  const [data, setData] = useState<Data>({ games: [], turns: [], memes: [] });
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [page, setPage] = useState(0);
  const epoch = useRef(0);
  const [sampleTarget, setSampleTarget] = useState(
    state.contacts.find((c) => c.kind === "group")?.id ||
      state.contacts[0]?.id ||
      "",
  );
  const [actor, setActor] = useState(
    state.contacts.find((c) => c.kind === "friend")?.id || "",
  );
  const [text, setText] = useState(
    plugin.usage?.commands?.[0]?.examples?.[0] || "进度",
  );
  const [quote, setQuote] = useState("");
  const [editing, setEditing] = useState("");
  const [draft, setDraft] = useState("");
  async function reload() {
    const ticket = epoch.current;
    try {
      const next = await request<Data>(
        `/api/playroom?plugin=${encodeURIComponent(plugin.id)}&target=${encodeURIComponent(target)}`,
      );
      if (ticket === epoch.current) {
        setData(next);
        setError("");
      }
    } catch (e) {
      if (ticket === epoch.current) setError((e as Error).message);
    }
  }
  useEffect(() => {
    epoch.current++;
    setData({ games: [], turns: [], memes: [] });
    void reload();
    const timer = setInterval(() => void reload(), 5000);
    return () => {
      epoch.current++;
      clearInterval(timer);
    };
  }, [state.account.id, plugin.id, target]);
  async function mutate(body: object, message: string) {
    setBusy(true);
    setError("");
    try {
      await request("/api/playroom", body);
      notify(message);
      await reload();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  }
  function changeTab(next: string) {
    setTab(next);
    setPage(0);
    const params = new URLSearchParams(location.hash.split("?")[1]);
    params.set("play", plugin.id);
    params.set("tab", next);
    history.replaceState(null, "", `#plugins?${params}`);
  }
  const needsSecret = [
    "undercover",
    "werewolf",
    "avalon",
    "murder",
    "chemistry",
  ].includes(plugin.id);
  const intro = needsSecret
    ? "群内真实 @ 开局 → 报名 → 好友私聊确认入局 对局码 → 按私聊收件码完成双向确认 → 房主开始游戏。秘密操作：游戏 对局码 操作。"
    : (
        {
          romance:
            "仅在私聊开启虚构剧情。直接回应或选择选项，结束游戏后恢复普通聊天。尺度在配置中选择。",
          radio: "仅在私聊开启电台。可以倾诉或点主题，结束游戏后恢复普通聊天。",
          fortune:
            "在允许的会话抽签；同一玩家、同一会话的北京时间当日结果会缓存，仅供娱乐。",
          personality:
            "按主题生成趣味题目，回复答 A/B/C/D。结果由程序计分，不是专业心理测评。",
          group_digest:
            "在允许的群里真实 @ 本人生成日报或周报，仅使用本群资料并附来源；失败可以重试，不静默删减资料。",
          group_memes:
            "引用本群有效消息后真实 @ 本人收藏这个梗；候选经后台确认才可查询和回顾，来源撤回后停止使用。",
        } as Record<string, string>
      )[plugin.id] ||
      "私聊可以单人直接开局；群内真实 @ 开局、报名后由房主开始。公开冒险不要求加好友，地图、背包和骰点独立保存。";
  const tabs = [
    { id: "games", label: "对局与存档" },
    { id: "turns", label: "回合与重试" },
    ...(plugin.id === "group_memes"
      ? [{ id: "memes", label: "群梗审核" }]
      : []),
    ...(state.mock ? [{ id: "demo", label: "脱机体验" }] : []),
  ];
  const games = data.games.slice(page * 20, (page + 1) * 20);
  const turns = data.turns.slice(page * 20, (page + 1) * 20);
  const memes = data.memes.slice(page * 20, (page + 1) * 20);
  const count =
    tab === "games"
      ? data.games.length
      : tab === "turns"
        ? data.turns.length
        : data.memes.length;
  return (
    <Modal
      open
      title={`${plugin.name}工作台`}
      description="查看独立存档和执行记录。秘密资料仅管理员可读，发送结果未知时请先核对微信。"
      onClose={() => {
        if (
          editing &&
          draft !== data.memes.find((m) => m.id === editing)?.explanation &&
          !window.confirm("群梗说明尚未保存，仍要关闭吗？")
        )
          return;
        const params = new URLSearchParams(location.hash.split("?")[1]);
        params.delete("play");
        params.delete("tab");
        history.replaceState(
          null,
          "",
          `#plugins${params.size ? `?${params}` : ""}`,
        );
        onClose();
      }}
      wide
    >
      <div className="soup-workbench play-workbench">
        <div className="soup-intro">
          <div>
            <strong>独立存档，秘密信息只走好友私聊</strong>
            <p>{intro} 真实模式不支持模拟输入。</p>
          </div>
        </div>
        <div className="soup-toolbar">
          <label>
            筛选会话
            <select
              aria-label="筛选会话"
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
                    {c.name}
                  </option>
                ))}
            </select>
          </label>
          <button
            className="button"
            disabled={busy}
            onClick={() => void reload()}
          >
            <ArrowClockwise size={16} />
            刷新
          </button>
        </div>
        <Tabs
          value={tab}
          onChange={changeTab}
          items={tabs.map((t) => [t.id, t.label] as [string, string])}
        />
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {tab === "games" &&
          (games.length === 0 ? (
            <Empty
              title="还没有对局存档"
              text="在启用范围内发送开局指令。抽签、日报和群梗的结果见回合记录。"
            />
          ) : (
            <div className="soup-list">
              {games.map((g) => (
                <article className="soup-card" key={g.id}>
                  <div className="soup-heading">
                    <strong>
                      {g.pack?.title || plugin.name} · {g.code}
                    </strong>
                    <span className="badge">
                      {status[g.status] || g.status}
                    </span>
                  </div>
                  <p className="soup-meta">
                    {state.contacts.find((c) => c.id === g.target)?.name ||
                      g.target}{" "}
                    · {phases[g.phase] || "等待报名"} · 第{g.round || 0}轮 ·{" "}
                    {dateTime(g.updated)}
                  </p>
                  <div className="play-players">
                    {g.players.map((p, i) => (
                      <span key={p.id}>
                        {i + 1}. {p.name} ·{" "}
                        {g.status === "lobby"
                          ? p.ready
                            ? "私聊已确认"
                            : "待私聊确认"
                          : p.alive
                            ? "在场"
                            : "出局"}
                      </span>
                    ))}
                  </div>
                  {g.pack?.text && <p className="soup-text">{g.pack.text}</p>}
                  <div className="actions">
                    {g.status !== "ended" && (
                      <>
                        <button
                          className="button"
                          disabled={busy}
                          onClick={() =>
                            void mutate(
                              {
                                operation:
                                  g.status === "paused" ? "resume" : "pause",
                                id: g.id,
                                revision: g.revision,
                              },
                              "对局状态已更新",
                            )
                          }
                        >
                          {g.status === "paused" ? "恢复对局" : "暂停对局"}
                        </button>
                        <button
                          className="button danger"
                          disabled={busy}
                          onClick={() => {
                            if (
                              window.confirm(
                                "结束这局并停止后续操作？原始聊天和存档会保留。",
                              )
                            )
                              void mutate(
                                {
                                  operation: "end",
                                  id: g.id,
                                  revision: g.revision,
                                },
                                "对局已结束",
                              );
                          }}
                        >
                          结束对局
                        </button>
                      </>
                    )}
                  </div>
                  <details className="soup-secret">
                    <summary>查看管理员秘密资料（不要截图发到群里）</summary>
                    <div className="soup-text">
                      {g.players.map((p, i) => (
                        <p key={p.id}>
                          {i + 1}号 {p.name}：{p.role || "无秘密身份"}
                          {p.word ? ` · 词语：${p.word}` : ""}
                        </p>
                      ))}
                      {g.pack?.solution && <p>{g.pack.solution}</p>}
                    </div>
                  </details>
                </article>
              ))}
            </div>
          ))}
        {tab === "turns" &&
          (turns.length === 0 ? (
            <Empty
              title="还没有执行记录"
              text="每轮操作与失败信息会出现在这里。"
            />
          ) : (
            <div className="soup-list">
              {turns.map((t) => (
                <article className="soup-card" key={t.id}>
                  <div className="soup-heading">
                    <strong>{t.message.sender || "玩家"}</strong>
                    <span className="badge">
                      {status[t.status] || t.status}
                    </span>
                  </div>
                  <p className="soup-meta">
                    {dateTime(t.updated)} · 已确认发送 {t.cursor}/
                    {t.deliveries?.length || 0} 条
                  </p>
                  <p className="soup-text">{t.message.content}</p>
                  {t.error && <p className="form-error">{t.error}</p>}
                  {t.status === "failed" && t.retryable && (
                    <button
                      className="button"
                      disabled={busy}
                      onClick={() =>
                        void mutate(
                          { operation: "retry", id: t.id },
                          "已提交重试",
                        )
                      }
                    >
                      重试这一回合
                    </button>
                  )}
                  {t.status === "unknown" && (
                    <div>
                      <p className="hint">
                        先核对微信中当前这条发送是否成功。未确认时请暂停或结束对局，不要重复发放。
                      </p>
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() => {
                          if (
                            window.confirm(
                              "你已在微信核实当前条目确实发送成功？确认后只会继续发送账本中剩余条目。",
                            )
                          )
                            void mutate(
                              { operation: "confirm-sent", id: t.id },
                              "已确认当前条目，继续处理剩余消息",
                            );
                        }}
                      >
                        已核实当前条目发送成功
                      </button>
                    </div>
                  )}
                  <details className="soup-secret">
                    <summary>查看本轮输出与秘密发放账本</summary>
                    {t.deliveries?.map((d, i) => (
                      <div className="soup-answer" key={i}>
                        <strong>
                          {d.private ? "秘密私聊" : "原会话公开"} ·{" "}
                          {d.sent ? "已确认" : "未确认"}
                        </strong>
                        <p className="soup-text">{d.text}</p>
                      </div>
                    ))}
                  </details>
                </article>
              ))}
            </div>
          ))}
        {tab === "memes" &&
          (memes.length === 0 ? (
            <Empty
              title="还没有群梗候选"
              text="引用本群的有效原始消息，发送“收藏这个梗”。"
            />
          ) : (
            <div className="soup-list">
              {memes.map((m) => (
                <article className="soup-card" key={m.id}>
                  <div className="soup-heading">
                    <strong>{m.explanation.split("：")[0] || "群梗"}</strong>
                    <span className="badge">
                      {status[m.status] || m.status}
                    </span>
                  </div>
                  <p className="soup-meta">
                    {dateTime(m.created)} · 来源 {m.source_id} ·{" "}
                    {m.source_available ? "来源有效" : "来源不可用"}
                  </p>
                  <p className="soup-answer">{m.text || "原文已清除"}</p>
                  <p className="soup-text">{m.explanation}</p>
                  <div className="actions">
                    {m.status === "pending" && (
                      <>
                        <button
                          className="button primary"
                          disabled={busy || !m.source_available}
                          onClick={() =>
                            void mutate(
                              { operation: "meme-confirm", id: m.id },
                              "群梗已确认",
                            )
                          }
                        >
                          确认应用
                        </button>
                        <button
                          className="button"
                          disabled={busy}
                          onClick={() =>
                            void mutate(
                              { operation: "meme-reject", id: m.id },
                              "已拒绝，不自动重复提交",
                            )
                          }
                        >
                          拒绝
                        </button>
                      </>
                    )}
                    {m.status !== "deleted" && (
                      <>
                        <button
                          className="button"
                          disabled={busy}
                          onClick={() => {
                            setEditing(m.id);
                            setDraft(m.explanation);
                          }}
                        >
                          编辑说明
                        </button>
                        <button
                          className="button danger"
                          disabled={busy}
                          onClick={() => {
                            if (
                              window.confirm(
                                "删除这条群梗？原始聊天消息会保留。",
                              )
                            )
                              void mutate(
                                { operation: "meme-delete", id: m.id },
                                "群梗已删除",
                              );
                          }}
                        >
                          删除
                        </button>
                      </>
                    )}
                  </div>
                  {editing === m.id && (
                    <form
                      className="soup-editor"
                      onSubmit={(e) => {
                        e.preventDefault();
                        void mutate(
                          { operation: "meme-edit", id: m.id, text: draft },
                          "已保存，重新进入待审核",
                        ).then((ok) => {
                          if (ok) setEditing("");
                        });
                      }}
                    >
                      <label>
                        群梗说明
                        <textarea
                          aria-label="群梗说明"
                          value={draft}
                          required
                          maxLength={2000}
                          rows={4}
                          onChange={(e) => setDraft(e.target.value)}
                        />
                      </label>
                      <div className="actions">
                        <button className="button primary" disabled={busy}>
                          保存并待审核
                        </button>
                        <button
                          className="button"
                          type="button"
                          onClick={() => setEditing("")}
                        >
                          取消
                        </button>
                      </div>
                    </form>
                  )}
                </article>
              ))}
            </div>
          ))}
        {tab === "demo" && state.mock && (
          <form
            className="soup-editor"
            onSubmit={(e) => {
              e.preventDefault();
              void mutate(
                {
                  operation: "simulate",
                  target: sampleTarget,
                  actor,
                  text,
                  quote_id: quote,
                },
                "已提交脱机消息",
              );
            }}
          >
            <p className="hint">
              仅使用演示账号与固定模型资料。群开局自动附加真实 @
              元数据。模拟好友私聊时，把“演示会话”和“发言玩家”选为同一个好友。确认入局后，还需按私聊收到的收件码回复“确认收件
              对局码 收件码”。
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
              发言玩家
              <select value={actor} onChange={(e) => setActor(e.target.value)}>
                {state.contacts
                  .filter((c) => c.kind === "friend")
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
                value={text}
                required
                maxLength={4000}
                onChange={(e) => setText(e.target.value)}
              />
            </label>
            <label>
              引用消息 ID（群梗用，可留空）
              <input value={quote} onChange={(e) => setQuote(e.target.value)} />
            </label>
            <button className="button primary" disabled={busy}>
              {busy ? "提交中…" : "提交模拟消息"}
            </button>
          </form>
        )}
        {tab !== "demo" && (
          <div className="soup-pagination">
            <button
              className="button"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              上一页
            </button>
            <span>
              第{page + 1}页 · 最近{count}条
            </span>
            <button
              className="button"
              disabled={(page + 1) * 20 >= count}
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

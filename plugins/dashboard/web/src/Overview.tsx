// 文件名：Overview.tsx
// 主要功能：展示账号资产、模型用量、消息趋势和运行健康，区分模拟与真实数据。
// 作者：2guan
import { useEffect, useState } from "react";
import {
  ArrowUpRight,
  Users,
  ChatsCircle,
  PuzzlePiece,
  CheckCircle,
  ArrowClockwise,
  Circle,
  Clock,
  Cpu,
  Database,
  PlugsConnected,
  Sparkle,
} from "@phosphor-icons/react";
import { WechatLogin } from "./WechatLogin";
import type { PageProps, State } from "./types";
import { Avatar, Badge, Empty } from "./ui";
import { dateTime, number } from "./api";

// 使用本地日历日期，避免 UTC 转换导致起止日期偏移一天。
function calendarDate(value: Date) {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")}`;
}
function validDate(value: string) {
  return (
    /^\d{4}-\d{2}-\d{2}$/.test(value) &&
    calendarDate(new Date(`${value}T00:00:00`)) === value
  );
}
function recentRange(days: number) {
  const end = new Date();
  const start = new Date(end);
  start.setDate(start.getDate() - days + 1);
  return { days, start: calendarDate(start), end: calendarDate(end) };
}
// 地址保存已应用的范围，刷新或浏览器前进后退时可恢复；非法日期回退到近七天。
function readRange() {
  const params = new URLSearchParams(location.hash.split("?")[1]);
  const start = params.get("from") || "";
  const end = params.get("to") || "";
  if (
    validDate(start) &&
    validDate(end) &&
    start <= end &&
    end <= calendarDate(new Date())
  ) {
    return { days: 0, start, end };
  }
  const days = Number(params.get("days"));
  return recentRange([7, 14, 30].includes(days) ? days : 7);
}

// 静态图表仅在数据改变时重绘，不创建逐帧动画和后台轮询。
function Chart({
  trend,
  kind,
}: {
  trend: State["trend"];
  kind: "tokens" | "messages";
}) {
  const max = Math.max(
    1,
    ...trend.map((p) =>
      kind === "tokens" ? p.input + p.output : p.received + p.sent,
    ),
  );
  const ticks = [1, 0.75, 0.5, 0.25, 0];
  // 所有柱子均保留；长范围只稀疏显示日期刻度，首尾日期始终可见。
  const labels = new Set(
    Array.from({ length: Math.min(trend.length, 6) }, (_, index) =>
      Math.round(
        (index * (trend.length - 1)) /
          Math.max(1, Math.min(trend.length, 6) - 1),
      ),
    ),
  );
  return (
    <div className="chart">
      <div className="chart-y">
        {ticks.map((n) => (
          <span key={n}>
            {kind === "tokens"
              ? `${Math.round((max * n) / 1000)}k`
              : Math.round(max * n)}
          </span>
        ))}
      </div>
      <div className="chart-plot">
        <div className="chart-grid">
          {ticks.map((n) => (
            <div key={n} />
          ))}
        </div>
        <div
          className="bars"
          style={{ columnGap: `${Math.min(7, 40 / trend.length)}%` }}
        >
          {trend.map((p, index) => {
            const a = kind === "tokens" ? p.input : p.received;
            const b = kind === "tokens" ? p.output : p.sent;
            return (
              <div className="bar-column" key={p.date}>
                <div
                  className="bar-stack"
                  tabIndex={0}
                  aria-label={`${p.date}，${a} / ${b}`}
                  title={`${p.date} · ${number(a)} / ${number(b)}`}
                  style={{ height: `${Math.max(2, ((a + b) / max) * 100)}%` }}
                >
                  <div className="bar-light" style={{ flex: b }} />
                  <div className="bar-dark" style={{ flex: a }} />
                </div>
                {labels.has(index) && <span>{p.date.slice(-5)}</span>}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
// 只更新运行时间卡片，避免每秒重绘图表；用单调时钟补足两次状态同步之间的时间。
function UptimeAsset({ seconds }: { seconds?: number }) {
  const [elapsed, setElapsed] = useState(seconds);
  useEffect(() => {
    setElapsed(seconds);
    if (seconds === undefined) return;
    const receivedAt = performance.now();
    const timer = window.setInterval(() => {
      setElapsed(seconds + Math.floor((performance.now() - receivedAt) / 1000));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [seconds]);
  const total = Math.max(0, Math.floor(elapsed ?? 0));
  const days = Math.floor(total / 86400);
  // 只显示两位时分，不展示秒；满一天后才加上天数。
  const clock = [Math.floor(total / 3600) % 24, Math.floor(total / 60) % 60]
    .map((value) => String(value).padStart(2, "0"))
    .join(":");
  return (
    <div className="asset uptime-asset" title="当前服务本次启动后的运行时间">
      <Clock size={18} />
      <span>已运行时间</span>
      <strong>
        {elapsed === undefined
          ? "—"
          : `${days > 0 ? `${days}天 ` : ""}${clock}`}
      </strong>
    </div>
  );
}

export default function Overview({ state, refresh, notify }: PageProps) {
  const [range, setRange] = useState(readRange);
  const [customOpen, setCustomOpen] = useState(range.days === 0);
  const [start, setStart] = useState(range.start);
  const [end, setEnd] = useState(range.end);
  const [rangeError, setRangeError] = useState("");
  const date = calendarDate(new Date());
  const data = state.trend
    .filter((point) => point.date >= range.start && point.date <= range.end)
    .sort((a, b) => a.date.localeCompare(b.date));
  const applyRange = (next: ReturnType<typeof readRange>) => {
    setRange(next);
    setStart(next.start);
    setEnd(next.end);
    setCustomOpen(next.days === 0);
    setRangeError("");
    const query = next.days
      ? `days=${next.days}`
      : `from=${next.start}&to=${next.end}`;
    if (location.hash !== `#overview?${query}`)
      history.pushState(null, "", `#overview?${query}`);
  };
  useEffect(() => {
    const restore = () => {
      const next = readRange();
      setRange(next);
      setStart(next.start);
      setEnd(next.end);
      setCustomOpen(next.days === 0);
      setRangeError("");
    };
    window.addEventListener("hashchange", restore);
    return () => window.removeEventListener("hashchange", restore);
  }, []);
  const today = state.trend.find((p) => p.date === date);
  const total = data.reduce((v, p) => v + p.input + p.output, 0);
  const groups = state.contacts.filter((c) => c.kind === "group").length;
  // 发送来源保存在归档消息中；仅成功发出的本人消息计数，与失败/等待回执区分。
  const sentBySource = { manual: 0, ai: 0, plugin: 0, wechat: 0, other: 0 };
  for (const message of state.messages) {
    if (
      !message.self ||
      message.status !== "sent" ||
      !message.time.startsWith(date)
    )
      continue;
    const source = message.source || "";
    if (source === "manual") sentBySource.manual++;
    else if (source === "ai") sentBySource.ai++;
    else if (source === "plugin" || source.startsWith("plugin:"))
      sentBySource.plugin++;
    else if (source === "wechat") sentBySource.wechat++;
    else sentBySource.other++;
  }
  const sentToday = Object.values(sentBySource).reduce(
    (sum, count) => sum + count,
    0,
  );
  // 首页属于“今日运行”，只统计本地日历日创建的任务；历史失败不再误报为待处理。
  const todayTasks = state.tasks.filter((task) => {
    const created = new Date(task.created);
    return !Number.isNaN(created.getTime()) && calendarDate(created) === date;
  });
  const completedTasks = todayTasks.filter(
    (task) => task.status === "completed",
  ).length;
  const activeTasks = todayTasks.filter(
    (task) => task.status === "queued" || task.status === "running",
  ).length;
  const failedTasks = todayTasks.filter(
    (task) => task.status === "failed" || task.status === "interrupted",
  ).length;
  const cancelledTasks = todayTasks.filter(
    (task) => task.status === "cancelled",
  ).length;
  return (
    <div className="page overview">
      <div className="page-toolbar">
        <h1 className="page-title">总览</h1>
        <button
          className="button"
          onClick={() => refresh().catch((e) => notify(e.message))}
        >
          <ArrowClockwise size={16} />
          刷新数据
        </button>
      </div>
      <div className="account-strip">
        <WechatLogin
          className="account-identity"
          online={state.account.status === "online"}
        >
          <Avatar
            src={state.account.photo}
            blank={state.account.status !== "online"}
            name={state.account.avatar || state.account.name}
            size="large"
          />
          <div>
            <div className="inline">
              <h3>{state.account.name}</h3>
              <Badge
                tone={state.account.status === "online" ? "green" : "amber"}
              >
                <Circle size={7} weight="fill" />
                {state.account.status === "online" ? "在线" : "离线"}
              </Badge>
            </div>
            <code>{state.account.id || "等待微信连接"}</code>
            <small>Owner · {state.account.owner || "尚未绑定"}</small>
          </div>
        </WechatLogin>
        {[
          [Users, "好友", state.contacts.length - groups],
          [ChatsCircle, "群聊", groups],
          [
            PuzzlePiece,
            "运行插件",
            state.plugins.filter((p) => p.enabled).length,
          ],
        ].map(([Icon, label, value]) => {
          const I = Icon as typeof Users;
          return (
            <a
              href={label === "运行插件" ? "#plugins" : "#contacts"}
              key={String(label)}
              className="asset"
            >
              <I size={18} />
              <span>{String(label)}</span>
              <strong>{number(value as number)}</strong>
            </a>
          );
        })}
        <UptimeAsset seconds={state.uptime_seconds} />
      </div>
      <div className="section-caption">
        <span>
          <span className="tiny-square" />
          今日运行
        </span>
        <span className="muted">
          {state.mock ? "演示统计 · 非真实用量" : "仅统计本系统已记录数据"}
        </span>
      </div>
      <div className="metrics">
        <div className="metric">
          <span>
            今日 Token 用量 <Cpu size={17} />
          </span>
          <strong>{number((today?.input || 0) + (today?.output || 0))}</strong>
          <div>
            <Badge tone="green">输入 / 输出</Badge>
            <small>
              {number(today?.input || 0)} / {number(today?.output || 0)}
            </small>
          </div>
        </div>
        <div className="metric">
          <span>
            今日收到消息 <ChatsCircle size={17} />
          </span>
          <strong>
            {number(today?.received || 0)}
            <em>条</em>
          </strong>
          <div>
            <span className="status-dot" />
            <small>按已确认接收消息统计</small>
          </div>
        </div>
        <div className="metric">
          <span>
            今日发出消息 <ArrowUpRight size={17} />
          </span>
          <strong>
            {number(sentToday)}
            <em>条</em>
          </strong>
          <div>
            <small>
              手动 {sentBySource.manual} · AI {sentBySource.ai} · 插件{" "}
              {sentBySource.plugin}
              {sentBySource.wechat > 0 && ` · 微信端 ${sentBySource.wechat}`}
              {sentBySource.other > 0 && ` · 其他 ${sentBySource.other}`}
            </small>
          </div>
        </div>
        <div className="metric">
          <span>
            模型任务 <Sparkle size={17} />
          </span>
          <strong>
            {todayTasks.length}
            <em>次</em>
          </strong>
          <div>
            <small>
              已完成 {completedTasks} · 进行中 {activeTasks} · 失败/中断{" "}
              {failedTasks}
              {cancelledTasks > 0 && ` · 已取消 ${cancelledTasks}`}
            </small>
          </div>
        </div>
      </div>
      <div className="section-caption trend-heading">
        <span>
          <span className="tiny-square" />
          趋势与用量
        </span>
        <div className="segmented">
          {[7, 14, 30].map((n) => (
            <button
              key={n}
              onClick={() => applyRange(recentRange(n))}
              aria-pressed={!customOpen && range.days === n}
              className={!customOpen && range.days === n ? "active" : ""}
            >
              近 {n} 天
            </button>
          ))}
          <button
            className={customOpen ? "active" : ""}
            aria-pressed={customOpen}
            onClick={() => setCustomOpen(true)}
          >
            自定义
          </button>
        </div>
      </div>
      {customOpen && (
        <form
          className="trend-range"
          onSubmit={(event) => {
            event.preventDefault();
            if (!validDate(start) || !validDate(end)) {
              setRangeError("请选择完整的起止日期。");
            } else if (start > end) {
              setRangeError("开始日期不能晚于结束日期。");
            } else if (end > date) {
              setRangeError("结束日期不能晚于今天。");
            } else {
              applyRange({ days: 0, start, end });
            }
          }}
        >
          <label>
            开始日期
            <input
              type="date"
              required
              max={date}
              value={start}
              onChange={(event) => {
                setStart(event.target.value);
                setRangeError("");
              }}
            />
          </label>
          <label>
            结束日期
            <input
              type="date"
              required
              max={date}
              value={end}
              onChange={(event) => {
                setEnd(event.target.value);
                setRangeError("");
              }}
            />
          </label>
          <button className="button" type="submit">
            应用范围
          </button>
          {rangeError && (
            <p className="form-error" role="alert">
              {rangeError}
            </p>
          )}
        </form>
      )}
      <p className="trend-range-note">
        {range.start} — {range.end} · 仅展示已记录日期
      </p>
      <div className="chart-grid-layout">
        <section className="panel">
          <div className="section-heading">
            <div>
              <h3>Token 消耗趋势</h3>
              <p>模型输入与输出用量</p>
            </div>
            <span className="chart-total">
              {number(total)} <small>Tokens</small>
            </span>
          </div>
          {data.length ? (
            <Chart trend={data} kind="tokens" />
          ) : (
            <Empty title="暂无用量" text="所选时间段内没有已记录的用量。" />
          )}
          <div className="chart-legend">
            <span>
              <i />
              输入 Token
            </span>
            <span>
              <i className="light" />
              输出 Token
            </span>
            <a href="#models">
              查看模型 <ArrowUpRight size={13} />
            </a>
          </div>
        </section>
        <section className="panel">
          <div className="section-heading">
            <div>
              <h3>消息交互趋势</h3>
              <p>每一段沟通，都有迹可循</p>
            </div>
            <Badge>接收 / 发送</Badge>
          </div>
          {data.length ? (
            <Chart trend={data} kind="messages" />
          ) : (
            <Empty title="暂无消息" text="所选时间段内没有已记录的消息统计。" />
          )}
          <div className="chart-legend">
            <span>
              <i />
              接收消息
            </span>
            <span>
              <i className="light" />
              发出消息
            </span>
            <a href="#wechat">
              进入微信 <ArrowUpRight size={13} />
            </a>
          </div>
        </section>
      </div>
      <div className="bottom-grid">
        <section className="panel">
          <div className="section-heading">
            <h3>服务状态</h3>
            <Badge tone="green">
              {state.mock ? "离线模拟运行" : "当前实例"}
            </Badge>
          </div>
          {[
            [
              PlugsConnected,
              "微信连接",
              state.account.status === "online" ? "已连接" : "等待连接",
            ],
            [
              Cpu,
              "AI 基础插件",
              state.plugins.find((p) => p.id === "ai")?.enabled
                ? "已启用"
                : "已停用",
            ],
            [Database, "数据存储", "SQLite · 本地持久化"],
          ].map(([Icon, name, value]) => {
            const I = Icon as typeof Cpu;
            return (
              <div className="health-row" key={String(name)}>
                <I size={19} />
                <span>{String(name)}</span>
                <small>{String(value)}</small>
                <CheckCircle size={16} className="green-text" />
              </div>
            );
          })}
        </section>
        <section className="panel">
          <div className="section-heading">
            <h3>最近活动</h3>
            <a className="text-link" href="#logs">
              全部记录 <ArrowUpRight size={14} />
            </a>
          </div>
          {state.logs
            .slice(-3)
            .reverse()
            .map((log) => (
              <div className="activity" key={log.id}>
                <span className="activity-dot">
                  <Clock size={14} />
                </span>
                <div>
                  <p>{log.message}</p>
                  <small>
                    {dateTime(log.time)} · {log.component}
                  </small>
                </div>
              </div>
            ))}
        </section>
      </div>
    </div>
  );
}

import { useEffect, useRef, useState } from "react";
import {
  ArrowClockwise,
  Check,
  X,
  SlidersHorizontal,
} from "@phosphor-icons/react";
import type { PageProps } from "./types";
import { Avatar, Badge, Empty, Modal, SettingsForm, Toggle } from "./ui";
import { dateTime, request } from "./api";
import "./friend-requests.css";

type FriendRequest = {
  id: string;
  dismissed?: boolean;
  message_id: string;
  applicant: string;
  nickname: string;
  avatar: string;
  comment: string;
  created: string;
  scene: number;
  details: { name: string; value: string }[];
  status: string;
  due?: string;
  reason?: string;
};
const statuses: Record<string, string> = {
  pending: "待处理",
  scheduled: "等待自动通过",
  processing: "正在通过",
  accepted: "已通过",
  unknown: "结果待核对",
  unavailable: "参数不完整",
};
const labels: Record<string, string> = {
  fromusername: "申请人微信 ID",
  fromnickname: "昵称",
  content: "申请附言",
  scene: "来源场景",
  sex: "性别",
  province: "省份",
  city: "城市",
  country: "国家/地区",
  alias: "微信号",
  sign: "个性签名",
  signature: "个性签名",
  sourcename: "来源名称",
  sourceusername: "来源微信 ID",
  sharecardusername: "名片分享者 ID",
  sharecardnickname: "名片分享者昵称",
  bigheadimgurl: "大头像地址",
  smallheadimgurl: "头像地址",
  certinfo: "认证信息",
  regionCode: "地区编码",
};
function fieldLabel(name: string) {
  const key = name.split("/").pop()?.replace(/^@/, "") || name;
  return labels[key] || name;
}
export function FriendRequests(props: PageProps) {
  const { state, user, act, notify } = props;
  const [items, setItems] = useState<FriendRequest[]>([]);
  const [loaded, setLoaded] = useState(false),
    [error, setError] = useState(""),
    [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState(""),
    [setting, setSetting] = useState(false),
    [toggleBusy, setToggleBusy] = useState(false);
  const [lastClosed, setLastClosed] = useState<FriendRequest | null>(null);
  const [selected, setSelected] = useState<FriendRequest | null>(null);
  const [search, setSearch] = useState(""),
    [filter, setFilter] = useState("all"),
    [page, setPage] = useState(0);
  const active = useRef(true),
    revision = useRef(0),
    sending = useRef(false);
  const currentRequest = useRef<AbortController | null>(null);
  const plugin = state.plugins.find((p) => p.id === "auto_accept");
  const group = props.schema.find((g) => g.id === "auto_accept");
  async function load() {
    currentRequest.current?.abort();
    const controller = new AbortController();
    currentRequest.current = controller;
    const version = revision.current;
    try {
      const result = await request<{ items: FriendRequest[] }>(
        "/api/friend-requests",
        undefined,
        controller.signal,
      );
      if (
        active.current &&
        !controller.signal.aborted &&
        version === revision.current
      ) {
        setItems(result.items);
        setLoaded(true);
        setLoadError("");
      }
    } catch (e) {
      if (active.current && !controller.signal.aborted)
        setLoadError((e as Error).message);
    }
  }
  useEffect(() => {
    active.current = true;
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => {
      active.current = false;
      currentRequest.current?.abort();
      clearInterval(timer);
    };
  }, []);
  async function accept(item: FriendRequest) {
    if (sending.current) return;
    sending.current = true;
    revision.current++;
    setBusy(item.id);
    setError("");
    try {
      const result = await request<FriendRequest>("/api/friend-requests", {
        id: item.id,
      });
      if (active.current) {
        setItems((old) =>
          old.map((row) => (row.id === item.id ? result : row)),
        );
        setSelected((old) => (old?.id === item.id ? result : old));
        notify("好友申请已通过");
      }
    } catch (e) {
      if (active.current) setError((e as Error).message);
    } finally {
      sending.current = false;
      if (active.current) {
        setBusy("");
        void load();
      }
    }
  }
  async function dismiss(item: FriendRequest, closed = true) {
    if (sending.current) return;
    sending.current = true;
    revision.current++;
    setBusy(item.id);
    setError("");
    try {
      const result = await request<FriendRequest>("/api/friend-requests", {
        id: item.id,
        action: closed ? "dismiss" : "restore",
      });
      if (active.current) {
        setItems((old) =>
          old.map((row) => (row.id === item.id ? result : row)),
        );
        setSelected((old) => (old?.id === item.id ? null : old));
        setLastClosed(closed ? result : null);
      }
    } catch (e) {
      if (active.current) setError((e as Error).message);
    } finally {
      sending.current = false;
      if (active.current) {
        setBusy("");
        void load();
      }
    }
  }
  const selectedCurrent = items.find((i) => i.id === selected?.id) || selected;
  const shown = items.filter(
    (i) =>
      (filter === "closed"
        ? i.dismissed
        : !i.dismissed && (filter === "all" || i.status === filter)) &&
      `${i.nickname} ${i.applicant} ${i.comment}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  const pages = Math.max(1, Math.ceil(shown.length / 20));
  const currentPage = Math.min(page, pages - 1);
  const canAccept = (item: FriendRequest) =>
    !item.dismissed &&
    (item.status === "pending" || item.status === "scheduled");
  return (
    <>
      <section className="panel friend-request-settings-summary">
        <div>
          <h2>好友申请</h2>
          <p className="muted">
            {plugin?.enabled
              ? `自动通过已开启 · 延迟 ${Number(state.settings["auto_accept.delay_seconds"] ?? 10)} 秒，仍按名单、附言和来源规则筛选`
              : "自动通过未开启，新申请可在这里手动处理"}
          </p>
        </div>
        <div className="friend-request-actions">
          <button
            type="button"
            className="button"
            onClick={() => setSetting(true)}
          >
            <SlidersHorizontal size={16} />
            自动通过设置
          </button>
          <button type="button" className="button" onClick={() => void load()}>
            <ArrowClockwise size={16} />
            刷新申请
          </button>
        </div>
      </section>
      {(error || loadError) && (
        <p role="alert" className="form-error">
          {error || loadError}
        </p>
      )}
      {lastClosed && (
        <p role="status" className="friend-request-undo">
          已关闭 {lastClosed.nickname || lastClosed.applicant} 的申请
          <button
            type="button"
            className="text-link"
            disabled={!!busy}
            onClick={() => void dismiss(lastClosed, false)}
          >
            撤销关闭
          </button>
        </p>
      )}
      <section className="panel friend-request-panel">
        <div className="friend-request-filters">
          <input
            aria-label="搜索好友申请"
            placeholder="搜索昵称、微信 ID、申请附言…"
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(0);
            }}
          />
          <select
            aria-label="好友申请状态"
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value);
              setPage(0);
            }}
          >
            <option value="all">全部状态</option>
            <option value="closed">已关闭</option>
            {Object.entries(statuses).map(([id, label]) => (
              <option key={id} value={id}>
                {label}
              </option>
            ))}
          </select>
          <span className="muted">{shown.length} 条申请</span>
        </div>
        {!loaded && !error && <p role="status">正在读取好友申请…</p>}
        <div className="friend-request-list">
          {shown.slice(currentPage * 20, currentPage * 20 + 20).map((item) => (
            <article className="friend-request-card" key={item.id}>
              <Avatar
                name={item.nickname || item.applicant || "申请人"}
                src={item.avatar}
              />
              <div className="friend-request-body">
                <div className="friend-request-heading">
                  <strong>
                    {item.nickname || item.applicant || "未知申请人"}
                  </strong>
                  <Badge>
                    {item.dismissed
                      ? "已关闭"
                      : statuses[item.status] || item.status}
                  </Badge>
                  <button
                    type="button"
                    className="icon-button friend-request-dismiss"
                    disabled={!!busy || item.status === "processing"}
                    aria-label={`${item.dismissed ? "恢复" : "关闭"}${item.nickname || item.applicant}的好友申请`}
                    title={
                      item.dismissed ? "恢复显示" : "已在手机处理，关闭本地申请"
                    }
                    onClick={() => void dismiss(item, !item.dismissed)}
                  >
                    {item.dismissed ? (
                      <ArrowClockwise size={17} />
                    ) : (
                      <X size={17} />
                    )}
                  </button>
                </div>
                <p className="muted">
                  {item.applicant || "微信未提供申请人 ID"} · 场景 {item.scene}{" "}
                  · {dateTime(item.created)}
                </p>
                <p className="friend-request-comment">
                  {item.comment || "未附带验证消息"}
                </p>
                {item.due && (
                  <p className="muted">预计自动通过：{dateTime(item.due)}</p>
                )}
                {item.reason && <p className="muted">{item.reason}</p>}
                <div className="friend-request-actions">
                  <button
                    type="button"
                    className="button"
                    onClick={() => setSelected(item)}
                  >
                    查看全部内容
                  </button>
                  {canAccept(item) && (
                    <button
                      type="button"
                      className="button primary"
                      disabled={!!busy}
                      aria-label={`通过${item.nickname || item.applicant}的好友申请`}
                      onClick={() => void accept(item)}
                    >
                      <Check size={16} />
                      {busy === item.id ? "正在通过…" : "手动通过"}
                    </button>
                  )}
                </div>
              </div>
            </article>
          ))}
        </div>
        {loaded && !shown.length && (
          <Empty
            title={items.length ? "没有匹配的申请" : "暂无好友申请"}
            text="展示本地接收到并保存的好友申请；微信未返回或旧版未保存的资料无法补齐。"
          />
        )}
        {shown.length > 20 && (
          <nav className="friend-request-pagination" aria-label="好友申请分页">
            <button
              type="button"
              className="button"
              disabled={currentPage === 0}
              onClick={() => setPage(currentPage - 1)}
            >
              上一页
            </button>
            <span>
              {currentPage + 1} / {pages}
            </span>
            <button
              type="button"
              className="button"
              disabled={currentPage + 1 >= pages}
              onClick={() => setPage(currentPage + 1)}
            >
              下一页
            </button>
          </nav>
        )}
      </section>
      <Modal
        open={!!selectedCurrent}
        onClose={() => setSelected(null)}
        title="好友申请详情"
        description="微信返回的完整申请附言与资料；验证凭据由服务端加密保管"
      >
        {selectedCurrent && (
          <div className="friend-request-detail">
            <h3>
              {selectedCurrent.nickname ||
                selectedCurrent.applicant ||
                "未知申请人"}
            </h3>
            <p className="friend-request-comment">
              {selectedCurrent.comment || "未附带验证消息"}
            </p>
            <dl>
              <dt>申请人微信 ID</dt>
              <dd>{selectedCurrent.applicant || "未提供"}</dd>
              <dt>收到时间</dt>
              <dd>{dateTime(selectedCurrent.created)}</dd>
              <dt>来源场景</dt>
              <dd>{selectedCurrent.scene}</dd>
              <dt>状态</dt>
              <dd>
                {selectedCurrent.dismissed ? "已关闭" : statuses[selectedCurrent.status] || selectedCurrent.status}
              </dd>
              <dt>原消息编号</dt>
              <dd>{selectedCurrent.message_id}</dd>
            </dl>
            <h4>微信返回的全部资料</h4>
            {selectedCurrent.details.length ? (
              <dl>
                {selectedCurrent.details.map((field, index) => (
                  <div key={index}>
                    <dt>
                      {fieldLabel(field.name)}
                      <small>{field.name}</small>
                    </dt>
                    <dd>{field.value || "（空）"}</dd>
                  </div>
                ))}
              </dl>
            ) : (
              <p className="muted">此申请来自旧版记录，未保存附加资料。</p>
            )}
            {selectedCurrent.reason && <p>{selectedCurrent.reason}</p>}
            {canAccept(selectedCurrent) && (
              <button
                type="button"
                className="button primary"
                disabled={!!busy}
                onClick={() => void accept(selectedCurrent)}
              >
                {busy === selectedCurrent.id ? "正在通过…" : "手动通过"}
              </button>
            )}
          </div>
        )}
      </Modal>
      <Modal
        open={setting}
        onClose={() => setSetting(false)}
        title="自动通过设置"
        description="与好友验证插件共用配置，仅对设置生效后收到的新申请安排自动通过"
      >
        <div className="field-row">
          <div>
            <strong>自动通过好友申请</strong>
            <p className="muted">
              关闭后等待中的申请不会自动通过，仍可手动处理。
            </p>
          </div>
          <Toggle
            label="自动通过好友申请"
            checked={!!plugin?.enabled}
            disabled={toggleBusy || user.role !== "admin"}
            onChange={async (enabled) => {
              setToggleBusy(true);
              try {
                await act({ kind: "plugin", id: "auto_accept", enabled });
              } catch (e) {
                notify((e as Error).message);
              } finally {
                setToggleBusy(false);
              }
            }}
          />
        </div>
        {group && (
          <SettingsForm
            {...props}
            group={{
              ...group,
              title: "自动通过规则",
              description:
                "名单、附言、来源及延迟均由服务端检查；修改延迟应用于新到申请。",
              fields: group.fields.filter((f) => f.key !== "cooldown"),
            }}
          />
        )}
      </Modal>
    </>
  );
}

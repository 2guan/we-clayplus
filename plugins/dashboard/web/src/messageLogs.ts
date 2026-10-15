// 文件名：messageLogs.ts
// 主要功能：将已归档微信消息投影为实时日志，复用同一消息状态避免重复存储原文。
// 作者：2guan
import type { Log, State } from "./types";

export function messageLogs(state: Pick<State, "logs" | "messages" | "contacts">): Log[] {
  const contacts = new Map(state.contacts.map((contact) => [contact.id, contact]));
  const kinds: Record<string, string> = {
    text: "文字", image: "图片", voice: "语音", video: "视频", file: "文件",
    emoji: "表情", location: "位置", link: "链接", system: "系统消息",
  };
  const statuses: Record<string, string> = {
    sending: "发送中", sent: "已发送", failed: "发送失败", error: "发送失败",
    received: "已接收", revoked: "已撤回",
  };
  // 稳定 ID 使发送中、成功与失败更新同一行；不把媒体字节复制到日志。
  const messages: Log[] = state.messages.map((message) => {
    const contact = contacts.get(message.target);
    const conversation = contact?.remark || contact?.name || message.target;
    const kind = kinds[message.kind] || message.kind || "未知类型";
    const status = statuses[message.status] || message.status;
    const sender = message.sender || (message.self ? "我" : "未知发送者");
    const excerpt = (message.content || `[${kind}]`).replace(/\s+/g, " ").trim();
    return {
      id: `wechat-message:${JSON.stringify([message.target, message.id])}`,
      time: message.time,
      level: ["failed", "error"].includes(message.status) ? "ERROR" : "INFO",
      component: "微信消息",
      target: message.target,
      summary: `${message.self ? "发出" : "收到"} · ${conversation} · ${sender} · ${kind}${status && status !== "已接收" && status !== "已发送" ? ` · ${status}` : ""}  ${excerpt}`,
      message: `[${message.self ? "发出" : "收到"}][${contact?.kind === "group" ? "群聊" : "会话"}] ${conversation} (${message.target})\n${sender} · ${kind}${status ? ` · ${status}` : ""}\n${message.content || `[${kind}]`}\n消息 ID：${message.id}`,
    };
  });
  // 当前后台日志精度为秒，微信消息可能更精确；同一时间使用原始位置稳定排序。
  // 旧记录缺少更细的采集序号时不能推断真实先后，避免按 ID 随机打乱。
  return [...state.logs, ...messages]
    .map((log, index) => ({ log, index, time: Date.parse(log.time) }))
    .sort((a, b) =>
      (Number.isFinite(a.time) ? a.time : -Infinity) -
        (Number.isFinite(b.time) ? b.time : -Infinity) ||
      a.index - b.index,
    )
    .map(({ log }) => log);
}

import type { Message } from "./types";

// 归档保留两个协议回执；卡片在原通知的位置更新，下载仍使用完成消息的 ID。
export function mergeFileMessages(messages: Message[]): (Message & { displayID?: string })[] {
  const key = (m: Message, id = m.id) => `${m.target}\0${id}`;
  const notices = new Map(messages.filter(m => m.kind === "file" && m.rich?.status === "uploading").map(m => [key(m), m]));
  const replacements = new Map<string, Message>();
  const replaced = new Set<Message>();
  for (const m of messages) {
    if (m.kind !== "file" || !m.rich?.file_notice_id || m.rich.status === "uploading") continue;
    const notice = notices.get(key(m, m.rich.file_notice_id));
    if (!notice || notice.self !== m.self || notice.sender_id !== m.sender_id || (!m.sender_id && notice.sender !== m.sender) || replacements.has(key(notice))) continue;
    replacements.set(key(notice), m);
    replaced.add(m);
  }
  return messages.filter(m => !replaced.has(m)).map(m => {
    const complete = replacements.get(key(m));
    return complete ? { ...complete, time: m.time, displayID: m.id } : m;
  });
}

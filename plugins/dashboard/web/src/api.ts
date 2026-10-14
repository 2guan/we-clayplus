// 文件名：api.ts
// 主要功能：封装同源 API、附件读取与下载；不在浏览器保存模型密钥。
// 作者：2guan
export async function request<T>(path: string, body?: unknown): Promise<T> {
  // 区分后台连接中断与厂商 API 返回的业务错误。
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).catch(() => { throw new Error("无法连接 Web 后台，请检查后台服务是否运行后重试"); });
  const data = await response.json();
  if (!response.ok) {
    if (response.status === 401)
      window.dispatchEvent(new Event("weclay-session-expired"));
    throw new Error(data.error || "请求失败，请稍后重试");
  }
  return data as T;
}
export function readFile(file: File): Promise<string> {
  if (file.size > 16 * 1024 * 1024)
    return Promise.reject(new Error("请选择不超过 16 MB 的文件"));
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1]);
    reader.onerror = () => reject(new Error("文件读取失败"));
    reader.readAsDataURL(file);
  });
}
export function download(name: string, text: string, type = "text/plain") {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function dateTime(value: string) {
  if (!value) return "—";
  return new Date(value).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
export function clock(value: string) {
  if (!value) return "";
  return new Date(value).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
export const number = (n: number) => new Intl.NumberFormat("zh-CN").format(n);

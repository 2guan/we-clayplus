// 文件名：momentAnalysis.ts
// 主要功能：按朋友圈条数分页读取个人动态，并仅收集缩略图用于多模态洞察。
// 作者：2guan
import type { Moment } from "./types";
import { request } from "./api";
// 将缩略图统一限制为 512 像素，兼容 Mock 的 SVG，同时减少模型输入体积。
async function thumbnail(blob: Blob): Promise<Blob> {
  const url = URL.createObjectURL(blob);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const scale = Math.min(
      1,
      512 / Math.max(image.naturalWidth, image.naturalHeight),
    );
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("无法处理朋友圈缩略图");
    context.fillStyle = "#fff";
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob(
        (b) => (b ? resolve(b) : reject(new Error("缩略图转换失败"))),
        "image/jpeg",
        0.8,
      ),
    );
  } finally {
    URL.revokeObjectURL(url);
  }
}
export async function loadMomentAnalysis(
  target: string,
  limit: number,
  onProgress: (text: string) => void,
) {
  const items: Moment[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let cursor = "";
  // 以动态条数计数，分页去重；不使用全局时间线冒充个人朋友圈。
  while (items.length < limit) {
    onProgress(`正在读取朋友圈 · ${items.length}/${limit} 条`);
    const page = await request<{ items: Moment[]; next_cursor?: string }>(
      `/api/moments?target=${encodeURIComponent(target)}&cursor=${encodeURIComponent(cursor)}`,
    );
    for (const m of page.items || [])
      if (m.author_id === target && !ids.has(m.id)) {
        ids.add(m.id);
        items.push(m);
        if (items.length === limit) break;
      }
    const next = page.next_cursor;
    if (!next || cursors.has(next) || next === cursor) break;
    cursors.add(next);
    cursor = next;
  }
  if (!items.length) throw new Error("这位好友当前没有可读取的朋友圈内容");
  const refs = items.flatMap((m) =>
    [
      ...new Set([...(m.images || []), ...(m.thumbnail ? [m.thumbnail] : [])]),
    ].map((url, i) => ({ url, label: `朋友圈 ${m.id} 的缩略图 ${i + 1}` })),
  );
  const images: { label: string; mime: string; data: string }[] = new Array(
    refs.length,
  );
  let next = 0;
  let bytes = 0;
  let done = 0;
  // 限制并发，复用已处理解密和来源校验的同源图片代理；不读取 original_images。
  await Promise.all(
    Array.from({ length: Math.min(4, refs.length) }, async () => {
      while (next < refs.length) {
        const index = next++;
        const ref = refs[index];
        const url = /^https?:\/\//.test(ref.url)
          ? `/api/wechat-image?url=${encodeURIComponent(ref.url)}`
          : ref.url;
        if (
          !url.startsWith("/api/wechat-image?") &&
          !url.startsWith("/media/") &&
          !url.startsWith("/sample-")
        )
          throw new Error("朋友圈缩略图地址无效");
        const response = await fetch(url);
        if (!response.ok) throw new Error("朋友圈缩略图加载失败，请稍后重试");
        const blob = await thumbnail(await response.blob());
        bytes += blob.size;
        if (bytes > 12 * 1024 * 1024)
          throw new Error("缩略图总量超过 12 MB，请减少朋友圈条数");
        const data = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result).split(",")[1]);
          reader.onerror = () => reject(new Error("缩略图读取失败"));
          reader.readAsDataURL(blob);
        });
        images[index] = {
          label: ref.label,
          mime: blob.type.split(";")[0],
          data,
        };
        onProgress(`正在读取缩略图 · ${++done}/${refs.length} 张`);
      }
    }),
  );
  return { items, images };
}

// 文件名：momentAnalysis.ts
// 主要功能：按朋友圈条数分页读取个人动态，并仅收集缩略图用于多模态洞察。
// 作者：2guan
import type { Moment } from "./types";
import { request } from "./api";
// 根据图片总数逐级缩小单图尺寸并调整 JPEG 质量；大量朋友圈图最低保留 100 像素长边，
// 在不重复解码和多次有损编码的前提下进一步压低上传体积，同时保留基本场景信息。
async function thumbnail(blob: Blob, count: number): Promise<Blob> {
  const url = URL.createObjectURL(blob);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    const maxSide =
      count > 96
        ? 100
        : count > 48
          ? 160
          : count > 24
            ? 256
            : count > 8
              ? 384
              : 512;
    // 数量越多，单张质量越低；466 张时仍保留 100px 识别边长，优先节省传输与上下文载荷。
    const quality =
      count > 96
        ? 0.5
        : count > 48
          ? 0.58
          : count > 24
            ? 0.64
            : count > 8
              ? 0.7
              : 0.76;
    const scale = Math.min(
      1,
      maxSide / Math.max(image.naturalWidth, image.naturalHeight),
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
        quality,
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
  const sources = items.flatMap((m) =>
    [...new Set([...(m.images || []), ...(m.thumbnail ? [m.thumbnail] : [])])]
      .filter(Boolean)
      .map((url, i) => ({ url, label: `朋友圈 ${m.id} 的缩略图 ${i + 1}` })),
  );
  const images: { label: string; mime: string; data: string }[] = [];
  let next = 0;
  let bytes = 0;
  let done = 0;
  let skipped = 0;
  // 限制并发，复用已处理解密和来源校验的同源图片代理；不读取 original_images。
  await Promise.all(
    Array.from({ length: Math.min(3, sources.length) }, async () => {
      while (next < sources.length) {
        const index = next++;
        const ref = sources[index];
        const url = /^https?:\/\//.test(ref.url)
          ? `/api/wechat-image?url=${encodeURIComponent(ref.url)}`
          : ref.url;
        if (
          !url.startsWith("/api/wechat-image?") &&
          !url.startsWith("/media/") &&
          !url.startsWith("/sample-")
        )
          throw new Error("朋友圈缩略图地址无效");
        let response: Response | undefined;
        // 较大范围偶有微信 CDN 瞬时限流；最多重试一次，避免一张失效图中断整批文字分析。
        for (let attempt = 0; attempt < 2; attempt++) {
          try {
            response = await fetch(url, { credentials: "same-origin" });
          } catch {
            if (attempt === 1) break;
          }
          if (
            response?.ok ||
            (response && response.status < 500 && response.status !== 429)
          )
            break;
          if (attempt === 0)
            await new Promise((resolve) => setTimeout(resolve, 350));
        }
        if (response?.status === 401)
          throw new Error("后台登录已失效，请重新登录后再试");
        if (!response?.ok) {
          skipped++;
          onProgress(
            `有图片暂时无法读取，已跳过 · ${++done}/${sources.length} 张`,
          );
          continue;
        }
        let blob: Blob;
        try {
          blob = await thumbnail(await response.blob(), sources.length);
        } catch {
          skipped++;
          onProgress(
            `有图片格式无法处理，已跳过 · ${++done}/${sources.length} 张`,
          );
          continue;
        }
        bytes += blob.size;
        if (bytes > 12 * 1024 * 1024)
          throw new Error("缩略图总量超过 12 MB，请减少朋友圈条数");
        const data = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result).split(",")[1]);
          reader.onerror = () => reject(new Error("缩略图读取失败"));
          reader.readAsDataURL(blob);
        });
        images.push({
          label: ref.label,
          mime: blob.type.split(";")[0],
          data,
        });
        onProgress(`正在读取缩略图 · ${++done}/${sources.length} 张`);
      }
    }),
  );
  const compressedSize =
    bytes >= 1024 * 1024
      ? `${(bytes / (1024 * 1024)).toFixed(2)} MB`
      : `${Math.ceil(bytes / 1024)} KB`;
  onProgress(
    `缩略图压缩完成 · ${images.length} 张 / ${compressedSize}${skipped ? `，跳过 ${skipped} 张` : ""}`,
  );
  return { items, images, skippedImages: skipped };
}

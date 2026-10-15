// 文件名：moment-analysis.spec.ts
// 主要功能：验证无归档朋友圈时分页拉取、按条数截取，并向模型提交缩略图而非原图。
// 作者：2guan
import { test, expect } from "@playwright/test";
test("朋友圈分析实时分页并携带缩略图", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const r = await route.fetch();
    const s = await r.json();
    s.moments = [];
    await route.fulfill({ json: s });
  });
  const cursors: string[] = [];
  let originals = 0;
  await page.route("**/api/moments?**", async (route) => {
    const u = new URL(route.request().url());
    const cursor = u.searchParams.get("cursor") || "";
    cursors.push(cursor);
    const m = (id: string) => ({
      id,
      author_id: u.searchParams.get("target"),
      content: `动态${id}`,
      time: "2026-10-14T10:00:00+08:00",
      images: ["/sample-analysis.svg"],
      original_images: ["/sample-original.png"],
    });
    await route.fulfill({
      json: {
        items: cursor ? [m("1"), m("2"), m("3")] : [m("1")],
        next_cursor: cursor ? "older" : "next",
      },
    });
  });
  await page.route("**/sample-analysis.svg", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="30" height="30"><rect width="30" height="30" fill="green"/></svg>',
    }),
  );
  await page.route("**/sample-original.png", (route) => {
    originals++;
    return route.abort();
  });
  let body: any;
  await page.route("**/api/tasks", async (route) => {
    body = route.request().postDataJSON();
    await route.fulfill({ json: { id: "fixture-analysis" } });
  });
  await page.goto("/#insights?tab=moments");
  await page.getByRole("spinbutton", { name: "朋友圈条数" }).fill("2");
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  await expect.poll(() => body).toBeTruthy();
  expect(cursors).toEqual(["", "next"]);
  expect(body.documents.map((d: any) => d.id)).toEqual(["1", "2"]);
  expect(body.images).toHaveLength(2);
  expect(body.images[0].mime).toBe("image/jpeg");
  expect(body.images[0].data.length).toBeGreaterThan(20);
  expect(body.images[1].label).toContain("朋友圈 2");
  expect(originals).toBe(0);
  await expect(page.locator(".insight-submit")).toContainText(
    "最近一次读取 2 条朋友圈",
  );
});

test("100 条朋友圈中单张缩略图暂不可读时继续分析其余内容", async ({ page }) => {
  // 高细节图片更容易看出低质量 JPEG 压缩是否确实减少上传字节数。
  const noisySvg = `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400">${Array.from(
    { length: 1600 },
    (_, i) =>
      `<rect x="${(i % 40) * 10}" y="${Math.floor(i / 40) * 10}" width="10" height="10" fill="rgb(${(i * 37) % 256},${(i * 73) % 256},${(i * 109) % 256})"/>`,
  ).join("")}</svg>`;
  await page.route("**/api/state", async (route) => {
    const r = await route.fetch();
    const s = await r.json();
    s.moments = [];
    await route.fulfill({ json: s });
  });
  await page.route("**/api/moments?**", (route) => {
    const target = new URL(route.request().url()).searchParams.get("target");
    return route.fulfill({
      json: {
        items: Array.from({ length: 100 }, (_, i) => ({
          id: `moment-${i}`,
          author_id: target,
          content: `动态 ${i}`,
          time: "2026-10-14T10:00:00+08:00",
          images: [
            i === 99
              ? "https://mmsns.qpic.cn/expired-image.jpg"
              : `/sample-analysis-${i}.svg`,
          ],
        })),
      },
    });
  });
  await page.route("**/sample-analysis-*.svg", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: noisySvg,
    }),
  );
  await page.route("**/api/wechat-image?**", (route) =>
    route.fulfill({ status: 502, body: "fixture unavailable" }),
  );
  let body: any;
  await page.route("**/api/tasks", async (route) => {
    body = route.request().postDataJSON();
    await route.fulfill({ json: { id: "fixture-analysis-100" } });
  });
  await page.goto("/#insights?tab=moments");
  await page.getByRole("spinbutton", { name: "朋友圈条数" }).fill("100");
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  await expect.poll(() => body).toBeTruthy();
  expect(body.documents).toHaveLength(100);
  expect(body.images).toHaveLength(99);
  expect(body.prompt).toContain("有 1 张朋友圈缩略图无法读取");
  const baselineBytes = await page.evaluate(async (svg) => {
    const source = URL.createObjectURL(
      new Blob([svg], { type: "image/svg+xml" }),
    );
    try {
      const image = new Image();
      image.src = source;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = 100;
      canvas.height = 100;
      canvas.getContext("2d")!.drawImage(image, 0, 0, 100, 100);
      const blob = await new Promise<Blob>((resolve, reject) =>
        canvas.toBlob(
          (result) =>
            result
              ? resolve(result)
              : reject(new Error("baseline encode failed")),
          "image/jpeg",
          0.8,
        ),
      );
      return blob.size;
    } finally {
      URL.revokeObjectURL(source);
    }
  }, noisySvg);
  const compressedBytes =
    Math.floor((body.images[0].data.length * 3) / 4) -
    (body.images[0].data.endsWith("==")
      ? 2
      : body.images[0].data.endsWith("=")
        ? 1
        : 0);
  expect(compressedBytes).toBeLessThan(baselineBytes * 0.85);
});

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

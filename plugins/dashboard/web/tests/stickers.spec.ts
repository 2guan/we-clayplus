// 文件名：stickers.spec.ts
// 主要功能：脱机验证表情分类、元数据编辑、人工发送与手机布局。
// 作者：2guan
import { test, expect } from "@playwright/test";
for (const mobile of [false, true]) {
  test(`表情库编辑和发送 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
    await page.setViewportSize(
      mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    );
    const items = [
      {
        id: "0123456789abcdef0123456789abcdef",
        name: "开心挥手",
        kind: "custom",
        meaning: "打招呼",
        tags: "问候",
        enabled: true,
        ai_allowed: false,
        favorite: false,
        image: "/fixture-sticker.svg",
      },
      {
        id: "abcdef0123456789abcdef0123456789",
        name: "点头赞同",
        kind: "pack",
        product_id: "synthetic-pack",
        meaning: "同意",
        tags: "赞同",
        enabled: true,
        ai_allowed: false,
        favorite: true,
        image: "/fixture-sticker.svg",
      },
    ];
    await page.route("**/fixture-sticker.svg", (route) =>
      route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><circle cx="50" cy="50" r="40" fill="#ffd661"/><circle cx="35" cy="42" r="4"/><circle cx="65" cy="42" r="4"/><path d="M30 60Q50 80 70 60" stroke="#333" fill="none" stroke-width="4"/></svg>',
      }),
    );
    await page.route("**/api/stickers", async (route) => {
      if (route.request().method() === "POST") {
        const q = route.request().postDataJSON();
        Object.assign(items.find((s) => s.id === q.item.id)!, q.item);
        await route.fulfill({ json: { ok: true } });
      } else await route.fulfill({ json: items });
    });
    let sent: Record<string, unknown> | undefined;
    await page.route("**/api/action", async (route) => {
      const q = route.request().postDataJSON();
      if (q.sticker_id) {
        sent = q;
        await route.fulfill({ json: { ok: true } });
      } else await route.continue();
    });
    await page.goto("/#wechat?target=mock-contact-2");
    await page.getByRole("button", { name: "打开表情库", exact: true }).click();
    const picker = page.getByRole("dialog", { name: "选择表情包" });
    await expect(picker).toBeVisible();
    const pickerBox = await picker.boundingBox();
    expect(pickerBox!.x).toBeGreaterThanOrEqual(0);
    expect(pickerBox!.x + pickerBox!.width).toBeLessThanOrEqual(
      mobile ? 390 : 1440,
    );
    await expect.poll(() => picker.locator("img").first().evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
    await page.screenshot({
      path: mobile
        ? "/tmp/sticker-quick-mobile.png"
        : "/tmp/sticker-quick-desktop.png",
    });
    await picker.getByRole("button", { name: "发送表情：开心挥手" }).click();
    await expect.poll(() => sent?.sticker_id).toBe(items[0].id);
    await expect(picker).toHaveCount(0);
    await page.getByRole("button", { name: "打开表情库", exact: true }).click();
    await picker.getByRole("button", { name: "管理表情包" }).click();
    const dialog = page.getByRole("dialog", { name: "表情库" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "微信专辑", exact: true }).click();
    await expect(dialog.locator(".sticker-tile")).toHaveCount(1);
    await expect(
      dialog.getByRole("button", { name: "编辑表情：点头赞同" }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "全部", exact: true }).click();
    await dialog.getByRole("textbox", { name: "搜索表情" }).fill("问候");
    await expect(dialog.locator(".sticker-tile")).toHaveCount(1);
    await dialog.getByRole("button", { name: "编辑表情：开心挥手" }).click();
    await dialog
      .getByLabel("含义与适用场景")
      .fill("开心地打招呼，适合问候好友");
    await dialog.getByLabel("允许 AI 使用", { exact: true }).check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect.poll(() => items[0].ai_allowed).toBe(true);
    const box = await dialog.boundingBox();
    expect(box!.width).toBeLessThanOrEqual(mobile ? 390 : 1440);
    expect(
      await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth),
    ).toBe(true);
    await page.screenshot({
      path: mobile
        ? "/tmp/weclay-stickers-mobile.png"
        : "/tmp/weclay-stickers-desktop.png",
    });
    await dialog.getByRole("button", { name: "发送表情", exact: true }).click();
    await expect.poll(() => sent?.sticker_id).toBe(items[0].id);
    expect(sent?.target).toBe("mock-contact-2");
    await expect(dialog).toHaveCount(0);
  });
}

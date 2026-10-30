import { expect, test } from "@playwright/test";

for (const mobile of [false, true]) {
  test(`朋友圈未读角标和评论回复关系 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await page.clock.install();
    let unread = 3;
    let calls = 0;
    let writes = 0;
    const moment = {
      id: "fixture-reminder", author_id: "fixture-self", author: "本人昵称",
      avatar: "", content: "周末随手记录", images: [], liked: false, likes: 0,
      time: new Date().toISOString(),
      comments: [
        { author: "好友昵称", reply_to: "本人昵称", content: "回复留言" },
        { author: "另一位好友", content: "普通留言" },
      ],
    };
    await page.route("**/api/moments/unread", (route) => {
      calls++;
      return route.fulfill({ json: { unread } });
    });
    await page.route("**/api/moments?*", (route) => route.fulfill({ json: { items: [moment], unread, next_cursor: "" } }));
    await page.route("**/api/moments/notifications", (route) => route.fulfill({ json: { items: [], unread, notifications: [{ id: "fixture-notice", moment_id: moment.id, actor: "好友昵称", kind: "comment", content: "回复留言", original: "本人评论", created: 1793199039, read: false }] } }));
    await page.route("**/api/moments/interact", (route) => { writes++; return route.fulfill({ status: 500 }); });
    await page.goto("/#wechat");
    const entry = page.getByRole("button", { name: mobile ? "打开朋友圈" : "朋友圈", exact: true });
    await expect(entry.locator(".moment-unread")).toHaveText("3");
    expect(calls).toBe(1);
    await entry.click();
    const dialog = page.getByRole("dialog", { name: "朋友圈", exact: true });
    await expect(dialog.getByText("好友昵称回复本人昵称：", { exact: true })).toBeVisible();
    await expect(dialog.getByText("另一位好友：", { exact: true })).toBeVisible();
    await page.screenshot({ path: `/tmp/weclay-moments-reminders-${mobile ? "mobile" : "desktop"}.png` });
    await dialog.getByRole("button", { name: "朋友圈互动提醒" }).click();
    const notifications = page.getByRole("dialog", { name: "朋友圈消息", exact: true });
    await expect(notifications.getByText("好友昵称回复了你的评论", { exact: true })).toBeVisible();
    await expect(notifications.locator(".moment-feed")).toHaveCount(0);
    await notifications.getByRole("button", { name: "关闭", exact: true }).click();
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    unread = 101;
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(entry.locator(".moment-unread")).toHaveText("99+");
    unread = 0;
    await page.clock.fastForward(60_001);
    await expect(entry.locator(".moment-unread")).toHaveCount(0);
    expect(writes).toBe(0);
    expect(calls).toBeGreaterThanOrEqual(3);
  });
}

test("朋友圈计数读取失败保留已有角标并允许恢复", async ({ page }) => {
  let failed = false;
  await page.route("**/api/moments/unread", (route) => failed
    ? route.fulfill({ status: 502, json: { error: "微信读取暂时失败" } })
    : route.fulfill({ json: { unread: 2 } }));
  await page.goto("/#wechat");
  const entry = page.getByRole("button", { name: "朋友圈", exact: true });
  await expect(entry.locator(".moment-unread")).toHaveText("2");
  failed = true;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(entry).toHaveAttribute("title", /暂不可用/);
  await expect(entry.locator(".moment-unread")).toHaveText("2");
  failed = false;
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(entry).toHaveAttribute("title", "朋友圈");
});

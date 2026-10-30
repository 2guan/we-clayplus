import { expect, test } from "@playwright/test";
for (const mobile of [false, true]) test(`提醒逐条打开、逐条已读 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
  if (mobile) await page.setViewportSize({ width: 390, height: 844 });
  let unread = 2, reads = 0, details = 0, failed = true;
  const moment = { id: "123", author_id: "fixture-owner", author: "动态作者", content: "仅该条关联动态", images: [], comments: [], likes: 0, liked: false, time: new Date().toISOString() };
  await page.route("**/api/moments/unread", r => r.fulfill({ json: { unread } }));
  await page.route("**/api/moments?*", r => r.fulfill({ json: { items: [], unread: 0 } }));
  await page.route("**/api/moments/notifications", r => r.fulfill({ json: { items: [], unread, notifications: [
    { id: "fixture-notification", moment_id: "123", actor: "测试好友", kind: "comment", created: 1793199039, content: "测试回复内容", original: "本人原评论", read: false },
    { id: "fixture-other", moment_id: "456", actor: "另一好友", kind: "comment", created: 1793199000, content: "别条消息", read: false },
  ] } }));
  await page.route("**/api/moments/notifications?id=*", r => {
    details++;
    expect(new URL(r.request().url()).searchParams.get("id")).toBe("fixture-notification");
    return failed ? r.fulfill({ status: 502, json: { error: "动态读取失败" } }) : r.fulfill({ json: moment });
  });
  await page.route("**/api/moments/notifications/read", r => {
    reads++;
    expect(r.request().postDataJSON()).toEqual({ ids: ["fixture-notification"] });
    unread = 1;
    return r.fulfill({ json: { unread } });
  });
  await page.goto("/#wechat");
  const entry = page.getByRole("button", { name: mobile ? "打开朋友圈" : "朋友圈", exact: true });
  await expect(entry.locator(".moment-unread")).toHaveText("2");
  await entry.click();
  await page.getByRole("dialog", { name: "朋友圈", exact: true }).getByRole("button", { name: "朋友圈互动提醒" }).click();
  const panel = page.getByRole("dialog", { name: "朋友圈消息", exact: true });
  const card = panel.getByRole("button", { name: /测试好友回复了你的评论/ });
  await expect(card).toBeVisible();
  await expect(panel.locator(".moment-feed")).toHaveCount(0);
  expect(reads).toBe(0); expect(details).toBe(0);
  await card.click();
  await expect(panel.getByRole("alert")).toHaveText("动态读取失败");
  await expect(card).toContainText("未读");
  expect(reads).toBe(0);
  failed = false;
  await card.click();
  const detail = page.getByRole("dialog", { name: "互动消息详情", exact: true });
  await expect(detail.getByText("仅该条关联动态")).toBeVisible();
  await expect(detail.locator(".moment-feed > article")).toHaveCount(1);
  await expect(page.locator(".moment-entry .moment-unread").first()).toHaveText("1");
  expect(reads).toBe(1);
  await page.screenshot({ path: `/tmp/weclay-notification-per-click-${mobile ? "mobile" : "desktop"}.png` });
  await detail.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(card).toContainText("已读");
  await expect(panel.getByRole("button", { name: /另一好友评论了你的朋友圈/ })).toContainText("未读");
  await card.click();
  await expect(detail).toBeVisible();
  expect(reads).toBe(1);
});

// 验证会话偏好恢复及通讯录资料入口，全部使用脱机数据。
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("返回微信页恢复公众号分类及最后会话，显式链接优先", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.contacts.push({ ...state.contacts[0], id: "gh_fixture", kind: "official", name: "测试公众号" });
    await route.fulfill({ json: state });
  });
  await page.goto("/#wechat?tab=official&target=gh_fixture");
  await expect(page.locator(".chat-title-profile")).toContainText("测试公众号");
  await page.goto("/#overview");
  await page.goto("/#wechat");
  await expect(page.locator('.wechat-rail button[aria-label="公众号 / 订阅号"]')).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".chat-title-profile")).toContainText("测试公众号");
  await page.reload();
  await expect(page.locator(".chat-title-profile")).toContainText("测试公众号");
  await page.goto("/#wechat?target=mock-contact-2");
  await expect(page.locator('.wechat-rail button[aria-label="聊天"]')).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".chat-title-profile")).not.toContainText("测试公众号");
});

test("通讯录桌面与手机均可打开人员及群资料", async ({ page }) => {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/#contacts");
    await page.reload();
    const container = page.locator(width === 390 ? ".contact-cards" : ".contacts-table");
    await expect(container).toBeVisible();
    await container.getByRole("button", { name: /^查看.+资料$/ }).first().click();
    await expect(page.getByRole("dialog", { name: "人员资料", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "关闭人员资料", exact: true }).click();
    await page.getByRole("button", { name: /^微信群聊 \(/ }).click();
    await container.getByRole("button", { name: /^查看.+资料$/ }).first().click();
    const room = page.getByRole("dialog", { name: "群资料", exact: true });
    await expect(room).toBeVisible();
    await room.getByRole("button", { name: "关闭", exact: true }).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `test-results/contacts-${width}.png`, animations: "disabled" });
  }
});

import { test, expect } from "@playwright/test";

test("撤回消息保留原文气泡和标记，不可引用或重新撤回", async ({ page, request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  const mediaRequests: string[] = [];
  page.on("request", request => { if (request.url().includes("/api/message-media")) mediaRequests.push(request.url()); });
  await page.route("**/api/state", async route => {
    const response = await route.fetch(); const state = await response.json();
    const base = { target: "mock-contact-2", sender_id: "fixture-member", sender: "测试群友", source: "wechat", kind: "text", self: false, status: "revoked_by_other", time: "2026-10-27T12:00:00+08:00" };
    state.messages = [
      { ...base, id: "fixture-revoked-image", kind: "image", content: "[图片]", media: "/sample-landscape.svg" },
      { ...base, id: "fixture-revoked-uncached", kind: "image", content: "[图片]" },
      { ...base, id: "fixture-revoked-self", self: true, status: "revoked", content: "本人撤回前的原文" },
      { ...base, id: "fixture-revoked-other", content: "群友撤回前的原文" },
    ];
    await route.fulfill({ json: state });
  });
  await page.goto("/#wechat?target=mock-contact-2");
  const messages = page.locator(".message-revoked");
  await expect(messages).toHaveCount(4);
  await expect(messages.locator(".message-revoke-status")).toHaveText(Array(4).fill("已撤回 · 不计入上下文"));
  const other = messages.filter({ hasText: "群友撤回前的原文" });
  await expect(other).toContainText("测试群友");
  await other.getByRole("button", { name: "消息操作", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: "引用", exact: true })).toBeDisabled();
  await expect(page.getByRole("menuitem", { name: "撤回", exact: true })).toHaveCount(0);
  await page.keyboard.press("Escape");
  const self = messages.filter({ hasText: "本人撤回前的原文" });
  await expect(self).toHaveClass(/self/);
  await expect(messages.getByRole("img", { name: "聊天图片" })).toHaveCount(1);
  await expect(messages.filter({ hasText: "原媒体未缓存或已不可用" })).toHaveCount(1);
  expect(mediaRequests).toEqual([]);
  await page.screenshot({ path: "test-results/revoked-bubbles-desktop.png", fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(messages).toHaveCount(4);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/revoked-bubbles-mobile.png", fullPage: true, animations: "disabled" });
});

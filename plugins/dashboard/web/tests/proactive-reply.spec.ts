// 主要功能：验证主动发言只由点击触发，支持手动模式、进度、失败重试和手机布局。
import { test, expect } from "@playwright/test";

test("手动模式可主动说一句，提交防重复且保留会话模式", async ({ page }) => {
  const target = "mock-contact-2";
  let task: any = null;
  let clicks = 0;
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.account.status = "online";
    state.settings["reply.enabled"] = false;
    state.contacts.find((c: any) => c.id === target).mode = "manual";
    state.tasks = task ? [{ ...task }] : [];
    await route.fulfill({ json: state });
  });
  await page.route("**/api/action", async (route) => {
    const body = route.request().postDataJSON();
    if (body.kind === "proactive-reply") {
      expect(body).toEqual({ kind: "proactive-reply", target });
      clicks++;
      await gate;
      task = { id: "fixture-proactive-task", target, source: "wechat-proactive", kind: "chat", status: "running" };
      return route.fulfill({ json: { ok: true } });
    }
    if (body.kind === "retry-reply") {
      expect(body).toMatchObject({ target, id: "fixture-proactive-task" });
      task = { ...task, id: "fixture-proactive-retry", status: "running", reply_retryable: false, auto_retries: 0 };
      return route.fulfill({ json: { ok: true } });
    }
    return route.continue();
  });
  await page.goto(`/#wechat?target=${target}`);
  const speak = page.getByRole("button", { name: "主动说一句", exact: true });
  await expect(speak).toBeEnabled();
  expect(clicks).toBe(0);
  await speak.click();
  await expect(speak).toBeDisabled();
  await expect(speak).toHaveAttribute("aria-busy", "true");
  expect(clicks).toBe(1);
  release();
  await expect(page.locator(".chat-reply-status")).toHaveText("正在主动接话…");
  await expect(page.getByRole("button", { name: "手动", exact: true })).toHaveAttribute("aria-pressed", "true");
  task = { ...task, status: "failed", auto_retries: 1, reply_retryable: true, error: "fixture-proactive-timeout" };
  await page.reload();
  const retry = page.locator(".chat-reply-error").getByRole("button", { name: "重试", exact: true });
  await expect(retry).toBeEnabled();
  await retry.click();
  await expect(page.locator(".chat-reply-error")).toHaveCount(0);
  await expect(page.locator(".chat-reply-status")).toHaveText("正在主动接话…");
  task = { ...task, status: "completed" };
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(speak).toBeVisible();
  await expect(speak).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/proactive-reply-mobile.png", fullPage: true });
});

test("主动发言提交失败时显示原因并恢复按钮", async ({ page }) => {
  await page.route("**/api/action", async (route) => {
    if (route.request().postDataJSON().kind !== "proactive-reply") return route.continue();
    return route.fulfill({ status: 400, json: { error: "当前会话没有可用上下文" } });
  });
  await page.goto("/#wechat?target=mock-contact-1");
  const speak = page.getByRole("button", { name: "主动说一句", exact: true });
  await speak.click();
  await expect(page.getByRole("status")).toContainText("当前会话没有可用上下文");
  await expect(speak).toBeEnabled();
});

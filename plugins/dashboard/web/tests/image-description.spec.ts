import { test, expect } from "@playwright/test";

test("图片描述可查看、失败可重试，手机主题无溢出", async ({ page, request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  let phase: "failed" | "pending" | "completed" = "failed";
  let clicks = 0;
  let release: () => void = () => {};
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route("**/api/state", async route => {
    const response = await route.fetch(); const state = await response.json();
    state.account.status = "online";
    state.settings["reply.enabled"] = true; state.settings["vision.auto"] = true;
    state.contacts.find((c: { id: string }) => c.id === "mock-contact-2").mode = "auto";
    state.messages = [{ id: "fixture-image-description", target: "mock-contact-2", sender: "测试成员", sender_id: "mock-contact-2", self: true, source: "ai", status: "sent", kind: "image", content: "[图片]", media: "/sample-landscape.svg", time: "2026-10-27T12:00:00+08:00", image_generated: true,
      image_analysis: { status: phase, description: phase === "completed" ? "蓝色湖面旁有一座红色小屋，图中文字为：欢迎回来。" : undefined, error: "识图服务暂时不可用", model: "fixture-vision", updated: "2026-10-27T12:00:30+08:00" } }];
    await route.fulfill({ json: state });
  });
  await page.route("**/api/action", async route => {
    const body = route.request().postDataJSON();
    if (body.kind !== "retry-image-analysis") return route.continue();
    expect(body).toMatchObject({ target: "mock-contact-2", id: "fixture-image-description" });
    clicks++; await gate; phase = "pending";
    await route.fulfill({ json: { ok: true } });
  });
  await page.goto("/#wechat?target=mock-contact-2");
  const panel = page.locator(".image-description");
  await expect(panel).toContainText("识图失败");
  const retry = panel.getByRole("button", { name: "重试图片识别" });
  await retry.click(); await expect(retry).toBeDisabled(); await expect(retry).toHaveAttribute("aria-busy", "true");
  expect(clicks).toBe(1); release(); await expect(panel).toContainText("正在识图…");
  phase = "completed"; await page.reload();
  await panel.getByText("图片描述 · AI 生成", { exact: true }).click();
  await expect(panel).toContainText("蓝色湖面旁有一座红色小屋");
  await expect(panel).toContainText("fixture-vision");
  await page.screenshot({ path: "test-results/image-description-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await panel.getByText("图片描述 · AI 生成", { exact: true }).click();
  await expect(panel).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: "test-results/image-description-mobile.png", fullPage: true, animations: "disabled" });
  await page.evaluate(() => localStorage.setItem("weclay-theme", "dark"));
  await page.reload();
  await panel.getByText("图片描述 · AI 生成", { exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.screenshot({ path: "test-results/image-description-mobile-dark.png", fullPage: true, animations: "disabled" });
});

import { test, expect } from "@playwright/test";

test("首页显示core运行时间，刷新不归零，未知时不计时", async ({ page, request }) => {
  const original = await (await request.get("/api/state")).json();
  expect(original.uptime_seconds).toBeNull(); // Mock并没有真实core。
  let seconds: number | null = 49 * 3600 + 3 * 60;
  await page.clock.install();
  await page.route("**/api/core-uptime", route => route.fulfill({ json: { uptime_seconds: seconds } }));
  await page.route("**/api/state", async route => {
    const response = await route.fetch(); const state = await response.json();
    await route.fulfill({ json: { ...state, uptime_seconds: seconds } });
  });
  await page.goto("/#overview");
  const uptime = page.locator(".uptime-asset");
  await expect(uptime.locator("strong")).toHaveText("2天 01:03");
  await expect(uptime).toHaveAttribute("title", /core 进程/);
  await page.reload();
  await expect(uptime.locator("strong")).toHaveText("2天 01:03");
  seconds = 30;
  await page.clock.fastForward(15000);
  await expect(uptime.locator("strong")).toHaveText("00:00");
  seconds = null;
  await page.clock.fastForward(15000);
  await expect(uptime.locator("strong")).toHaveText("—");
});

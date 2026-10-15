// 文件名：navigation.spec.ts
// 主要功能：验证机器人标题、头像及侧栏收缩后的导航、持久化和手机布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test("机器人标题与头像随账号更新，图片失败时保留默认图标", async ({ page, request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  let nickname = "测试机器人";
  let photo = "https://wx.qlogo.cn/navigation-fixture";
  await page.route("**/api/state", async route => {
    const response = await route.fetch();
    const state = await response.json();
    state.account.name = nickname;
    state.account.photo = photo;
    state.account.avatar = "";
    await route.fulfill({ json: state });
  });
  await page.route("**/api/wechat-image?**", route => photo.endsWith("broken")
    ? route.fulfill({ status: 404, body: "" })
    : route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="green"/></svg>' }));
  await page.goto("/#overview");
  await expect(page).toHaveTitle("测试机器人 · 总览");
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", /navigation-fixture/);
  await expect(page.locator(".brand-mark")).toHaveText("W");
  await expect(page.locator(".brand img")).toHaveCount(0);
  await page.locator('.sidebar a[href="#settings"]').click();
  await expect(page).toHaveTitle("测试机器人 · 系统设置");
  nickname = "另一个机器人";
  photo = "https://wx.qlogo.cn/broken";
  await page.goto("/#overview");
  await page.reload();
  await expect(page).toHaveTitle("另一个机器人 · 总览");
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", /^data:image\/svg\+xml/);
  await expect(page.locator(".brand img")).toHaveCount(0);
  nickname = "";
  photo = "";
  await page.reload();
  await expect(page).toHaveTitle("we-clay · 总览");
});

test("侧栏可用键盘收缩、记住状态，手机仍显示完整抽屉", async ({ page, request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  await page.goto("/#overview");
  const state = await (await request.get("/api/state")).json();
  const sidebar = page.locator(".sidebar");
  const collapse = page.locator(".topbar").getByRole("button", { name: "收起侧边栏", exact: true });
  await collapse.focus();
  await collapse.press("Enter");
  await expect(page.getByRole("button", { name: "展开侧边栏", exact: true })).toHaveAttribute("aria-expanded", "false");
  for (const width of [1024, 1440, 2560]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(sidebar).toHaveCSS("width", "72px");
    const link = sidebar.getByRole("link", { name: "微信", exact: true });
    await expect(link).toHaveAttribute("title", "微信");
    await expect(link.locator("span")).toBeHidden();
    await link.click();
    await expect(page).toHaveTitle(`${state.account.name} · 微信`);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.screenshot({ path: "test-results/sidebar-collapsed.png" });
  await page.reload();
  await expect(sidebar).toHaveCSS("width", "72px");
  await page.setViewportSize({ width: 375, height: 812 });
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await expect(sidebar).toHaveCSS("width", "245px");
  await expect(sidebar.locator('a[href="#settings"] span')).toBeVisible();
  await expect(page.getByRole("button", { name: "展开侧边栏", exact: true })).toBeHidden();
  await page.screenshot({ path: "test-results/sidebar-mobile.png", animations: "disabled" });
  await sidebar.getByRole("link", { name: "系统设置", exact: true }).click();
  await expect(sidebar).not.toHaveClass(/open/);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "展开侧边栏", exact: true }).click();
  await expect(sidebar).toHaveCSS("width", "220px");
  await page.reload();
  await expect(sidebar).toHaveCSS("width", "220px");
});

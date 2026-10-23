import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("分类数量、刷新与返回、方向键和工作台导航保持一致", async ({ page }) => {
  await page.goto("/#plugins");
  await expect(page.getByRole("tab", { name: /全部/ })).toContainText("30");
  const games = page.getByRole("tab", { name: /游戏与聚会/ });
  await games.click();
  await expect(page.locator(".plugin-card")).toHaveCount(6);
  await expect(page).toHaveURL(/category=games/);
  await page.reload();
  await expect(games).toHaveAttribute("aria-selected", "true");
  await page.getByRole("tab", { name: /基础与 AI/ }).click();
  await expect(page.locator(".plugin-card")).toHaveCount(4);
  await page.goBack();
  await expect(games).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".plugin-card")).toHaveCount(6);
  await page
    .getByRole("button", { name: "工作台 海龟汤", exact: true })
    .click();
  await expect(page).toHaveURL(/category=games.*soup=games/);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await expect(page).not.toHaveURL(/soup=/);
  await games.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tab", { name: /剧情与冒险/ })).toBeFocused();
  await expect(page.locator(".plugin-card")).toHaveCount(4);
  await page.keyboard.press("End");
  await expect(page.getByRole("tab", { name: /扩展插件/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(
    page.getByText("这个分类暂无插件", { exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Home");
  await expect(page.locator(".plugin-card")).toHaveCount(30);
  await page.screenshot({
    path: "/tmp/weclay-plugin-categories-desktop.png",
    animations: "disabled",
  });
  await page.setViewportSize({ width: 2304, height: 1000 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/weclay-plugin-categories-wide.png",
    animations: "disabled",
  });
});

test("扩展插件自动归类，使用示例在步骤上方且按场景分组复制", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.route("**/api/plugins", async (route) => {
    const response = await route.fetch();
    const items = await response.json();
    items.push({
      ...items[0],
      id: "fixture-extension",
      name: "扩展演示",
      builtin: false,
    });
    await route.fulfill({ response, json: items });
  });
  await page.goto("/#plugins");
  await page.getByRole("tab", { name: /扩展插件/ }).click();
  await expect(page.locator(".plugin-card")).toHaveCount(1);
  await expect(
    page.getByRole("heading", { name: "扩展演示", exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: /游戏与聚会/ }).click();
  await page.getByRole("button", { name: "使用 狼人杀", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const sections = dialog.locator(".plugin-usage > section");
  await expect(
    sections.nth(1).getByRole("heading", { name: "触发词示例", exact: true }),
  ).toBeVisible();
  await expect(
    sections.nth(2).getByRole("heading", { name: "使用说明", exact: true }),
  ).toBeAttached();
  await expect(
    dialog.getByRole("heading", { name: "夜间与角色私聊", exact: true }),
  ).toBeAttached();
  const example = "游戏 abcd1234 狼聊 我们讨论一下目标";
  await dialog
    .getByRole("button", { name: `复制触发词：${example}`, exact: true })
    .click();
  await expect(dialog.getByRole("status")).toHaveText("触发词已复制");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    example,
  );
  await expect(dialog).toContainText("替换为本局实际值");
});

test("手机暗色分类与标签不溢出，事件插件不伪造触发词", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page.getByRole("tab", { name: /自动化/ }).click();
  await page
    .getByRole("button", { name: "使用 入群欢迎", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".plugin-trigger")).toHaveCount(0);
  await expect(dialog).toContainText("真实系统入群通知");
  await page.keyboard.press("Escape");
  await page.getByRole("tab", { name: /游戏与聚会/ }).click();
  await page.getByRole("button", { name: "使用 海龟汤", exact: true }).click();
  const tag = dialog.getByRole("button", {
    name: "复制触发词：来碗海龟汤",
    exact: true,
  });
  await expect(tag).toBeVisible();
  expect((await tag.boundingBox())!.width).toBeLessThan(
    (await dialog.boundingBox())!.width - 50,
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/weclay-plugin-categories-mobile.png",
    animations: "disabled",
  });
});

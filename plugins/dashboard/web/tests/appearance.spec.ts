// 文件名：appearance.spec.ts
// 主要功能：验证五套亮暗配色、全站继承、登录入口、偏好持久化和手机布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test("五种配色均支持亮暗模式，并覆盖所有页面与认证页", async ({
  page,
  request,
}) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  await page.goto("/#overview");
  await expect(page.locator("html")).toHaveAttribute("data-color", "green");
  const switchMode = async (name: string) => {
    const button = page.getByRole("button", { name: /切换主题，当前/ });
    const expected = `切换主题，当前${name}`;
    for (
      let i = 0;
      i < 3 && (await button.getAttribute("aria-label")) !== expected;
      i++
    )
      await button.click();
    await expect(button).toHaveAttribute("aria-label", expected);
  };
  const chooseColor = async (name: string) => {
    await page
      .getByRole("button", { name: "选择主题颜色", exact: true })
      .click();
    await expect(
      page.getByRole("group", { name: "主题配色", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name, exact: true }).click();
    await expect(
      page.getByRole("group", { name: "主题配色", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "选择主题颜色", exact: true }),
    ).toBeFocused();
  };
  await expect(
    page
      .getByRole("group", { name: "主题外观", exact: true })
      .getByRole("button"),
  ).toHaveCount(2);
  const accents = new Set<string>();
  await expect(page.getByRole("button", { name: "查看告警" })).toHaveCount(0);
  for (const [color, label] of [
    ["green", "绿色"],
    ["blue", "蓝色"],
    ["red", "红色"],
    ["orange", "橙色"],
    ["gray", "灰色"],
  ]) {
    for (const [mode, name] of [
      ["light", "明亮"],
      ["dark", "暗色"],
    ]) {
      await chooseColor(label);
      await switchMode(name);
      await expect(page.getByRole("dialog")).toHaveCount(0);
      await expect(page.locator("html")).toHaveAttribute("data-color", color);
      await expect(page.locator("html")).toHaveAttribute("data-theme", mode);
      accents.add(
        await page
          .locator("html")
          .evaluate((el) =>
            getComputedStyle(el).getPropertyValue("--accent").trim(),
          ),
      );
      // 导航不会重置主题；页面均使用根语义变量，错误状态保持独立颜色。
      for (const route of [
        "overview",
        "wechat",
        "contacts",
        "models",
        "plugins",
        "insights",
        "logs",
        "settings",
      ]) {
        await page.goto(`/#${route}`);
        await expect(page.locator(".main-content")).not.toBeEmpty();
        await expect(page.locator("html")).toHaveAttribute("data-color", color);
        await expect(page.locator("html")).toHaveAttribute("data-theme", mode);
      }
      await page.goto("/#overview");
      if (color === "blue" && mode === "dark")
        await page.screenshot({
          path: "test-results/blue-dark.png",
          fullPage: true,
        });
      if (color === "orange" && mode === "light")
        await page.screenshot({
          path: "test-results/orange-light.png",
          fullPage: true,
        });
    }
  }
  expect(accents.size).toBe(10);
  await page.getByRole("button", { name: "选择主题颜色", exact: true }).click();
  await page.screenshot({ path: "test-results/theme-popover.png" });
  await page.getByRole("button", { name: "灰色", exact: true }).press("Escape");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-color", "gray");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  // 手机保持两个顶栏按钮，小色板不越过屏幕；Esc 和外部点击均可收起。
  await page.setViewportSize({ width: 320, height: 760 });
  await switchMode("跟随系统");
  for (const colorScheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme });
    await expect(page.locator("html")).toHaveAttribute(
      "data-theme",
      colorScheme,
    );
  }
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "选择主题颜色", exact: true }).click();
  for (const name of ["绿色", "蓝色", "红色", "橙色", "灰色"])
    await expect(page.getByRole("button", { name, exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  const palette = page.getByRole("group", { name: "主题配色", exact: true });
  expect(
    await palette.evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return rect.left >= 0 && rect.right <= innerWidth;
    }),
  ).toBe(true);
  await page.getByRole("button", { name: "灰色", exact: true }).press("Escape");
  await expect(palette).toHaveCount(0);
  await page.getByRole("button", { name: "选择主题颜色", exact: true }).click();
  await page.locator(".connection-status").click();
  await expect(palette).toHaveCount(0);
  await page.getByRole("button", { name: "脱机 Mock", exact: true }).click();
  for (const entry of ["登录页", "初始化页"]) {
    await page.getByRole("button", { name: entry, exact: true }).click();
    await chooseColor("蓝色");
    await expect(page.locator("html")).toHaveAttribute("data-color", "blue");
    await page.getByRole("button", { name: "返回控制台", exact: true }).click();
  }
});

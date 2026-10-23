// 文件名：plugins.spec.ts
// 主要功能：验证紧凑插件卡片、两种配置编辑方式的草稿互通、持久化和错误恢复。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("插件卡片紧凑无标签，名称与图标同排", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/#plugins");
  const card = page.locator(".plugin-card").first();
  await expect(page.locator(".plugin-card")).toHaveCount(30);
  await expect(card.locator(".badge, .capabilities")).toHaveCount(0);
  const bounds = await card.boundingBox();
  const name = await card.locator("h3").boundingBox();
  const icon = await card.locator(".plugin-icon").boundingBox();
  expect(bounds!.height).toBeLessThan(205);
  expect(name!.x).toBeGreaterThan(icon!.x + icon!.width);
  expect(Math.abs(name!.y - icon!.y)).toBeLessThan(10);
  await page.screenshot({
    path: "/tmp/weclay-plugins-1440.png",
    animations: "disabled",
  });
});

test("可视化和 TOML 只转换草稿，保存后刷新与开关保持一致", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 入群欢迎" }).click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("消息内容", { exact: true })
    .fill("你好，欢迎参加讨论。");
  await dialog.getByRole("button", { name: "配置文本", exact: true }).click();
  await expect(dialog.getByLabel("TOML 配置")).toHaveValue(
    /你好，欢迎参加讨论/,
  );
  // 选项卡不能作为 form 的 submit 按钮，否则会意外保存。
  let saved = await (await request.get("/api/plugin-config?id=welcome")).json();
  expect(saved.text).not.toContain("你好，欢迎参加讨论");
  await dialog
    .getByLabel("TOML 配置")
    .fill(
      saved.text.replace("欢迎加入，一起轻松交流。", "文本修改也能回到表单"),
    );
  await dialog.getByRole("button", { name: "可视化", exact: true }).click();
  await expect(dialog.getByLabel("消息内容", { exact: true })).toHaveValue(
    "文本修改也能回到表单",
  );
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("插件配置已保存");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("switch", { name: "启用 入群欢迎" }).click();
  await expect(
    page.getByRole("switch", { name: "启用 入群欢迎" }),
  ).toHaveAttribute("aria-checked", "false");
  await page.reload();
  await page.getByRole("button", { name: "配置 入群欢迎" }).click();
  await expect(dialog.getByLabel("消息内容", { exact: true })).toHaveValue(
    "文本修改也能回到表单",
  );
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(
    page.getByRole("switch", { name: "启用 入群欢迎" }),
  ).toHaveAttribute("aria-checked", "false");
  saved = await (await request.get("/api/plugin-config?id=welcome")).json();
  expect(saved.values.welcome.message).toBe("文本修改也能回到表单");
});

test("错误文本保留且不保存，并发修改要求重新读取", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 AI 助手" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "配置文本", exact: true }).click();
  const editor = dialog.getByLabel("TOML 配置");
  await editor.fill("[routing]\ntimeout = [");
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("TOML 语法错误");
  await expect(editor).toHaveValue("[routing]\ntimeout = [");
  await editor.fill("[routing]\ntimeout = 123");
  const state = await (await request.get("/api/state")).json();
  await request.post("/api/action", {
    data: {
      kind: "settings",
      group: "routing",
      revision: state.config_revision,
      values: { timeout: 95 },
    },
  });
  await dialog.getByRole("button", { name: "可视化", exact: true }).click();
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("配置已发生变化");
  expect(
    (await (await request.get("/api/state")).json()).settings[
      "routing.timeout"
    ],
  ).toBe(95);
});

test("手机与暗色配置可用且不横向溢出", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page.getByRole("button", { name: "配置 每日摘要" }).click();
  await expect(page.getByLabel("消息数量", { exact: true })).toHaveValue("200");
  await page.screenshot({
    path: "/tmp/weclay-plugin-config-mobile.png",
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("使用入口在配置左侧，内置插件展示说明且可用键盘关闭", async ({ page }) => {
  await page.goto("/#plugins");
  const usage = page.getByRole("button", { name: "使用 AI 助手", exact: true });
  const config = page.getByRole("button", {
    name: "配置 AI 助手",
    exact: true,
  });
  expect((await usage.boundingBox())!.x).toBeLessThan(
    (await config.boundingBox())!.x,
  );
  await usage.click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("heading", { name: "插件介绍", exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("heading", { name: "使用说明", exact: true }),
  ).toBeVisible();
  await expect(dialog).toContainText("在模型中心添加模型厂商");
  await expect(dialog.locator(".plugin-trigger")).toHaveCount(6);
  await expect(dialog).toContainText("普通消息示例，不是固定命令");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
});

test("命令示例原样复制，拒绝权限时保留文本，手机暗色不溢出", async ({
  page,
  context,
}) => {
  const example = '/example say "你好，世界"';
  // 独立接口夹具模拟带命令声明的扩展，不给 Mock 插件编造真实触发词。
  await page.route("**/api/plugins", async (route) => {
    const response = await route.fetch();
    const items = await response.json();
    items[0].usage = {
      steps: ["在允许的会话发送以下命令。"],
      commands: [
        {
          name: "/example say",
          description: "回显文字",
          syntax: "/example say <text>",
          examples: [example],
        },
      ],
    };
    await route.fulfill({ response, json: items });
  });
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page
    .locator(".plugin-card")
    .first()
    .getByRole("button", { name: /^使用 / })
    .click();
  const dialog = page.getByRole("dialog");
  const trigger = dialog.getByRole("button", {
    name: `复制触发词：${example}`,
    exact: true,
  });
  await trigger.click();
  await expect(dialog.getByRole("status")).toHaveText("触发词已复制");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    example,
  );
  await page.screenshot({
    path: "/tmp/weclay-plugin-usage-mobile.png",
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  // 显式模拟浏览器拒绝，确认界面不会误报成功，也不会清除可手工复制的原文。
  await page.evaluate(() => {
    navigator.clipboard.writeText = async () => {
      throw new Error("denied");
    };
  });
  await trigger.click();
  await expect(dialog.getByRole("status")).toContainText("无法访问剪贴板");
  await expect(trigger).toContainText(example);
});

// 文件名：safety.spec.ts
// 主要功能：验证防护及清洗的可视化规则、离线场景、持久化、处理记录和手机暗色布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("默认插件与离线示例覆盖攻击、正常分析、资料隔离和清洗", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  for (const name of ["提示词防护", "上下文清洗"]) {
    await expect(
      page.getByRole("switch", { name: `启用 ${name}` }),
    ).toHaveAttribute("aria-checked", "true");
  }
  const before = await (await request.get("/api/state")).json();
  await page
    .getByRole("button", { name: "验证 提示词防护", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  const run = dialog.getByRole("button", { name: "运行离线验证", exact: true });
  const result = dialog.getByRole("region", { name: "验证结果" });
  await dialog.getByRole("button", { name: "指令覆盖", exact: true }).click();
  await run.click();
  await expect(
    result.getByRole("heading", { name: "已阻止", exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "正常摘要", exact: true }).click();
  await run.click();
  await expect(
    result.getByRole("heading", { name: "检查完成", exact: true }),
  ).toBeVisible();
  await expect(result).toContainText("周五交付");
  await dialog.getByRole("button", { name: "历史污染", exact: true }).click();
  await run.click();
  await expect(result).toContainText("疑似注入，隔离资料");
  await expect(result.locator("pre").first()).not.toContainText("系统提示词");
  await dialog.getByRole("button", { name: "推理清理", exact: true }).click();
  await run.click();
  await expect(result.locator("pre").last()).toHaveText("周四联调，周五交付。");
  await dialog.getByRole("button", { name: "提示词编辑", exact: true }).click();
  await run.click();
  await expect(
    result.getByRole("heading", { name: "检查完成", exact: true }),
  ).toBeVisible();
  const after = await (await request.get("/api/state")).json();
  expect(after.messages).toEqual(before.messages);
  expect(after.tasks).toEqual(before.tasks);
});

test("可视化自定义规则保存后生效，审计只显示规则而不含输入原文", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page
    .getByRole("button", { name: "验证 提示词防护", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "规则与范围", exact: true }).click();
  await dialog.getByRole("button", { name: "添加规则", exact: true }).click();
  await dialog.getByLabel("规则名称", { exact: true }).fill("测试暗号");
  await dialog.getByLabel("规则 ID", { exact: true }).fill("fixture-rule");
  await dialog
    .getByLabel("正则表达式", { exact: true })
    .fill("fixture-private-text");
  await dialog.getByLabel("内容来源", { exact: true }).selectOption("input");
  await dialog
    .getByRole("button", { name: "保存规则与范围", exact: true })
    .click();
  await expect(
    page.getByRole("status").filter({ hasText: "防护规则已保存" }),
  ).toBeVisible();
  const response = await request.post("/api/tasks", {
    data: { kind: "chat", prompt: "fixture-private-text 不应进入审计日志" },
  });
  expect(response.status()).toBe(400);
  await dialog.getByRole("button", { name: "处理记录", exact: true }).click();
  await dialog.getByRole("button", { name: "刷新记录", exact: true }).click();
  await expect(dialog.locator(".safety-event").first()).toContainText(
    "fixture-rule",
  );
  await expect(dialog.locator(".safety-event").first()).not.toContainText(
    "fixture-private-text",
  );
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page
    .getByRole("button", { name: "配置 提示词防护", exact: true })
    .click();
  await dialog.getByRole("button", { name: "配置文本", exact: true }).click();
  await expect(dialog.getByLabel("TOML 配置")).toHaveValue(/fixture-rule/);
});

test("手机暗色下上下文验证完整展示且不溢出", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page
    .getByRole("button", { name: "验证 上下文清洗", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "推理清理", exact: true }).click();
  await dialog
    .getByRole("button", { name: "运行离线验证", exact: true })
    .click();
  await expect(
    dialog.getByRole("heading", { name: "检查完成", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "/tmp/weclay-safety-mobile.png",
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

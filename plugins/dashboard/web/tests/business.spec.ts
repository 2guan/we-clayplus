// 文件名：business.spec.ts
// 主要功能：验证业务插件在脱机模式下的预览、发送、定时任务、素材库以及响应式布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});
test("统计预览与词云不发送消息，移动端不溢出", async ({ page, request }) => {
  const before = (await (await request.get("/api/state")).json()).messages
    .length;
  await page.goto("/#plugins");
  await page
    .getByRole("button", { name: "工作台 消息统计", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-result pre")).toContainText(
    "消息统计",
  );
  await expect(dialog.locator(".business-result pre")).toContainText("条消息");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "工作台 词云", exact: true }).click();
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-result img")).toBeVisible();
  expect(
    await dialog
      .locator(".business-result img")
      .evaluate((img: HTMLImageElement) => img.naturalWidth),
  ).toBe(1000);
  expect((await (await request.get("/api/state")).json()).messages.length).toBe(
    before,
  );
  await page.screenshot({
    path: "/tmp/weclay-business-wordcloud.png",
    animations: "disabled",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
test("菜单确认发送复用预览内容，按钮防止重复发送", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page
    .getByRole("button", { name: "工作台 今天吃什么", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  const result = dialog.locator(".business-result pre");
  await expect(result).toContainText("今天可以吃");
  const text = await result.textContent();
  page.once("dialog", (d) => d.accept());
  await dialog.getByRole("button", { name: "发送预览", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "已发送", exact: true }),
  ).toBeDisabled();
  const state = await (await request.get("/api/state")).json();
  expect(state.messages.at(-1).content).toBe(text);
  await dialog.getByRole("button", { name: "执行记录", exact: true }).click();
  await expect(dialog.locator(".business-records")).toContainText(
    "已发送工作台预览结果",
  );
});
test("计划任务保存、立即执行、重载及删除", async ({ page }) => {
  await page.goto("/#plugins");
  await page
    .getByRole("button", { name: "工作台 定时任务", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "新建任务", exact: true }).click();
  await dialog.getByLabel("任务名称", { exact: true }).fill("每日测试提醒");
  await dialog.getByLabel("任务内容 / 查询参数").fill("这是一个脱机测试提醒");
  await dialog.getByRole("button", { name: "保存任务", exact: true }).click();
  await expect(dialog.locator(".business-job")).toContainText("每日测试提醒");
  page.once("dialog", (d) => d.accept());
  await dialog.getByRole("button", { name: "立即执行", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("任务已执行");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page
    .getByRole("button", { name: "工作台 定时任务", exact: true })
    .click();
  await expect(dialog.locator(".business-job")).toContainText("每日测试提醒");
  page.once("dialog", (d) => d.accept());
  await dialog
    .getByRole("button", { name: "删除任务 每日测试提醒", exact: true })
    .click();
  await expect(dialog.locator(".business-job")).toHaveCount(0);
});
test("第三阶段插件可完整脱机预览，错误不会误报成功", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  for (const name of [
    "新闻",
    "音乐",
    "视频解析",
    "摸鱼日报",
    "入群欢迎",
    "自动通过",
    "每日摘要",
  ]) {
    await page
      .getByRole("button", { name: `工作台 ${name}`, exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    if (name === "音乐") await dialog.getByLabel("关键词 / 内容").fill("晴天");
    await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
    await expect(dialog.locator(".business-result > pre")).not.toBeEmpty({
      timeout: 15000,
    });
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  }
  await request.post("/api/action", {
    data: { kind: "mock-scenario", content: "error" },
  });
  await page.getByRole("button", { name: "工作台 新闻", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "生成预览", exact: true })
    .click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "模拟外部服务暂时不可用",
  );
});
test("表情制作与素材库在手机暗色模式可用", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page
    .getByRole("button", { name: "工作台 表情包", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("操作", { exact: true }).selectOption("create");
  await dialog.getByLabel("关键词 / 内容").fill("今天也要开心");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-result img")).toBeVisible();
  await dialog.getByRole("button", { name: "素材库", exact: true }).click();
  await dialog.getByLabel("素材名称", { exact: true }).fill("小图示例");
  await dialog.getByLabel("素材图片", { exact: true }).setInputFiles({
    name: "fixture.png",
    mimeType: "image/png",
    buffer: await page.screenshot({
      clip: { x: 0, y: 0, width: 2, height: 2 },
    }),
  });
  await dialog.getByRole("button", { name: "保存素材", exact: true }).click();
  await expect(dialog.locator(".business-assets")).toContainText("小图示例");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/weclay-business-mobile-dark.png",
    animations: "disabled",
  });
});

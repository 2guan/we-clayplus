// 文件名：message-logs.spec.ts
// 主要功能：验证消息日志的紧凑摘要、原文、时间顺序、分页和暂停快照。
// 作者：2guan
import { test, expect } from "@playwright/test";

test("微信消息并入实时日志并可过滤搜索", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.messages.push(
      {id:"fixture-log-text",target:"fixture-target",sender:"日志测试好友",content:"第一行\n第二行",kind:"text",self:false,status:"received",time:new Date().toISOString()},
      {id:"fixture-log-media",target:"fixture-target",sender:"我",content:"",kind:"image",self:true,status:"failed",time:new Date().toISOString()},
    );
    await route.fulfill({json:state});
  });
  await page.goto("/#logs");
  await page.getByLabel("日志组件", {exact:true}).selectOption("微信消息");
  await page.getByLabel("搜索日志", {exact:true}).fill("fixture-log-text");
  const row = page.locator(".log-line");
  await expect(row).toHaveCount(1);
  await expect(row).toContainText("第一行 第二行");
  await row.click();
  await expect(page.getByRole("dialog")).toContainText("日志测试好友");
  await expect(page.getByRole("dialog")).toContainText("第一行\n第二行");
  await page.getByRole("button",{name:"关闭",exact:true}).click();
  await page.getByLabel("搜索日志", {exact:true}).fill("fixture-log-media");
  await page.getByLabel("日志级别", {exact:true}).selectOption("ERROR");
  await expect(row).toHaveCount(1);
  await expect(row).toContainText("发送失败");
  await expect(row).toContainText("[图片]");
  await page.getByRole("switch",{name:"实时更新日志"}).click();
  await expect(page.locator(".terminal-head")).toContainText("已暂停展示");
});

test("最新记录在上，更早记录在列表末尾按需展开", async ({ page }) => {
  const logs=Array.from({ length: 125 }, (_, index) => ({id:`fixture-log-${index}`,time:new Date(Date.parse("2026-10-14T10:00:00Z")+index*1000).toISOString(),level:"INFO",component:"test",message:`测试日志 ${index}`}));
  await page.route("**/api/logs?**",route=>route.fulfill({json:logs}));
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.messages = [];
    state.logs = logs;
    await route.fulfill({ json: state });
  });
  await page.goto("/#logs");
  await expect(page.locator(".log-line")).toHaveCount(100);
  await expect(page.locator(".log-line").first()).toContainText("测试日志 124");
  await expect(page.locator(".log-line").last()).toContainText("测试日志 25");
  await expect(page.locator(".log-load-more")).toContainText("还有 25 条");
  await page.locator(".log-load-more").click();
  await expect(page.locator(".log-line")).toHaveCount(125);
  await expect(page.locator(".log-line").last()).toContainText("测试日志 0");
  await expect(page.locator(".log-load-more")).toHaveCount(0);
});

test("手机端日志行与筛选栏不会撑出页面", async ({ page }) => {
  const log={id:"fixture-mobile-log",time:"2026-10-14T10:00:00Z",level:"ERROR",component:"test",message:"很长的日志内容".repeat(80)};
  await page.route("**/api/logs?**",route=>route.fulfill({json:new URL(route.request().url()).searchParams.has("id")?log:[log]}));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.logs = [{
      id: "fixture-mobile-log",
      time: "2026-10-14T10:00:00Z",
      level: "ERROR",
      component: "test",
      message: "很长的日志内容".repeat(80),
    }];
    state.messages = [];
    await route.fulfill({ json: state });
  });
  await page.goto("/#logs");
  await expect(page.locator(".log-line")).toHaveCount(1);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await page.locator(".log-line").click();
  await expect(page.getByRole("dialog")).toContainText("很长的日志内容");
});

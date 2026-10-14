// 文件名：behaviors.spec.ts
// 主要功能：验证行为页展示已接通范围、默认提示词及待接入能力的真实状态。
// 作者：2guan
import { test, expect } from "@playwright/test";
test("默认策略、提示词和执行状态明确展示", async ({ page, request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  await page.goto("/#models?tab=actions");
  await page
    .getByRole("textbox", { name: "搜索行为" })
    .fill("contact.SetRemark");
  await page.locator(".behavior-item").click();
  await expect(page.getByLabel("此操作的系统提示词")).toHaveValue(
    /新的备注全文/,
  );
  await expect(page.locator(".behavior-detail")).toContainText(
    "对话工具已接通",
  );
  await page.getByRole("textbox", { name: "搜索行为" }).fill("contact.List");
  await page.locator(".behavior-item").click();
  await expect(page.getByLabel("此操作的系统提示词")).toHaveValue(/联系人列表/);
  await expect(page.locator(".behavior-detail")).toContainText("执行器待接入");
  await expect(page.locator(".behavior-detail")).toContainText(
    "开启不代表已经可以执行",
  );
});

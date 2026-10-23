import { test, expect } from "@playwright/test";

test("完整行为目录、发起人授权和未支持接口明确展示", async ({
  page,
  request,
}) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  await page.goto("/#models?tab=actions");
  const search = page.getByRole("textbox", { name: "搜索行为" });
  await search.fill("contact.SetRemark");
  await page.locator(".behavior-item").click();
  await expect(page.getByLabel("此操作的系统提示词")).toHaveValue(
    /新的备注全文/,
  );
  await expect(page.locator(".behavior-detail")).toContainText(
    "对话工具已接通",
  );
  await search.fill("contact.List");
  await page.locator(".behavior-item").click();
  await expect(page.locator(".behavior-detail")).toContainText(
    "对话工具已接通",
  );
  await expect(page.getByLabel("允许发起操作的成员 ID")).toBeVisible();
  await page.getByLabel("允许发起操作的成员 ID").fill("fixture-actor");
  await page.getByRole("button", { name: "保存行为配置" }).click();
  await page.reload();
  await search.fill("contact.List");
  await page.locator(".behavior-item").click();
  await expect(page.getByLabel("允许发起操作的成员 ID")).toHaveValue(
    "fixture-actor",
  );
  await search.fill("message.Send");
  await page.locator(".behavior-item").click();
  await expect(
    page.getByRole("switch", { name: "允许跨会话操作" }),
  ).toBeVisible();
  await expect(
    page.getByRole("switch", { name: "执行前由后台确认" }),
  ).toBeVisible();
  await search.fill("chatroom.Quit");
  await page.locator(".behavior-item").click();
  await expect(page.locator(".behavior-detail")).toContainText("底层暂不支持");
  await expect(
    page.getByRole("button", { name: "保存行为配置" }),
  ).toBeDisabled();
  await expect(page.getByRole("region", { name: "AI操作记录" })).toContainText(
    "暂无模型操作记录",
  );
});

test("后台确认显示具体操作并只提交一次", async ({ page }) => {
  let approved = false,
    submits = 0;
  await page.route("**/api/ai-actions", async (route) => {
    if (route.request().method() === "POST") {
      submits++;
      approved = true;
      await route.fulfill({ json: { status: "succeeded" } });
      return;
    }
    await route.fulfill({
      json: [
        {
          id: "fixture-action",
          operation: "chatroom.Create",
          status: approved ? "succeeded" : "pending",
          actor: "fixture-actor",
          target: "56844300491@chatroom",
          created: "2026-10-16T12:00:00+08:00",
          preview: { target: "示例群", member_refs: ["同名成员", "同名成员"] },
          reference_ids: {
            target_ref: ["56844300491@chatroom"],
            member_refs: ["wxid_member_a", "wxid_member_b"],
          },
        },
      ],
    });
  });
  await page.goto("/#models?tab=actions");
  const records = page.getByRole("region", { name: "AI操作记录" });
  await expect(records).toContainText("同名成员、同名成员");
  await expect(records).toContainText("实际目标 ID");
  await expect(records).toContainText("56844300491@chatroom");
  await expect(records).toContainText("同名成员 · wxid_member_a");
  await expect(records).toContainText("同名成员 · wxid_member_b");
  await expect(records).not.toContainText("此记录未完整保存成员唯一 ID");
  await records.getByRole("button", { name: "确认执行" }).click();
  await expect(records).toContainText("已完成");
  await expect(records.getByRole("button", { name: "确认执行" })).toHaveCount(
    0,
  );
  expect(submits).toBe(1);
});

test("旧待确认记录缺少成员唯一 ID 时明确提示", async ({ page }) => {
  await page.route("**/api/ai-actions", async (route) => {
    await route.fulfill({
      json: [{
        id: "legacy-pending-action",
        operation: "chatroom.InviteMember",
        status: "pending",
        actor: "fixture-actor",
        target: "56844300491@chatroom",
        created: "2026-10-16T12:00:00+08:00",
        preview: { target: "示例群", member_refs: ["同名成员"] },
      }],
    });
  });
  await page.goto("/#models?tab=actions");
  const records = page.getByRole("region", { name: "AI操作记录" });
  await expect(records).toContainText("实际目标 ID");
  await expect(records).toContainText("56844300491@chatroom");
  await expect(records).toContainText("此记录未完整保存成员唯一 ID");
});

test("手机行为配置可完整操作且无横向溢出", async ({ page }) => {
  await page.setViewportSize({width: 390, height: 844});
  const errors: string[]=[];page.on("pageerror",e=>errors.push(e.message));
  await page.goto("/#models?tab=actions");
  await page.getByRole("textbox",{name:"搜索行为"}).fill("chatroom.Create");
  await page.locator(".behavior-item").click();
  await expect(page.getByLabel("允许发起操作的成员 ID")).toBeVisible();
  await expect(page.getByRole("switch",{name:"执行前由后台确认"})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
  await page.screenshot({path:"/tmp/weclay-ai-behaviors-mobile.png",fullPage:true});
  await page.setViewportSize({width:1440,height:1000});
  await page.screenshot({path:"/tmp/weclay-ai-behaviors-desktop.png",fullPage:true});
  expect(errors).toEqual([]);
});

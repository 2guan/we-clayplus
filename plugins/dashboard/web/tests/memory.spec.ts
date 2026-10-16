// 文件名：memory.spec.ts
// 主要功能：脱机验证记忆审核、来源、单会话扫描、回复依据与管理员边界。
// 作者：2guan
import { test, expect, type Page } from "@playwright/test";

async function setup(page: Page, operator = false) {
  const target = "memory-fixture";
  const memory = {
    id: "fixture-memory",
    scope: "person",
    subject: target,
    target,
    category: "preference",
    key: "name",
    content: "称呼我小林",
    status: "pending",
    sensitivity: "low",
    consent: false,
    origin: "scan",
    created_at: "2026-10-15T08:00:00Z",
    confirmed_at: "",
    confirmed_by: "",
    version: 1,
    sources: [
      {
        target,
        message_id: "fixture-source",
        sender_id: target,
        hash: "fixture-hash",
      },
    ],
  };
  const items: any[] = [memory];
  const actions: any[] = [];
  const scans: any[] = [];
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.settings["memory.scan_enabled"] = true;
    state.contacts.push({
      id: target,
      name: "记忆测试联系人",
      remark: "",
      kind: "friend",
      tags: [],
      mode: "manual",
      unread: 0,
    });
    await route.fulfill({ json: state });
  });
  if (operator)
    await page.route("**/api/auth", async (route) => {
      const response = await route.fetch();
      const auth = await response.json();
      auth.user.role = "operator";
      await route.fulfill({ json: auth });
    });
  await page.route("**/api/memory**", async (route) => {
    const url = new URL(route.request().url());
    const q =
      route.request().method() === "POST"
        ? route.request().postDataJSON()
        : null;
    if (q) actions.push(q);
    if (url.pathname === "/api/memory/source")
      return route.fulfill({
        json: {
          message: {
            id: "fixture-source",
            target,
            sender_id: target,
            sender: "记忆测试联系人",
            content: "请记住，称呼我小林",
            time: "2026-10-15T08:00:00Z",
            kind: "text",
          },
        },
      });
    if (url.pathname === "/api/memory/preview")
      return route.fulfill({
        json: { items: items.filter((m) => m.status === "confirmed") },
      });
    if (url.pathname === "/api/memory/history")
      return route.fulfill({
        json: { changes: [{ action: "create", actor: "fixture-admin" }] },
      });
    if (url.pathname === "/api/memory/trace")
      return route.fulfill({
        json: {
          current_id: "fixture-current",
          history_ids: ["fixture-history"],
          memory_ids: [memory.id],
          memory_versions: [2],
        },
      });
    if (url.pathname === "/api/memory/scans") {
      if (q?.action === "start")
        scans.push({
          id: "fixture-scan",
          target: q.target,
          status: "running",
          processed: 1,
          total: 2,
          warning: "已跳过敏感内容，请到记忆页明确授权保存",
          cursor: 1,
          upper: 2,
          created_at: "2026-10-15T08:00:00Z",
          updated_at: "2026-10-15T08:00:00Z",
        });
      if (q?.action === "cancel") scans[0].status = "cancelled";
      return route.fulfill({ json: { scans, scan: scans[0] } });
    }
    if (q) {
      if (q.action === "save") {
        const old = items.find((m) => m.id === q.item.id);
        if (old) Object.assign(old, q.item, { version: old.version + 1 });
        else items.push({ ...q.item, id: "fixture-new", version: 1 });
      } else {
        const item = items.find((m) => m.id === q.id);
        item.status = (
          {
            confirm: "confirmed",
            reject: "rejected",
            expire: "expired",
            delete: "deleted",
          } as any
        )[q.action];
        item.version++;
      }
      return route.fulfill({ json: { item: items[0] } });
    }
    const filtered = items.filter(
      (m) =>
        (!url.searchParams.get("status") ||
          m.status === url.searchParams.get("status")) &&
        m.status !== "deleted",
    );
    return route.fulfill({ json: { items: filtered } });
  });
  return { target, actions, items };
}

for (const width of [1440, 390])
  test(`长期记忆审核与单会话扫描 ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const { target, actions } = await setup(page);
    await page.goto(`/#memory?target=${target}`);
    await expect(
      page.getByRole("heading", { name: "AI 记忆", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "查看来源 1" }).click();
    await expect(page.getByRole("dialog", { name: "来源消息" })).toContainText(
      "请记住，称呼我小林",
    );
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "关闭", exact: true })
      .click();
    await page.getByRole("checkbox", { name: "选择候选：称呼我小林" }).check();
    await page.getByRole("button", { name: "确认所选候选" }).click();
    await expect(page.locator(".memory-item").first()).toContainText("已确认");
    expect(actions.find((q) => q.action === "confirm")).toMatchObject({
      id: "fixture-memory",
      version: 1,
    });
    await page
      .getByRole("button", { name: "当前可用记忆", exact: true })
      .click();
    await expect(
      page.getByRole("dialog", { name: "当前可用记忆" }),
    ).toContainText("称呼我小林");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "关闭", exact: true })
      .click();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const edit = page.getByRole("dialog", { name: "编辑记忆" });
    await edit
      .getByRole("textbox", { name: "记忆内容", exact: true })
      .fill("称呼我林同学");
    await edit.getByRole("button", { name: "保存记忆", exact: true }).click();
    await expect(edit).toHaveCount(0);
    const save = actions.find((q) => q.action === "save");
    expect(save.item).not.toHaveProperty("origin");
    expect(save.item.status).toBe("pending");
    expect(save.item.version).toBe(2);
    await page.getByRole("button", { name: "拒绝", exact: true }).click();
    await expect(page.locator(".memory-item").first()).toContainText("已拒绝");
    await page
      .getByRole("button", { name: "从聊天记录更新…", exact: true })
      .click();
    const scan = page.getByRole("dialog", {
      name: "从聊天记录更新",
      exact: true,
    });
    await expect(scan).toContainText("所选聊天内容将发送至配置的模型");
    await scan.getByRole("button", { name: "发送至模型并生成候选" }).click();
    await expect(scan).toHaveCount(0);
    expect(actions.find((q) => q.action === "start").target).toBe(target);
    await expect(page.getByRole("region", { name: "扫描进度" })).toContainText(
      "1 / 2",
    );
    await expect(page.getByRole("region", { name: "扫描进度" })).toContainText("已跳过敏感内容");
    await page.getByRole("button", { name: "取消扫描", exact: true }).click();
    await expect(page.getByRole("region", { name: "扫描进度" })).toContainText(
      "已取消",
    );
    await page
      .getByRole("button", { name: "本次回复依据", exact: true })
      .click();
    await page
      .getByRole("textbox", { name: "任务 ID", exact: true })
      .fill("fixture-task");
    await page.getByRole("button", { name: "查询依据", exact: true }).click();
    await expect(page.locator(".memory-trace")).toContainText(
      "fixture-memory · v2",
    );
    await expect(page.locator(".memory-trace")).toContainText(
      "fixture-current",
    );
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `/tmp/weclay-memory-${width}.png`,
      fullPage: true,
    });
  });

test("会话入口与近期上下文命名", async ({ page }) => {
  const { target } = await setup(page);
  await page.goto(`/#wechat?target=${target}`);
  await expect(
    page.getByRole("link", { name: "记忆", exact: true }),
  ).toHaveAttribute("href", `#models?tab=memory-items&target=${target}`);
  await page
    .getByRole("button", { name: "会话资料与专属策略", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "重置近期上下文", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("switch", { name: "允许本人记住低敏偏好" }),
  ).toBeVisible();
});

test("操作员不展示长期记忆页面和入口", async ({ page }) => {
  await setup(page, true);
  await page.goto("/#memory");
  await expect(
    page.getByRole("link", { name: "AI 记忆", exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".memory-page")).toHaveCount(0);
});

test("敏感记忆的保存和模型披露分别授权", async ({ page }) => {
  const { target, actions } = await setup(page);
  await page.goto(`/#memory?target=${target}`);
  await page.getByRole("button", { name: "新增记忆", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新增记忆", exact: true });
  await dialog
    .getByRole("textbox", { name: "事实标识", exact: true })
    .fill("sensitive-note");
  await dialog
    .getByRole("textbox", { name: "记忆内容", exact: true })
    .fill("离线敏感授权示例");
  await dialog
    .getByRole("combobox", { name: "敏感级别", exact: true })
    .selectOption("sensitive");
  const consent = dialog.getByRole("checkbox", {
    name: "已获得本人明确同意保存此敏感信息",
  });
  const modelConsent = dialog.getByRole("checkbox", {
    name: "已获得本人明确同意发送给配置的模型",
  });
  await expect(consent).not.toBeChecked();
  await expect(modelConsent).not.toBeChecked();
  await consent.check();
  await dialog.getByRole("button", { name: "保存记忆", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const save = actions.find((q) => q.action === "save");
  expect(save.item.consent).toBe(true);
  expect(save.item.allow_model).toBe(false);
});

test("来源失效时显示服务端错误且不展示旧正文", async ({ page }) => {
  const { target } = await setup(page);
  await page.route("**/api/memory/source?**", (route) =>
    route.fulfill({ status: 400, json: { error: "来源消息已撤回或删除" } }),
  );
  await page.goto(`/#memory?target=${target}`);
  await page.getByRole("button", { name: "查看来源 1" }).click();
  await expect(page.getByRole("alert")).toContainText("来源消息已撤回或删除");
  await expect(page.getByRole("dialog", { name: "来源消息" })).toHaveCount(0);
});

test("记忆三个页签位于模型中心行为与任务之后，旧链接保留会话范围", async ({ page }) => {
  const { target } = await setup(page);
  await page.goto(`/#memory?target=${target}&tab=trace&task=fixture-task`);
  await expect(page).toHaveURL(new RegExp(`#models\\?target=${target}&tab=memory-trace&task=fixture-task`));
  await expect(page.getByRole("heading", {name:"模型中心",exact:true})).toBeVisible();
  await expect(page.getByRole("link", {name:"AI 记忆",exact:true})).toHaveCount(0);
  const names=await page.locator(".model-center > .tabs button").allTextContents();
  expect(names.slice(names.indexOf("行为与任务")+1)).toEqual(["记忆与审核","记忆设置","本次回复依据"]);
  await expect(page.getByRole("button",{name:"本次回复依据",exact:true})).toHaveAttribute("aria-pressed","true");
  await page.getByRole("button",{name:"记忆与审核",exact:true}).click();
  await expect(page.locator(".memory-item")).toContainText("称呼我小林");
  await expect(page.getByRole("combobox",{name:"记忆会话",exact:true})).toHaveValue(target);
  await page.getByRole("button",{name:"记忆设置",exact:true}).click();
  await expect(page.locator(".memory-page")).toContainText("长期记忆");
  await page.reload();
  await expect(page.getByRole("button",{name:"记忆设置",exact:true})).toHaveAttribute("aria-pressed","true");
});

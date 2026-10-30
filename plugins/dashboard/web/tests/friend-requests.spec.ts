import { expect, test } from "@playwright/test";
for (const mobile of [false, true])
  test(`好友申请完整资料、手动通过与自动延迟 ${mobile ? "手机" : "桌面"}`, async ({
    page,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    let item = {
      id: "a".repeat(64),
      message_id: "fixture-message",
      applicant: "fixture-peer",
      nickname: "申请昵称",
      avatar: "",
      comment: "第一行：共同好友介绍\n第二行：想加入交流\n第三行：完整末尾",
      created: new Date().toISOString(),
      scene: 3,
      status: "pending",
      details: [
        { name: "msg/@alias", value: "申请微信号" },
        { name: "msg/custom_info", value: "完整附加内容" },
        { name: "msg/@city", value: "测试城市" },
      ],
      reason: "",
    };
    let accepts = 0;
    await page.route("**/api/friend-requests", (r) => {
      if (r.request().method() === "POST") {
        accepts++;
        expect(r.request().postDataJSON()).toEqual({ id: item.id });
        item = { ...item, status: "accepted", reason: "已通过好友申请" };
        return r.fulfill({ json: item });
      }
      return r.fulfill({
        json: { items: [item], pending: item.status === "pending" ? 1 : 0 },
      });
    });
    await page.goto("/#contacts?tab=requests");
    const panel = page.locator(".friend-request-panel");
    await expect(panel.getByText("申请昵称", { exact: true })).toBeVisible();
    await expect(panel.getByText(item.comment, { exact: true })).toBeVisible();
    await panel
      .getByRole("button", { name: "查看全部内容", exact: true })
      .click();
    const detail = page.getByRole("dialog", {
      name: "好友申请详情",
      exact: true,
    });
    await expect(
      detail.getByText("完整附加内容", { exact: true }),
    ).toBeVisible();
    await expect(detail.getByText("测试城市", { exact: true })).toBeVisible();
    await page.screenshot({
      path: `/tmp/weclay-friend-request-${mobile ? "mobile" : "desktop"}.png`,
    });
    await detail.getByRole("button", { name: "手动通过", exact: true }).click();
    await expect(
      detail.getByRole("button", { name: "手动通过", exact: true }),
    ).toHaveCount(0);
    await expect(
      detail.getByText("已通过好友申请", { exact: true }),
    ).toBeVisible();
    expect(accepts).toBe(1);
    await detail.getByRole("button", { name: "关闭", exact: true }).click();
    await page
      .getByRole("button", { name: "自动通过设置", exact: true })
      .click();
    const settings = page.getByRole("dialog", {
      name: "自动通过设置",
      exact: true,
    });
    const toggle = settings.getByRole("switch", {
      name: "自动通过好友申请",
      exact: true,
    });
    const wasEnabled = await toggle.getAttribute("aria-checked");
    await toggle.click();
    await expect(toggle).toHaveAttribute(
      "aria-checked",
      wasEnabled === "true" ? "false" : "true",
    );
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", wasEnabled!);
    await settings
      .getByRole("spinbutton", { name: "处理延迟（秒）", exact: true })
      .fill("3600");
    await settings
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(settings).toContainText("已与服务器同步");
    await settings
      .getByRole("spinbutton", { name: "处理延迟（秒）", exact: true })
      .fill("10");
    await settings
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(settings).toContainText("已与服务器同步");
    await settings.getByRole("button", { name: "关闭", exact: true }).click();
    await panel
      .getByRole("textbox", { name: "搜索好友申请" })
      .fill("不存在的申请");
    await expect(
      panel.getByText("没有匹配的申请", { exact: true }),
    ).toBeVisible();
  });
test("好友申请通过失败不重复提交并保留原因", async ({ page }) => {
  let attempts = 0;
  let status = "pending";
  const item = {
    id: "b".repeat(64),
    message_id: "fixture-error",
    applicant: "fixture-error-peer",
    nickname: "失败申请",
    avatar: "",
    comment: "测试附言",
    created: new Date().toISOString(),
    scene: 3,
    details: [],
  };
  await page.route("**/api/friend-requests", (r) => {
    if (r.request().method() === "POST") {
      attempts++;
      status = "unknown";
      return r.fulfill({
        status: 409,
        json: { error: "通过结果未确认，请在手机微信核对；不会自动重试" },
      });
    }
    return r.fulfill({
      json: {
        items: [
          {
            ...item,
            status,
            reason: status === "unknown" ? "请在手机微信核对" : "",
          },
        ],
        pending: status === "pending" ? 1 : 0,
      },
    });
  });
  await page.goto("/#contacts?tab=requests");
  await page
    .getByRole("button", { name: "通过失败申请的好友申请", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("通过结果未确认");
  await expect(
    page.getByRole("article").getByText("结果待核对", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "通过失败申请的好友申请", exact: true }),
  ).toHaveCount(0);
  expect(attempts).toBe(1);
});

for (const mobile of [false, true])
  test(`关闭已在手机处理的申请并撤销 ${mobile ? "手机" : "桌面"}`, async ({
    page,
  }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    let item = {
      id: "c".repeat(64),
      message_id: "fixture-dismiss",
      applicant: "fixture-peer",
      nickname: "测试申请人",
      avatar: "",
      comment: "已在手机处理",
      created: new Date().toISOString(),
      scene: 3,
      status: "scheduled",
      due: new Date(Date.now() + 10000).toISOString(),
      details: [],
      dismissed: false,
    };
    const actions: string[] = [];
    await page.route("**/api/friend-requests", (r) => {
      if (r.request().method() === "POST") {
        const q = r.request().postDataJSON();
        expect(q.id).toBe(item.id);
        actions.push(q.action);
        expect(["dismiss", "restore"]).toContain(q.action);
        item = {
          ...item,
          dismissed: q.action === "dismiss",
          status: "pending",
          due: "",
        };
        return r.fulfill({ json: item });
      }
      return r.fulfill({
        json: { items: [item], pending: item.dismissed ? 0 : 1 },
      });
    });
    await page.goto("/#contacts?tab=requests");
    const panel = page.locator(".friend-request-panel");
    await panel
      .getByRole("button", { name: "关闭测试申请人的好友申请", exact: true })
      .click();
    await expect(panel.locator(".friend-request-card")).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "撤销关闭", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "撤销关闭", exact: true }).click();
    await expect(
      panel.getByRole("button", {
        name: "通过测试申请人的好友申请",
        exact: true,
      }),
    ).toBeVisible();
    await panel
      .getByRole("button", { name: "关闭测试申请人的好友申请", exact: true })
      .click();
    await panel
      .getByRole("combobox", { name: "好友申请状态" })
      .selectOption("closed");
    await expect(
      panel.getByRole("article").getByText("已关闭", { exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByRole("button", {
        name: "通过测试申请人的好友申请",
        exact: true,
      }),
    ).toHaveCount(0);
    await page.screenshot({
      path: `/tmp/weclay-friend-dismiss-${mobile ? "mobile" : "desktop"}.png`,
    });
    expect(actions).toEqual(["dismiss", "restore", "dismiss"]);
  });

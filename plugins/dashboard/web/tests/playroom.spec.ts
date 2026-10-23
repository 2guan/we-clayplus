import { test, expect } from "@playwright/test";
test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});
test("13项插件独立注册、配置和工作台入口", async ({ page, request }) => {
  await page.goto("/#plugins");
  for (const name of [
    "谁是卧底",
    "狼人杀",
    "阿瓦隆／阵营推理",
    "剧本杀",
    "默契测试",
    "心动剧场",
    "深夜电台／树洞",
    "每日随机副本",
    "世界探险／文字跑团",
    "每日运势／抽签",
    "趣味性格测试",
    "群聊日报／周报",
    "群梗博物馆",
  ]) {
    await expect(
      page.getByRole("button", { name: `工作台 ${name}`, exact: true }),
    ).toBeVisible();
  }
  await page
    .getByRole("button", { name: "配置 心动剧场", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("剧情尺度", { exact: true }).selectOption("暧昧");
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("已保存");
  expect(
    (await (await request.get("/api/plugin-config?id=romance")).json()).values
      .romance.tone,
  ).toBe("暧昧");
});
test("互动工作台切换页签、关闭和刷新保留插件分类", async ({ page }) => {
  await page.goto("/#plugins");
  const games = page.getByRole("tab", { name: /游戏与聚会/ });
  await games.click();
  await page.getByRole("button", { name: "工作台 谁是卧底", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "回合与重试", exact: true }).click();
  await expect(page).toHaveURL(/#plugins\?category=games&play=undercover&tab=turns$/);
  await page.reload();
  await expect(page).toHaveURL(/#plugins\?category=games&play=undercover&tab=turns$/);
  await expect(dialog.getByRole("button", { name: "回合与重试", exact: true })).toHaveClass(/active/);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(page).toHaveURL(/#plugins\?category=games$/);
  await expect(games).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(games).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".plugin-card")).toHaveCount(6);
});
test("脱机建立卧底报名局，存档刷新保留且秘密默认折叠", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins?play=undercover&tab=demo");
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("模拟消息", { exact: true }).fill("玩谁是卧底 美食");
  await dialog
    .getByRole("button", { name: "提交模拟消息", exact: true })
    .click();
  await expect
    .poll(async () => {
      const d = await (
        await request.get("/api/playroom?plugin=undercover")
      ).json();
      return d.games[0]?.status;
    })
    .toBe("lobby");
  await dialog.getByRole("button", { name: "对局与存档", exact: true }).click();
  await dialog.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(dialog.locator(".soup-card")).toHaveCount(1);
  await expect(dialog.locator(".soup-secret")).not.toHaveAttribute("open", "");
  await page.reload();
  await expect(page.getByRole("dialog").locator(".soup-card")).toHaveCount(1);
});
test("失败回合重试和未知发送必须单独核对", async ({ page }) => {
  let posted: unknown;
  await page.route("**/api/playroom*", async (route) => {
    if (route.request().method() === "POST") {
      posted = route.request().postDataJSON();
      return route.fulfill({ json: { ok: true } });
    }
    return route.fulfill({
      json: {
        games: [],
        memes: [],
        turns: [
          {
            id: "fixture-play-failed",
            plugin: "radio",
            target: "mock-contact-0",
            status: "failed",
            error: "模型暂不可用",
            retryable: true,
            cursor: 0,
            message: { sender: "演示玩家", content: "打开深夜电台" },
            updated: new Date().toISOString(),
          },
          {
            id: "fixture-play-unknown",
            plugin: "radio",
            target: "mock-contact-0",
            status: "unknown",
            error: "发送结果未确认",
            retryable: false,
            cursor: 0,
            message: { sender: "演示玩家", content: "继续" },
            deliveries: [{ text: "私密回复", private: true, sent: false }],
            updated: new Date().toISOString(),
          },
        ],
      },
    });
  });
  await page.goto("/#plugins?play=radio&tab=turns");
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("模型暂不可用", { exact: true })).toBeVisible();
  await dialog
    .getByRole("button", { name: "重试这一回合", exact: true })
    .click();
  await expect
    .poll(() => posted)
    .toEqual({ operation: "retry", id: "fixture-play-failed" });
  await expect(
    dialog.getByRole("button", { name: "重试这一回合", exact: true }),
  ).toHaveCount(1);
  page.once("dialog", (d) => d.accept());
  await dialog
    .getByRole("button", { name: "已核实当前条目发送成功", exact: true })
    .click();
  await expect
    .poll(() => posted)
    .toEqual({ operation: "confirm-sent", id: "fixture-play-unknown" });
});
test("手机暗色群梗审核与编辑", async ({ page }) => {
  let posted: unknown;
  await page.route("**/api/playroom*", async (route) => {
    if (route.request().method() === "POST") {
      posted = route.request().postDataJSON();
      return route.fulfill({ json: { ok: true } });
    }
    return route.fulfill({
      json: {
        games: [],
        turns: [],
        memes: [
          {
            id: "fixture-meme",
            target: "mock-group",
            source_id: "fixture-source",
            text: "原始公开消息",
            explanation: "周末梗：关于周末的玩笑",
            status: "pending",
            created: new Date().toISOString(),
            source_available: true,
          },
        ],
      },
    });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page
    .getByRole("button", { name: "工作台 群梗博物馆", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "群梗审核", exact: true }).click();
  await expect(dialog.getByText("原始公开消息", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "确认应用", exact: true }).click();
  await expect
    .poll(() => posted)
    .toEqual({ operation: "meme-confirm", id: "fixture-meme" });
  await dialog.getByRole("button", { name: "编辑说明", exact: true }).click();
  await dialog.getByLabel("群梗说明", { exact: true }).fill("修改后的娱乐说明");
  await dialog
    .getByRole("button", { name: "保存并待审核", exact: true })
    .click();
  await expect
    .poll(() => posted)
    .toEqual({
      operation: "meme-edit",
      id: "fixture-meme",
      text: "修改后的娱乐说明",
    });
  expect(await dialog.evaluate((e) => e.scrollWidth <= e.clientWidth + 1)).toBe(
    true,
  );
  await page.screenshot({
    path: "/tmp/weclay-playroom-mobile.png",
    animations: "disabled",
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({
    path: "/tmp/weclay-playroom-desktop.png",
    animations: "disabled",
  });
});

import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("海龟汤配置与题库编辑持久化", async ({ page, request }) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 海龟汤", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const mention = dialog.getByRole("switch", { name: "群聊开局必须 @ 本人", exact: true });
  await expect(mention).toHaveAttribute("aria-checked", "true");
  await mention.click();
  await dialog.getByLabel("主持自称", { exact: true }).fill("汤圆");
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("插件配置已保存");
  expect((await (await request.get("/api/plugin-config?id=turtlesoup")).json()).values.turtlesoup.group_start_mention).toBe(false);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "工作台 海龟汤", exact: true }).click();
  await dialog.getByRole("button", { name: "题库", exact: true }).click();
  await dialog.getByRole("button", { name: "新增题目", exact: true }).click();
  await dialog.getByLabel("标题", { exact: true }).fill("浏览器测试题");
  await dialog.getByLabel("主题", { exact: true }).fill("演示");
  await dialog.getByLabel("汤面", { exact: true }).fill("他没有带伞，衣服却没有湿。为什么？");
  await dialog.getByLabel("汤底", { exact: true }).fill("他一直在室内，通过窗户看雨，没有走到雨中。");
  await dialog.getByLabel("关键事实", { exact: true }).fill("f1|他在室内\nf2|没有走到雨中");
  await dialog.getByLabel("分级提示", { exact: true }).fill("关注他的位置。\n他没离开房间。");
  await dialog.getByRole("button", { name: "保存题目", exact: true }).click();
  await expect(dialog.locator(".soup-card")).toContainText(["浏览器测试题"]);
  const saved = await (await request.get("/api/turtlesoup")).json();
  expect(saved.puzzles.some((p: { title: string }) => p.title === "浏览器测试题")).toBe(true);
  await page.reload();
  await expect(page.getByRole("dialog").getByText("浏览器测试题", { exact: true })).toBeVisible();
});

test("脱机开局、提示和揭晓，页面隐藏汤底直到展开", async ({ page, request }) => {
  await page.goto("/#plugins?soup=demo");
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "提交模拟消息", exact: true }).click();
  await expect.poll(async () => (await (await request.get("/api/turtlesoup")).json()).games[0]?.status).toBe("playing");
  await dialog.getByRole("button", { name: "对局", exact: true }).click();
  await dialog.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(dialog.locator(".soup-card")).toHaveCount(1);
  const secret = dialog.locator(".soup-secret");
  await expect(secret).not.toHaveAttribute("open", "");
  await expect(dialog.locator(".soup-text").last()).not.toBeVisible();
  await dialog.getByRole("button", { name: "脱机体验", exact: true }).click();
  await dialog.getByLabel("模拟消息", { exact: true }).fill("提示");
  await dialog.getByRole("button", { name: "提交模拟消息", exact: true }).click();
  await expect.poll(async () => (await (await request.get("/api/turtlesoup")).json()).games[0]?.hints).toBe(1);
  await dialog.getByLabel("模拟消息", { exact: true }).fill("看汤底");
  await dialog.getByRole("button", { name: "提交模拟消息", exact: true }).click();
  await expect.poll(async () => (await (await request.get("/api/turtlesoup")).json()).games[0]?.status).toBe("revealed");
  await dialog.getByRole("button", { name: "问答与重试", exact: true }).click();
  await dialog.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(dialog.locator(".soup-answer").first()).toContainText("【汤底】");
});

test("手机暗色工作台、导入错误与失败回合重试", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page.getByRole("button", { name: "工作台 海龟汤", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "题库", exact: true }).click();
  await dialog.getByLabel("导入海龟汤题库").setInputFiles({ name: "bad.json", mimeType: "application/json", buffer: Buffer.from("not-json") });
  await expect(dialog.getByRole("alert")).toContainText("有效的 JSON");
  await expect(dialog.locator(".soup-card")).toHaveCount(3);
  expect(await dialog.evaluate((e) => e.scrollWidth <= e.clientWidth + 1)).toBe(true);
  await page.screenshot({ path: "/tmp/weclay-turtlesoup-mobile.png", animations: "disabled" });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: "/tmp/weclay-turtlesoup-desktop.png", animations: "disabled" });
  await page.route("**/api/turtlesoup*", async (route) => {
    if (route.request().method() === "POST") return route.fulfill({ json: { ok: true } });
    await route.fulfill({ json: { puzzles: [], games: [], turns: [{ id: "fixture-failed", game_id: "", message: { sender: "玩家", target: "mock-contact-0", content: "失败的游戏问题" }, command: "question", status: "failed", task_id: "", output: "", error: "模型暂时不可用", retryable: true, updated: new Date().toISOString() }] } });
  });
  await dialog.getByRole("button", { name: "问答与重试", exact: true }).click();
  await dialog.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(dialog.getByText("模型暂时不可用", { exact: true })).toBeVisible();
  const request = page.waitForRequest((r) => r.method() === "POST" && r.url().endsWith("/api/turtlesoup"));
  await dialog.getByRole("button", { name: "重试这一回合", exact: true }).click();
  expect((await request).postDataJSON()).toEqual({ operation: "retry", id: "fixture-failed" });
});

import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("点歌预览卡片、歌词和试听，发送后保留音乐结构", async ({
  page,
  request,
}) => {
  const external: string[] = [];
  page.on("request", (r) => {
    if (
      !r.url().startsWith("http://127.0.0.1:18899") &&
      /^https?:/.test(r.url())
    )
      external.push(r.url());
  });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "工作台 音乐", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("目标会话").fill("mock-contact-0");
  const contacts = (await (await request.get("/api/state")).json()).contacts;
  const target = contacts.find(
    (contact: { id: string }) => contact.id === "mock-contact-0",
  );
  await expect(dialog.getByLabel("目标会话")).toHaveValue(
    target.remark || target.name,
  );
  await expect(
    dialog.getByText("ID：mock-contact-0", { exact: true }),
  ).toBeVisible();
  await dialog.getByLabel("关键词 / 内容").fill("测试歌名");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".wechat-music")).toContainText("测试歌名");
  await expect(
    dialog.getByText("音源：脱机示例", { exact: true }),
  ).toBeVisible();
  await dialog.getByText("查看歌词", { exact: true }).click();
  await expect(dialog.locator(".wechat-music pre")).toContainText("演示歌词");
  await expect(dialog.getByLabel("歌曲试听")).toHaveAttribute(
    "src",
    "/sample-audio.wav",
  );
  await page.screenshot({
    path: "/tmp/weclay-music-desktop.png",
    animations: "disabled",
  });
  page.once("dialog", (d) => d.accept());
  await dialog.getByRole("button", { name: "发送预览", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "已发送", exact: true }),
  ).toBeDisabled();
  const state = await (await request.get("/api/state")).json();
  const m = state.messages.at(-1);
  expect(m.kind).toBe("music");
  expect(m.music.title).toContain("测试歌名");
  expect(m.music.lyric).toContain("演示歌词");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.goto("/#wechat?target=mock-contact-0");
  await expect(page.locator(".wechat-music")).toContainText("测试歌名");
  await page.locator(".wechat-music summary").click();
  await expect(page.locator(".wechat-music pre")).toContainText("演示歌词");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/weclay-music-mobile.png",
    animations: "disabled",
  });
  expect(external).toEqual([]);
});

test("文字分享和音频文件可选，失败不显示发送成功", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "工作台 音乐", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("关键词 / 内容").fill("晴天");
  await dialog.getByLabel("分享形式").selectOption("link");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-result")).toContainText("晴天");
  await expect(dialog.locator(".wechat-music")).toHaveCount(0);
  await dialog.getByLabel("分享形式").selectOption("media");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-result audio")).toHaveAttribute(
    "src",
    "/sample-audio.wav",
  );
  await request.post("/api/action", {
    data: { kind: "mock-scenario", content: "error" },
  });
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "模拟音乐服务暂时不可用",
  );
  await expect(
    dialog.getByRole("button", { name: "发送预览", exact: true }),
  ).toHaveCount(0);
});

test("音乐音源可选择并保存，保留原 Golem 自定义地址", async ({
  page,
  request,
}) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 音乐", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const source = dialog.getByLabel("音乐音源", { exact: true });
  await expect(source).toHaveValue("auto");
  await expect(source.locator("option")).toHaveText([
    "自动 · 酷我优先，抖音备用",
    "酷我音乐",
    "抖音音乐",
    "自定义服务",
  ]);
  await expect(dialog).toContainText("https://109a.cn/API/kwyy/api.php");
  await expect(dialog).toContainText("https://109a.cn/API/dyyy/api.php");
  await expect(
    dialog.getByLabel("自定义服务地址", { exact: true }),
  ).toHaveCount(0);
  await source.selectOption("custom");
  await expect(
    dialog.getByLabel("自定义服务地址", { exact: true }),
  ).toHaveValue("https://109a.cn/API/qqyy/api.php");
  await source.selectOption("douyin");
  await expect(
    dialog.getByLabel("自定义服务地址", { exact: true }),
  ).toHaveCount(0);
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await (await request.get("/api/plugin-config?id=music")).json()).values
          .music.source,
    )
    .toBe("douyin");
  await page.reload();
  await page.getByRole("button", { name: "配置 音乐", exact: true }).click();
  await expect(
    page.getByRole("dialog").getByLabel("音乐音源", { exact: true }),
  ).toHaveValue("douyin");
  const config = (
    await (await request.get("/api/plugin-config?id=music")).json()
  ).values.music;
  expect(config.endpoint).toBe("https://109a.cn/API/qqyy/api.php");
});

test("重名会话显示名称和 ID，选择后仍按准确 ID 预览", async ({
  page,
  request,
}) => {
  const contacts = (await (await request.get("/api/state")).json()).contacts;
  const target = contacts[1];
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.contacts
      .slice(0, 2)
      .forEach((contact: { name: string; remark: string }) => {
        contact.name = "同名会话";
        contact.remark = "";
      });
    await route.fulfill({ response, json: state });
  });
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "工作台 音乐", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("目标会话").fill(`同名会话 · ${target.id}`);
  await expect(dialog.getByLabel("目标会话")).toHaveValue("同名会话");
  await expect(
    dialog.getByText(`ID：${target.id}`, { exact: true }),
  ).toBeVisible();
  await dialog.getByLabel("关键词 / 内容").fill("晴天");
  const sent = page.waitForRequest(
    (request) =>
      request.url().endsWith("/api/business") && request.method() === "POST",
  );
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  expect((await sent).postDataJSON().request.target).toBe(target.id);
  await expect(dialog.locator(".wechat-music")).toContainText("晴天");
});

test("群聊点歌默认必须真实 @，可配置关闭并持久保存", async ({ page, request }) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 音乐", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const mention = dialog.getByRole("switch", { name: "群聊触发必须 @ 本人", exact: true });
  await expect(mention).toHaveAttribute("aria-checked", "true");
  await expect(dialog).toContainText("私聊、工作台和定时任务不受影响");
  await mention.click();
  await dialog.getByRole("button", { name: "配置文本", exact: true }).click();
  await expect(dialog.getByLabel("TOML 配置")).toHaveValue(/group_mention_required = false/);
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect.poll(async () => (await (await request.get("/api/plugin-config?id=music")).json()).values.music.group_mention_required).toBe(false);
  await page.reload();
  await page.getByRole("button", { name: "配置 音乐", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("switch", { name: "群聊触发必须 @ 本人", exact: true })).toHaveAttribute("aria-checked", "false");
});

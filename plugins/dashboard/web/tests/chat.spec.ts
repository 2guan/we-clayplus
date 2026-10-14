// 文件名：chat.spec.ts
// 主要功能：验证表情光标插入、粘贴附件、语音合成发送及手机端边界，使用独立脱机服务。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

// 选择模板后保存实际提示词，验证刷新、自由编辑和恢复继承，且不修改共享人设。
test("专属提示词支持人设库、自定义和继承全局", async ({ page, request }) => {
  const initial = await (await request.get("/api/state")).json();
  const persona = initial.prompts[1];
  await page.goto("/#wechat?target=mock-contact-2");
  await page.getByRole("button", { name: "专属策略", exact: true }).click();
  const dialog = page.getByRole("dialog").filter({
    has: page.getByRole("heading", { name: "会话资料与专属策略", exact: true }),
  });
  const source = dialog.getByRole("combobox", {
    name: "提示词来源",
    exact: true,
  });
  const input = dialog.getByRole("textbox", {
    name: "专属人设提示词",
    exact: true,
  });
  const save = dialog.getByRole("button", {
    name: "保存专属策略",
    exact: true,
  });
  await source.selectOption(`persona:${persona.id}`);
  await expect(input).toHaveValue(persona.content);
  await save.click();
  await expect(page.getByRole("status")).toContainText("专属策略已保存");
  await page.reload();
  await page.getByRole("button", { name: "专属策略", exact: true }).click();
  await expect(input).toHaveValue(persona.content);
  await expect(source).toHaveValue(`persona:${persona.id}`);
  await source.selectOption("custom");
  await expect(input).toHaveValue(persona.content);
  await input.fill("仅此会话使用的自定义人设");
  await save.click();
  await expect(page.getByRole("status")).toContainText("专属策略已保存");
  const custom = await (await request.get("/api/state")).json();
  expect(custom.overrides["mock-contact-2"].prompt).toBe(
    "仅此会话使用的自定义人设",
  );
  expect(custom.prompts).toEqual(initial.prompts);
  await page.reload();
  await page.getByRole("button", { name: "专属策略", exact: true }).click();
  await expect(source).toHaveValue("custom");
  await expect(input).toHaveValue("仅此会话使用的自定义人设");
  await source.selectOption("default");
  await expect(input).toHaveValue("");
  await save.click();
  await expect
    .poll(
      async () =>
        (await (await request.get("/api/state")).json()).overrides[
          "mock-contact-2"
        ].prompt,
    )
    .toBeUndefined();
});

test("表情可分类搜索并插入光标，私聊与群聊使用同一列表", async ({
  page,
  request,
}) => {
  await page.goto("/#wechat?target=mock-contact-2");
  const input = page.getByRole("textbox", { name: "消息内容", exact: true });
  await input.fill("你好世界");
  await input.press("Home");
  await input.press("ArrowRight");
  await input.press("ArrowRight");
  await page.getByRole("button", { name: "插入表情" }).click();
  const picker = page.getByRole("dialog", { name: "选择表情" });
  await picker.getByRole("button", { name: "手势", exact: true }).click();
  await picker.getByRole("button", { name: "表情：赞", exact: true }).click();
  await expect(input).toHaveValue("你好👍世界");
  await page.getByRole("button", { name: "插入表情" }).click();
  await picker.getByLabel("搜索表情").fill("咖啡");
  await expect(picker.locator(".emoji-grid button")).toHaveCount(1);
  await picker.getByRole("button", { name: "表情：咖啡" }).click();
  await expect(input).toHaveValue("你好👍☕世界");
  expect(await input.evaluate((el) => getComputedStyle(el).boxShadow)).toBe(
    "none",
  );
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(
    page.locator(".message.self").filter({ hasText: "你好👍☕世界" }),
  ).toBeVisible();
  const state = await (await request.get("/api/state")).json();
  const group = state.contacts.find(
    (item: { kind: string }) => item.kind === "group",
  );
  await expect(
    page.locator(".conversation").filter({ hasText: group.name }),
  ).toBeVisible();
  // 聊天、公众号/订阅号和朋友圈各有独立入口，群聊仍与好友共用聊天列表。
  await expect(page.locator(".wechat-rail button")).toHaveCount(3);
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await page.getByRole("button", { name: "插入表情" }).click();
    await expect(picker).toBeVisible();
    expect(
      await picker.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return (
          rect.left >= 0 &&
          rect.right <= innerWidth &&
          rect.top >= 0 &&
          rect.bottom <= innerHeight
        );
      }),
    ).toBe(true);
    await page.getByLabel("搜索表情").press("Escape");
    await expect(picker).not.toBeVisible();
    await expect(input).toBeFocused();
  }
});

test("粘贴图片与文件先预览，发送保留内容并可移除", async ({
  page,
  request,
}) => {
  await page.goto("/#wechat?target=mock-contact-2");
  const input = page.getByRole("textbox", { name: "消息内容", exact: true });
  const before = (await (await request.get("/api/state")).json()).messages
    .length;
  // 浏览器级剪贴板事件模拟，不读取系统剪贴板或用户文件。
  await input.evaluate((el) => {
    const transfer = new DataTransfer();
    const png = Uint8Array.from(
      atob(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=",
      ),
      (c) => c.charCodeAt(0),
    );
    transfer.items.add(new File([png], "粘贴图片.png", { type: "image/png" }));
    transfer.items.add(
      new File(["脱机文件内容"], "粘贴文件.txt", { type: "text/plain" }),
    );
    transfer.items.add(
      new File(["remove"], "移除.txt", { type: "text/plain" }),
    );
    el.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: transfer,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(
    page.locator(".attachment-queue .attachment-preview"),
  ).toHaveCount(3);
  await expect(page.getByAltText("待发送图片")).toBeVisible();
  expect((await (await request.get("/api/state")).json()).messages.length).toBe(
    before,
  );
  await page
    .getByRole("button", { name: "移除附件 移除.txt", exact: true })
    .click();
  await input.fill("附件说明");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(page.locator(".attachment-queue")).toHaveCount(0);
  await expect(
    page.locator(".message.self").filter({ hasText: "附件说明" }),
  ).toBeVisible();
  const state = await (await request.get("/api/state")).json();
  const sent = state.messages.filter(
    (m: { self: boolean; content: string }) =>
      m.self &&
      ["粘贴图片.png", "粘贴文件.txt", "附件说明"].includes(m.content),
  );
  expect(sent).toHaveLength(3);
  expect(
    sent.every(
      (m: { target: string; status: string }) =>
        m.target === "mock-contact-2" && m.status === "sent",
    ),
  ).toBe(true);
  const file = sent.find((m: { kind: string }) => m.kind === "file");
  expect(await (await request.get(file.media)).text()).toBe("脱机文件内容");
  const image = sent.find((m: { kind: string }) => m.kind === "image");
  expect(
    (await (await request.get(image.media)).body()).subarray(1, 4).toString(),
  ).toBe("PNG");
});

test("文字语音先试听再发送，生成失败不能发送", async ({ page, request }) => {
  await page.goto("/#wechat?target=mock-contact-2");
  await page.getByRole("button", { name: "文字转语音" }).click();
  const dialog = page.getByRole("dialog", { name: "发送语音", exact: true });
  await dialog.getByLabel("语音文字").fill("这是一条脱机语音");
  await dialog.getByRole("button", { name: "合成试听" }).click();
  await expect(dialog.locator("audio")).toBeVisible();
  await expect
    .poll(async () =>
      dialog.locator("audio").evaluate((el: HTMLAudioElement) => el.readyState),
    )
    .toBeGreaterThan(0);
  let state = await (await request.get("/api/state")).json();
  expect(
    state.messages.some(
      (m: { content: string }) => m.content === "这是一条脱机语音",
    ),
  ).toBe(false);
  await dialog.getByRole("button", { name: "发送语音", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.locator(".message.self .voice-message")).toHaveCount(1);
  state = await (await request.get("/api/state")).json();
  const voice = state.messages.find(
    (m: { content: string }) => m.content === "这是一条脱机语音",
  );
  expect(voice).toMatchObject({
    target: "mock-contact-2",
    kind: "audio",
    status: "sent",
  });
  await request.post("/api/action", {
    data: { kind: "mock-scenario", content: "error" },
  });
  await page.reload();
  await page.getByRole("button", { name: "文字转语音" }).click();
  await dialog.getByLabel("语音文字").fill("模拟生成失败");
  await dialog.getByRole("button", { name: "合成试听" }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "发送语音", exact: true }),
  ).toHaveCount(0);
});

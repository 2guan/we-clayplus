// 文件名：wechat-data.spec.ts
// 主要功能：验证真实头像渲染、会话可见性及打开朋友圈时主动读取。
// 作者：2guan
import { test, expect } from "@playwright/test";

test("账号头像与朋友圈读取入口", async ({ page }) => {
  let fetched = false;
  let timeline: unknown[] = [];
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.mock = false;
    state.account.photo = "https://wx.qlogo.cn/fixture-avatar";
    state.account.moment_cover = "https://shmmsns.qpic.cn/fixture-cover";
    state.contacts[0].avatar = "https://wx.qlogo.cn/fixture-friend";
    state.contacts[1].kind = "official";
    state.moments[0].link = "https://example.com/article";
    state.moments[0].title = "测试分享文章";
    timeline = state.moments;
    await route.fulfill({ json: state });
  });
  await page.route("**/api/wechat-image?*", (route) =>
    route.fulfill({
      contentType: "image/png",
      body: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  );
  await page.route("**/api/moments?*", async (route) => {
    fetched = true;
    await route.fulfill({ json: { items: timeline, unread: 0 } });
  });
  await page.goto("/#overview");
  await expect(page.locator(".account-identity .avatar img")).toBeVisible();
  await page.goto("/#wechat");
  await expect(page.locator(".conversation").first()).toBeVisible();
  await expect(page.locator(".conversation .avatar img").first()).toBeVisible();
  await page
    .getByRole("button", { name: "公众号 / 订阅号", exact: true })
    .click();
  await expect(page.locator(".contact-kind").first()).toHaveText(
    "公众号 / 订阅号",
  );
  await page.getByRole("button", { name: "朋友圈", exact: true }).click();
  await expect.poll(() => fetched).toBe(true);
  await expect(
    page.getByRole("button", { name: "刷新朋友圈", exact: true }),
  ).toBeEnabled();
  await expect(
    page.getByText("把日子过成喜欢的样子。", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: /测试分享文章/ }),
  ).toHaveAttribute("href", "https://example.com/article");
  await expect(page.locator(".moment-cover-photo")).toHaveAttribute(
    "src",
    /fixture-cover/,
  );
});

// 同一消息可包含多篇公众号文章；不用 HTML 注入展示，点击保持真实 URL。
test("公众号文章保留可点击卡片", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.contacts[0].kind = "official";
    state.contacts[0].name = "测试公众号";
    state.messages = [
      {
        id: "fixture-article",
        target: state.contacts[0].id,
        sender: "测试公众号",
        kind: "link",
        content: "测试文章",
        self: false,
        status: "sent",
        source: "wechat",
        time: new Date().toISOString(),
        publisher: "测试公众号",
        articles: [
          {
            title: "测试文章",
            description: "可点击的文章摘要",
            url: "https://example.com/article",
            image: "/api/message-thumbnail?id=fixture-thumb",
          },
          { title: "另一篇文章", url: "https://example.com/second" },
        ],
      },
    ];
    await route.fulfill({ json: state });
  });
  await page.route("**/api/message-thumbnail?id=fixture-thumb", (route) =>
    route.fulfill({
      contentType: "image/png",
      body: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1cAAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  );
  await page.goto("/#wechat?target=mock-contact-0");
  await expect(page.locator(".message-article-compact img")).toBeVisible();
  await expect(page.locator(".message-article-compact img")).toHaveAttribute(
    "src",
    "/api/message-thumbnail?id=fixture-thumb",
  );
  await expect(page.locator(".message-article")).toHaveCount(2);
  await expect(page.getByRole("link", { name: /测试文章/ })).toHaveAttribute(
    "href",
    "https://example.com/article",
  );
  await expect(page.getByRole("link", { name: /另一篇文章/ })).toHaveAttribute(
    "target",
    "_blank",
  );
});

// 分类不移动或复制归档；桌面、手机、地址刷新都应保持一致。
test("聊天与公众号分类隔离并支持手机入口", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    const base = state.contacts[0];
    state.contacts = ["friend", "group", "system", "official"].map(
      (kind, i) => ({
        ...base,
        id: `category-${i}`,
        name: `分类测试-${kind}`,
        remark: "",
        kind,
        preview: "测试消息",
        time: new Date().toISOString(),
        unread: 1,
      }),
    );
    state.messages = [];
    await route.fulfill({ json: state });
  });
  await page.route("**/api/action", (route) =>
    route.fulfill({ json: { ok: true } }),
  );
  await page.goto("/#wechat");
  await expect(page.locator(".conversation")).toHaveCount(3);
  await expect(
    page.locator(".conversation").filter({ hasText: "分类测试-official" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "公众号 / 订阅号", exact: true })
    .click();
  await expect(page.locator(".conversation")).toHaveCount(1);
  await expect(page.locator(".conversation")).toContainText(
    "分类测试-official",
  );
  await page.locator(".conversation").click();
  await expect(page.locator(".chat-title h2")).toHaveText("分类测试-official");
  await page.reload();
  await expect(
    page.locator(".wechat-rail button[aria-label='公众号 / 订阅号']"),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".conversation")).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "返回会话列表", exact: true }).click();
  await page.getByRole("button", { name: "微信", exact: true }).click();
  await expect(page.locator(".conversation")).toHaveCount(3);
  await page
    .getByRole("button", { name: "公众号 / 订阅号", exact: true })
    .click();
  await expect(page.locator(".conversation")).toHaveCount(1);
});

test("微信结构化消息与移动端布局", async ({ page, request }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  // 先读取独立测试快照，页面切换时不再从可能已释放的拦截响应取 JSON。
  const fixture = await (await request.get("/api/state")).json();
  await page.route("**/api/state", async (route) => {
    const state = structuredClone(fixture);
    state.contacts = [
      {
        ...state.contacts[0],
        id: "fixture-peer",
        kind: "friend",
        name: "测试好友",
      },
    ];
    const base = {
      target: "fixture-peer",
      sender: "测试好友",
      self: false,
      source: "wechat",
      status: "sent",
      time: new Date().toISOString(),
    };
    state.messages = [
      { ...base, id: "pat", kind: "system", content: "测试好友拍了拍我" },
      {
        ...base,
        id: "transfer",
        kind: "transfer",
        content: "[转账]",
        rich: { title: "微信转账", amount: "￥1.00", status: "转账通知" },
      },
      {
        ...base,
        id: "redpacket",
        kind: "redpacket",
        content: "[红包]",
        rich: {
          title: "微信红包",
          description: "测试祝福",
          status: "请在微信中查看领取状态",
        },
      },
      {
        ...base,
        id: "location",
        kind: "location",
        content: "位置",
        rich: {
          title: "测试公园",
          description: "测试地址",
          latitude: 30,
          longitude: 120,
        },
      },
      {
        ...base,
        id: "quote",
        kind: "quote",
        content: "这是回复[微笑]",
        rich: {
          author: "测试好友",
          quote: "这是引用原文",
          quote_id: "audio",
          quote_type: 34,
        },
      },
      {
        ...base,
        id: "emoji",
        kind: "emoji",
        content: "表情包",
        rich: {
          title: "自定义表情",
          image: "/api/message-media?target=fixture-peer&id=emoji",
        },
      },
      { ...base, id: "short-in", kind: "text", content: "好" },
      {
        ...base,
        id: "short-out",
        kind: "text",
        content: "好",
        self: true,
        source: "manual",
      },
      { ...base, id: "image", kind: "image", content: "图片" },
      { ...base, id: "audio", kind: "audio", content: "语音", duration: 2 },
    ];
    await route.fulfill({ json: state });
  });
  await page.route("**/api/message-media?*", (route) =>
    route.fulfill({
      contentType: "image/png",
      body: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  );
  await page.goto("/#wechat?target=fixture-peer");
  await expect(page.locator(".wechat-payment.transfer")).toContainText(
    "￥1.00",
  );
  await expect(page.locator(".wechat-payment.redpacket")).toContainText(
    "测试祝福",
  );
  await expect(page.locator(".wechat-payment a")).toHaveCount(0);
  await expect(page.locator(".wechat-quote blockquote")).toContainText(
    "这是引用原文",
  );
  await expect(
    page.getByRole("img", { name: "[微笑]", exact: true }),
  ).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "在地图中查看" }),
  ).toHaveAttribute("href", /position=120,30/);
  await expect(
    page.getByRole("button", { name: "查看图片", exact: true }),
  ).toHaveCount(1);
  await expect(
    page.getByRole("button", { name: "查看表情包", exact: true }),
  ).toHaveCount(1);
  // 同样一个字，发出气泡不能被下方状态撑得比收到气泡更宽。
  const incomingShort = page
    .locator(".message:not(.self) .bubble-text")
    .filter({ hasText: /^好$/ });
  const outgoingShort = page
    .locator(".message.self .bubble-text")
    .filter({ hasText: /^好$/ });
  expect(
    Math.abs(
      (await incomingShort.boundingBox())!.width -
        (await outgoingShort.boundingBox())!.width,
    ),
  ).toBeLessThanOrEqual(1);
  await expect(page.locator(".voice-message")).toContainText("2″");
  await expect(
    page.getByRole("button", { name: "播放语音", exact: true }),
  ).toHaveCount(1);
  await expect(page.locator(".voice-message audio")).not.toHaveAttribute(
    "controls",
  );
  await expect(page.locator(".bubble-emoji")).toHaveCSS(
    "background-color",
    "rgba(0, 0, 0, 0)",
  );
  await page.getByRole("button", { name: "打开引用内容", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("引用原消息");
  await expect(
    page
      .getByRole("dialog")
      .getByRole("button", { name: "播放语音", exact: true }),
  ).toHaveCount(1);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await page.locator(".wechat-payment.transfer").scrollIntoViewIfNeeded();
  await page.screenshot({ path: "/tmp/weclay-rich-desktop.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page
        .locator(".sidebar")
        .evaluate((el) => el.getBoundingClientRect().right),
    )
    .toBeLessThanOrEqual(0);
  await page.locator(".wechat-payment.transfer").scrollIntoViewIfNeeded();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({ path: "/tmp/weclay-rich-mobile.png" });
  expect(
    Math.abs(
      (await incomingShort.boundingBox())!.width -
        (await outgoingShort.boundingBox())!.width,
    ),
  ).toBeLessThanOrEqual(1);
  // 只拦截本地 Mock 请求，断言正文不再拼接假引用。
  let sent: Record<string, unknown> | undefined;
  let releaseSend!: () => void;
  const sendReceipt = new Promise<void>((resolve) => {
    releaseSend = resolve;
  });
  await page.route("**/api/action", async (route) => {
    const body = route.request().postDataJSON();
    if (body.kind === "send") {
      sent = body;
      await sendReceipt;
      await route.fulfill({ json: { ok: true } });
    } else await route.continue();
  });
  await page.locator(".bubble-quote").click({ button: "right" });
  await page.getByRole("menuitem", { name: "引用", exact: true }).click();
  await page.getByRole("textbox", { name: "消息内容" }).fill("这是新的回复");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect.poll(() => sent?.quote_id).toBe("quote");
  expect(sent?.content).toBe("这是新的回复");
  // 模拟微信回执慢：输入框仍能编辑，新草稿不能被旧回执覆盖。
  await expect(page.getByRole("textbox", { name: "消息内容" })).toBeEnabled();
  await page.getByRole("textbox", { name: "消息内容" }).fill("下一条草稿");
  releaseSend();
  await expect(
    page.getByRole("button", { name: "发送", exact: true }),
  ).toBeEnabled();
  await expect(page.getByRole("textbox", { name: "消息内容" })).toHaveValue(
    "下一条草稿",
  );
});

// 右键菜单没有遮罩，删除仅提交归档操作；键盘和视口避让同样可用。
test("消息右键快捷菜单", async ({ page }) => {
  let deleted = false;
  await page.route("**/api/action", async (route) => {
    if (route.request().postDataJSON()?.kind === "delete-message") {
      deleted = true;
      await route.fulfill({ json: { ok: true } });
    } else await route.continue();
  });
  await page.goto("/#wechat?target=mock-contact-2");
  const bubble = page.locator(".bubble").first();
  await bubble.click({ button: "right" });
  const menu = page.getByRole("menu", { name: "消息快捷菜单" });
  await expect(menu).toBeVisible();
  // 收到的消息不能撤回，右键菜单只显示复制、引用与删除。
  await expect(menu.getByRole("menuitem")).toHaveCount(3);
  await expect(menu.getByRole("menuitem", { name: "撤回" })).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: "消息操作" })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await bubble.click({ button: "right" });
  await page.setViewportSize({ width: 850, height: 600 });
  await expect(menu).toHaveCount(0);
  await bubble.click({ button: "right" });
  await menu.getByRole("menuitem", { name: "删除", exact: true }).click();
  await expect.poll(() => deleted).toBe(true);
  await expect(menu).toHaveCount(0);
});

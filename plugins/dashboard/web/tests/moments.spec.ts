// 文件名：moments.spec.ts
// 主要功能：脱机检查朋友圈封面回退、图片尺寸、互动窗口、图文编辑和人员资料入口。
// 作者：2guan
import { test, expect } from "@playwright/test";
const item = {
  id: "fixture-moment",
  author_id: "mock-contact-2",
  author: "测试好友",
  avatar: "",
  content: "一张照片",
  images: ["/missing-moment.jpg"],
  liked: false,
  likes: 2,
  time: new Date().toISOString(),
  comments: [{ author: "测试好友", content: "不错" }],
};
for (const mobile of [false, true])
  test(`朋友圈和资料 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    await page.route("**/api/state", async (route) => {
      const r = await route.fetch();
      const s = await r.json();
      s.account.moment_cover = "/bad-cover.jpg";
      s.messages = [
        {
          id: "fixture-person",
          target: "mock-contact-2",
          sender_id: "mock-contact-2",
          sender: "测试好友",
          content: "你好",
          kind: "text",
          self: false,
          status: "sent",
          time: new Date().toISOString(),
        },
      ];
      s.contacts.find((c: { id: string }) => c.id === "mock-contact-2").kind =
        "friend";
      await route.fulfill({ json: s });
    });
    await page.route("**/api/moments?*", (route) =>
      route.fulfill({ json: { items: [item], unread: 1 } }),
    );
    await page.route("**/api/moments/notifications", (route) =>
      route.fulfill({ json: { items: [item], unread: 1, notice: "关联动态" } }),
    );
    await page.goto("/#wechat?target=mock-contact-2");
    await page.getByRole("button", { name: "查看测试好友的资料" }).click();
    await expect(
      page.getByRole("dialog", { name: "人员资料", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "发送消息", exact: true }),
    ).toBeVisible();
    await page.locator(".person-moments").click();
    await expect(
      page.locator(".moment-feed").getByText("一张照片"),
    ).toBeVisible();
    const thumb = await page.locator(".moment-thumb").first().boundingBox();
    expect(thumb!.height).toBeLessThanOrEqual(160);
    await expect(page.locator(".moment-load-state")).toContainText(
      "已显示全部可见动态",
    );
    await page.screenshot({
      path: `/tmp/moments-${mobile ? "mobile" : "desktop"}.png`,
    });
  });
test("朋友圈编辑与消息窗口", async ({ page }) => {
  await page.route("**/api/moments?*", (route) =>
    route.fulfill({ json: { items: [item], unread: 1 } }),
  );
  await page.route("**/api/moments/notifications", (route) =>
    route.fulfill({ json: { items: [item], unread: 1, notice: "关联动态" } }),
  );
  await page.goto("/#wechat?target=mock-contact-2");
  await page
    .getByRole("button", { name: "朋友圈", exact: true })
    .first()
    .click();
  await page.getByRole("button", { name: "朋友圈互动提醒" }).click();
  await expect(
    page.getByRole("dialog", { name: "朋友圈消息", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("dialog", { name: "朋友圈消息", exact: true })
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await page.getByRole("button", { name: "发表朋友圈" }).click();
  await page.getByRole("textbox", { name: "朋友圈内容" }).fill("脱机草稿");
  await page.getByRole("button", { name: "选择表情", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "搜索表情" })).toBeVisible();
  await page.getByRole("button", { name: "选择表情", exact: true }).click();
  await page.locator(".moment-editor input[type=file]").setInputFiles({
    name: "fixture.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(page.getByAltText("待发布图片 1")).toBeVisible();
  let submitted = false;
  await page.route("**/api/moments", async (route) => {
    const q = route.request().postDataJSON();
    expect(q.content).toBe("脱机草稿");
    expect(q.images).toHaveLength(1);
    expect(q.blacklist).toEqual([]);
    expect(q.with_users).toEqual([]);
    submitted = true;
    await route.fulfill({ json: { id: "fixture-post" } });
  });
  await page.getByRole("button", { name: "发表", exact: true }).click();
  await expect.poll(() => submitted).toBe(true);
  await expect(
    page.getByRole("dialog", { name: "发表朋友圈", exact: true }),
  ).toHaveCount(0);
});

// 翻页不能重复取首屏，互动不能清空已加载历史；所有写操作均由测试拦截。
test("链接卡片、个人互动与滚动分页", async ({ page }) => {
  const rows = Array.from({ length: 6 }, (_, i) => ({
    ...item,
    id: String(100 - i),
    link: "https://example.com/article",
    title: "测试链接标题",
    images: [],
    thumbnail: "/fixture-thumb.png",
  }));
  let pages = 0,
    interactions = 0;
  await page.route("**/api/moments?*", (route) => {
    pages++;
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    return route.fulfill({
      json: {
        items: cursor
          ? [{ ...rows[0], id: "94", content: "第二页动态" }]
          : rows,
        unread: 0,
        cover: "/fixture-cover.png",
        next_cursor: cursor ? "" : "95",
      },
    });
  });
  await page.route("**/fixture-*.png", (route) =>
    route.fulfill({
      contentType: "image/png",
      body: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
        "base64",
      ),
    }),
  );
  await page.route("**/api/moments/interact", (route) => {
    interactions++;
    const q = route.request().postDataJSON();
    expect(q.target).toBe("mock-contact-2");
    return route.fulfill({
      json: {
        ...rows[0],
        liked: true,
        comments: [{ author: "测试", content: q.content || "" }],
      },
    });
  });
  await page.route("**/api/state", async (route) => {
    const r = await route.fetch();
    const s = await r.json();
    s.messages = [
      {
        id: "fixture",
        target: "mock-contact-2",
        sender_id: "mock-contact-2",
        sender: "测试好友",
        content: "你好",
        kind: "text",
        self: false,
        status: "sent",
        time: new Date().toISOString(),
      },
    ];
    await route.fulfill({ json: s });
  });
  await page.goto("/#wechat?target=mock-contact-2");
  await page.getByRole("button", { name: "查看测试好友的资料" }).click();
  await page.locator(".person-moments").click();
  await expect(page.locator(".moment-cover-photo")).toBeVisible();
  await expect(page.locator(".moment-link img").first()).toBeVisible();
  await expect(page.locator(".moment-thumb")).toHaveCount(0);
  await page.getByRole("button", { name: "赞", exact: true }).first().click();
  await expect(
    page.getByRole("button", { name: "取消赞", exact: true }),
  ).toBeVisible();
  await page.locator(".comment-input input").first().fill("测试评论");
  await page.locator(".comment-input button").first().click();
  await expect.poll(() => interactions).toBe(2);
  await page.locator(".moment-load-state").scrollIntoViewIfNeeded();
  await expect(page.getByText("第二页动态", { exact: true })).toBeVisible();
  await expect(page.locator(".moment-load-state")).toHaveText(
    "已显示全部可见动态",
  );
  expect(pages).toBe(2);
});

// 让头图请求保持挂起，断言列表先显示，不依赖计时阈值或真实微信。
test("慢头图不阻塞列表，显示点赞姓名与图标", async ({ page }) => {
  let release: () => void = () => {};
  const coverGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/moments/cover?*", async (route) => {
    await coverGate;
    await route.fulfill({ json: { cover: "" } });
  });
  await page.route("**/api/moments?*", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            ...item,
            like_users: [
              { id: "fixture-a", name: "小明" },
              { id: "fixture-b", name: "小红" },
            ],
          },
        ],
        unread: 0,
        next_cursor: "",
      },
    }),
  );
  await page.goto("/#wechat?target=mock-contact-2");
  await page
    .getByRole("button", { name: "朋友圈", exact: true })
    .first()
    .click();
  await expect(page.getByText("一张照片", { exact: true })).toBeVisible();
  await expect(page.locator(".moment-likes")).toHaveText("小明、小红");
  await expect(
    page.getByRole("button", { name: "赞", exact: true }).locator("svg"),
  ).toBeVisible();
  release();
});

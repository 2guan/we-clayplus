import { test, expect } from "@playwright/test";
const moment = {
  id: "fixture-draft",
  author_id: "mock-contact-2",
  author: "测试好友",
  content: "海边的落日",
  images: [],
  liked: false,
  likes: 0,
  time: new Date().toISOString(),
  comments: [],
};
async function openMoments(page: import("@playwright/test").Page) {
  await page.route("**/api/moments?*", (route) =>
    route.fulfill({ json: { items: [moment], unread: 0, next_cursor: "" } }),
  );
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    const c = state.contacts.find(
      (c: { id: string }) => c.id === "mock-contact-2",
    );
    c.kind = "friend";
    c.name = "测试好友";
    state.messages = [
      {
        id: "fixture-person",
        target: c.id,
        sender_id: c.id,
        sender: "测试好友",
        content: "你好",
        kind: "text",
        self: false,
        status: "sent",
        time: new Date().toISOString(),
      },
    ];
    await route.fulfill({ json: state });
  });
  await page.goto("/#wechat?target=mock-contact-2");
  await page.getByRole("button", { name: "查看测试好友的资料" }).click();
  await page.locator(".person-moments").click();
  await expect(page.getByText("海边的落日", { exact: true })).toBeVisible();
}
for (const mobile of [false, true])
  test(`AI留言只填草稿 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
    if (mobile) await page.setViewportSize({ width: 390, height: 844 });
    let sends = 0;
    await page.route("**/api/moments/interact", async (route) => {
      sends++;
      expect(route.request().postDataJSON().content).toBe(
        "这片落日也太温柔了！",
      );
      await route.fulfill({
        json: {
          ...moment,
          comments: [{ author: "本人", content: "这片落日也太温柔了！" }],
        },
      });
    });
    await page.route("**/api/moments/draft", async (route) => {
      expect(route.request().postDataJSON()).toEqual({ id: moment.id });
      await route.fulfill({
        json: { text: "这片落日也太温柔了！", warnings: [] },
      });
    });
    await openMoments(page);
    const button = page.getByRole("button", {
      name: "AI生成测试好友的留言草稿",
    });
    await button.click();
    const input = page.getByRole("textbox", { name: "评论 测试好友 的动态" });
    await expect(input).toHaveValue("这片落日也太温柔了！");
    await expect(input).toBeFocused();
    expect(sends).toBe(0);
    const rect = await button.boundingBox();
    expect(rect!.x + rect!.width).toBeLessThanOrEqual(mobile ? 390 : 1280);
    await page.screenshot({
      path: `/tmp/weclay-moments-draft-${mobile ? "mobile" : "desktop"}.png`,
    });
    await page
      .locator(".comment-input")
      .getByRole("button", { name: "发送", exact: true })
      .click();
    await expect.poll(() => sends).toBe(1);
  });
test("AI留言失败保留输入，迟到草稿不覆盖手工修改", async ({ page }) => {
  let calls = 0;
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/moments/draft", async (route) => {
    calls++;
    if (calls === 1) {
      await route.fulfill({
        status: 502,
        json: { error: "识图模型暂不可用，请重试" },
      });
      return;
    }
    await gate;
    await route.fulfill({ json: { text: "迟到的AI草稿", warnings: [] } });
  });
  await openMoments(page);
  const input = page.getByRole("textbox", { name: "评论 测试好友 的动态" });
  const button = page.getByRole("button", { name: "AI生成测试好友的留言草稿" });
  await input.fill("我自己写的内容");
  await button.click();
  await expect(page.locator(".moment-draft-note")).toContainText(
    "识图模型暂不可用",
  );
  await expect(input).toHaveValue("我自己写的内容");
  await expect(button).toBeEnabled();
  await button.click();
  await expect(button).toBeDisabled();
  await input.fill("我在生成过程中又编辑了");
  release();
  await expect(page.locator(".moment-draft-note")).toContainText(
    "已保留你编辑的留言",
  );
  await expect(input).toHaveValue("我在生成过程中又编辑了");
  await expect(button).toBeEnabled();
});

import { expect, test, type Page } from "@playwright/test";

const moment = {
  id: "fixture-reply-moment", author_id: "fixture-owner", author: "动态作者", avatar: "",
  content: "原动态正文", images: [], liked: false, likes: 0, time: new Date().toISOString(),
  comments: [
    { id: "7", author_id: "fixture-peer", author: "好友一", content: "第一问" },
    { id: "8", author_id: "fixture-peer", author: "好友一", content: "第二问" },
    { id: "9", author_id: "fixture-other", author: "好友二", content: "补充讨论", reply_to: "好友一" },
  ],
};
async function openMoments(page: Page, mobile = false) {
  if (mobile) await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/api/moments?*", (route) => route.fulfill({ json: { items: [moment], unread: 0, next_cursor: "" } }));
  await page.route("**/api/moments/notifications", (route) => route.fulfill({ json: { items: [], unread: 1, notifications: [{ id: "fixture-notice", moment_id: moment.id, actor: "好友二", kind: "comment", content: "补充讨论", created: 1793199039, read: false }] } }));
  await page.route("**/api/moments/notifications?id=*", (route) => route.fulfill({ json: moment }));
  await page.route("**/api/moments/notifications/read", (route) => route.fulfill({ json: { unread: 0 } }));
  await page.goto("/#wechat");
  await page.getByRole("button", { name: mobile ? "打开朋友圈" : "朋友圈", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "朋友圈", exact: true });
  await expect(dialog.getByText("原动态正文")).toBeVisible();
  return dialog;
}

for (const mobile of [false, true]) test(`选中具体评论并使用AI回复 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
  let writes = 0;
  await page.route("**/api/moments/draft", (route) => {
    expect(route.request().postDataJSON()).toEqual({ id: moment.id, reply_comment_id: 7, reply_author_id: "fixture-peer" });
    return route.fulfill({ json: { text: "回答第一问。" } });
  });
  await page.route("**/api/moments/interact", (route) => {
    writes++;
    expect(route.request().postDataJSON()).toMatchObject({ id: moment.id, kind: "moment-comment", content: "回答第一问。", reply_comment_id: 7, reply_author_id: "fixture-peer" });
    return route.fulfill({ json: { ...moment, comments: [...moment.comments, { id: "10", author: "本人昵称", author_id: "fixture-self", content: "回答第一问。", reply_to: "好友一", reply_to_id: "fixture-peer" }] } });
  });
  const dialog = await openMoments(page, mobile);
  await dialog.getByRole("button", { name: "回复好友一的评论：第一问", exact: true }).click();
  const input = dialog.getByRole("textbox", { name: "回复 好友一 的评论" });
  await expect(input).toBeFocused();
  await expect(dialog.locator(".moment-reply-target")).toContainText("第一问");
  await dialog.getByRole("button", { name: "AI生成动态作者的留言草稿" }).click();
  await expect(input).toHaveValue("回答第一问。");
  expect(writes).toBe(0);
  await page.screenshot({ path: `/tmp/weclay-moment-reply-${mobile ? "mobile" : "desktop"}.png` });
  await dialog.locator(".comment-input").getByRole("button", { name: "发送", exact: true }).click();
  await expect(dialog.getByText("本人昵称回复好友一：", { exact: true })).toBeVisible();
  expect(writes).toBe(1);
  await expect(dialog.locator(".moment-reply-target")).toHaveCount(0);
});

test("取消回复恢复普通留言，同一人多条评论使用不同编号", async ({ page }) => {
  let writes = 0;
  await page.route("**/api/moments/interact", (route) => {
    writes++;
    const body = route.request().postDataJSON();
    expect(body.reply_comment_id).toBeUndefined();
    expect(body.reply_author_id).toBeUndefined();
    return route.fulfill({ json: { ...moment, comments: [...moment.comments, { author: "本人昵称", content: body.content }] } });
  });
  const dialog = await openMoments(page);
  await dialog.getByRole("button", { name: "回复好友一的评论：第二问", exact: true }).click();
  await expect(dialog.locator(".moment-reply-target")).toContainText("第二问");
  await dialog.getByRole("button", { name: "取消回复好友一" }).click();
  const input = dialog.getByRole("textbox", { name: "评论 动态作者 的动态" });
  await input.fill("普通留言");
  await dialog.locator(".comment-input").getByRole("button", { name: "发送", exact: true }).click();
  await expect(dialog.getByText("本人昵称：", { exact: true })).toBeVisible();
  expect(writes).toBe(1);
});

test("互动提醒中回复评论，失败保留对象和输入，不自动重发", async ({ page }) => {
  let writes = 0;
  await page.route("**/api/moments/interact", (route) => {
    writes++;
    expect(route.request().postDataJSON()).toMatchObject({ reply_comment_id: 9, reply_author_id: "fixture-other", content: "回应这条补充" });
    return writes === 1
      ? route.fulfill({ status: 502, json: { error: "回复结果未确认，请刷新核对" } })
      : route.fulfill({ json: { ...moment, comments: [...moment.comments, { author: "本人昵称", content: "回应这条补充", reply_to: "好友二" }] } });
  });
  const dialog = await openMoments(page);
  await dialog.getByRole("button", { name: "朋友圈互动提醒" }).click();
  await page.getByRole("dialog", { name: "朋友圈消息", exact: true }).getByRole("button", { name: /好友二评论了你的朋友圈/ }).click();
  const notices = page.getByRole("dialog", { name: "互动消息详情", exact: true });
  await notices.getByRole("button", { name: "回复好友二的评论：补充讨论", exact: true }).click();
  const input = notices.getByRole("textbox", { name: "回复 好友二 的评论" });
  await input.fill("回应这条补充");
  await notices.locator(".comment-input").getByRole("button", { name: "发送", exact: true }).click();
  await expect(page.getByText("回复结果未确认，请刷新核对", { exact: true })).toBeVisible();
  await expect(input).toHaveValue("回应这条补充");
  await expect(notices.locator(".moment-reply-target")).toContainText("好友二");
  expect(writes).toBe(1);
  await notices.locator(".comment-input").getByRole("button", { name: "发送", exact: true }).click();
  await expect(notices.getByText("本人昵称回复好友二：", { exact: true })).toBeVisible();
  expect(writes).toBe(2);
});

test("取消回复后迟到的AI草稿不覆盖普通留言", async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/moments/draft", async (route) => {
    await gate;
    await route.fulfill({ json: { text: "过时的定向回复" } });
  });
  const dialog = await openMoments(page);
  await dialog.getByRole("button", { name: "回复好友一的评论：第一问", exact: true }).click();
  await dialog.getByRole("button", { name: "AI生成动态作者的留言草稿" }).click();
  await dialog.getByRole("button", { name: "取消回复好友一" }).click();
  const input = dialog.getByRole("textbox", { name: "评论 动态作者 的动态" });
  await input.fill("保留我的普通留言");
  release();
  await expect(dialog.locator(".moment-draft-note")).toContainText("未覆盖");
  await expect(input).toHaveValue("保留我的普通留言");
});

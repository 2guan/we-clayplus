// 文件名：chat-mentions.spec.ts
// 主要功能：验证群成员拼音检索、键盘选择、头像右键提醒及删除绑定。
// 作者：2guan
import { test, expect } from "@playwright/test";
test("群聊 @ 选择绑定真实成员 ID", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const r = await route.fetch();
    const s = await r.json();
    s.contacts.push({
      id: "mention@chatroom",
      name: "提醒测试群",
      kind: "group",
      tags: [],
      mode: "manual",
      unread: 0,
    });
    s.messages = [
      {
        id: "mention-source",
        target: "mention@chatroom",
        sender_id: "member-zhang",
        sender: "张三",
        content: "消息",
        kind: "text",
        self: false,
        status: "sent",
        time: new Date().toISOString(),
      },
    ];
    await route.fulfill({ json: s });
  });
  let rooms = 0;
  await page.route("**/api/room?**", (route) => {
    rooms++;
    return route.fulfill({
      json: {
        members: [
          {
            id: "member-zhang",
            name: "张三",
            display_name: "张三",
            avatar: "",
          },
          { id: "member-li", name: "李四", display_name: "李四", avatar: "" },
        ],
      },
    });
  });
  const sends: any[] = [];
  await page.route("**/api/action", async (route) => {
    const q = route.request().postDataJSON();
    if (q.kind === "send") {
      sends.push(q);
      await route.fulfill({ json: { ok: true } });
    } else await route.continue();
  });
  await page.goto("/#wechat?target=mention%40chatroom");
  const input = page.getByRole("textbox", { name: "消息内容" });
  await input.fill("@zhang");
  await expect(page.getByRole("option")).toHaveCount(1);
  await input.press("Enter");
  await expect(input).toHaveValue("@张三\u2005");
  expect(sends).toHaveLength(0);
  await input.press("End");
  await input.press("a");
  await input.press("Enter");
  await expect.poll(() => sends.length).toBe(1);
  expect(sends[0].mentions).toEqual(["member-zhang"]);
  await page
    .getByRole("button", { name: "查看张三的资料" })
    .click({ button: "right" });
  await page.getByRole("menuitem", { name: "@ 张三", exact: true }).click();
  await expect(input).toHaveValue("@张三\u2005");
  // 删除 @ 字段后，普通文本不应保留隐藏的提醒对象。
  await input.fill("普通文字");
  await input.press("Enter");
  await expect.poll(() => sends.length).toBe(2);
  expect(sends[1].content).toBe("普通文字");
  expect(sends[1].mentions).toEqual([]);
  await input.fill("@");
  await input.press("ArrowDown");
  await input.press("Enter");
  await expect(input).toHaveValue("@李四\u2005");
  await input.fill("@");
  await expect(page.getByRole("option")).toHaveCount(2);
  await page.screenshot({ path: "/tmp/chat-mentions-desktop.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  const bounds = await page.locator(".chat-mention-list").boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390);
  await expect
    .poll(async () => {
      const r = await page.locator(".sidebar").boundingBox();
      return r!.x + r!.width;
    })
    .toBeLessThanOrEqual(0);
  await page.screenshot({ path: "/tmp/chat-mentions-mobile.png" });
  expect(rooms).toBe(1);
});

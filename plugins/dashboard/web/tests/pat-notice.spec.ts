import { expect, test } from "@playwright/test";

for (const mobile of [false, true]) test(`拍一拍仅显示文案 ${mobile ? "手机" : "桌面"}`, async ({ page }) => {
  if (mobile) await page.setViewportSize({ width: 390, height: 844 });
  const target = "fixture-pat-room@chatroom";
  const text = '"成员甲" 拍了拍 "成员乙" 的\n^ ^\n👀 "⚫️" 👅';
  await page.route("**/api/state", async (route) => {
    const r = await route.fetch();
    const state = await r.json();
    state.contacts = [{ id: target, kind: "group", name: "测试群聊", remark: "", unread: 0, mode: "auto", preview: "拍一拍", tags: [], members: 2 }];
    const base = { target, sender: "测试群聊", sender_id: target, self: false, kind: "system", source: "wechat", status: "sent", time: new Date().toISOString() };
    state.messages = [
      { ...base, id: "fixture-pat-forward", content: text, interaction: { kind: "pat", actor_id: "fixture-member-a", actor_name: "成员甲", target_id: "fixture-member-b", target_name: "成员乙", verified: true } },
      { ...base, id: "fixture-pat-reverse", content: '"成员乙" 拍了拍 "成员甲"', interaction: { kind: "pat", actor_id: "fixture-member-b", actor_name: "成员乙", target_id: "fixture-member-a", target_name: "成员甲", verified: true } },
    ];
    await route.fulfill({ json: state });
  });
  await page.goto(`/#wechat?target=${target}`);
  const messages = page.locator(".system-message");
  await expect(messages).toHaveCount(2);
  await expect(messages.first()).toContainText('"成员甲" 拍了拍 "成员乙"');
  await expect(messages.last()).toContainText('"成员乙" 拍了拍 "成员甲"');
  await expect(messages.first()).toHaveCSS("white-space", "pre-wrap");
  await expect(messages.first()).toHaveText(text);
  await expect(messages.locator(".interaction-detail")).toHaveCount(0);
  await expect(messages.first()).not.toContainText("fixture-member-a");
  await page.screenshot({ path: `/tmp/weclay-pat-notice-${mobile ? "mobile" : "desktop"}.png` });

});

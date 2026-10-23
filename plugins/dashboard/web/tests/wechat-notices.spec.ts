import { test, expect } from "@playwright/test";

test("邀请通知显示具体成员，语音通话以图标和状态气泡展示", async ({
  page,
  request,
}) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.messages.push(
      {
        id: "fixture-readable-invite",
        target: "mock-contact-0",
        sender: "系统",
        content: "“邀请成员”邀请“成员甲、成员乙”加入了群聊",
        kind: "system",
        self: false,
        status: "sent",
        source: "wechat",
        time: "2026-10-22T12:00:00+08:00",
      },
      {
        id: "fixture-readable-call",
        target: "mock-contact-0",
        sender: "测试成员",
        content: "[语音通话] 对方已取消",
        kind: "call",
        rich: { title: "语音通话", status: "对方已取消" },
        self: false,
        status: "sent",
        source: "wechat",
        time: "2026-10-22T12:01:00+08:00",
      },
    );
    await route.fulfill({ response, json: state });
  });
  await page.goto("/#wechat?target=mock-contact-0");
  await expect(
    page.getByText("“邀请成员”邀请“成员甲、成员乙”加入了群聊", { exact: true }),
  ).toBeVisible();
  const call = page.getByRole("group", { name: "语音通话", exact: true });
  await expect(call).toHaveText("对方已取消");
  await expect(call.locator("svg")).toBeVisible();
  await call.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "/tmp/weclay-call-desktop.png",
    animations: "disabled",
  });
  await call.click({ button: "right" });
  await expect(
    page.getByRole("menuitem", { name: "引用", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 390, height: 844 });
  await call.scrollIntoViewIfNeeded();
  await expect(call).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/weclay-call-mobile.png",
    animations: "disabled",
  });
});

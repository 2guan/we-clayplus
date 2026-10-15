// 文件名：payments.spec.ts
// 主要功能：脱机验证领取前人工确认、两种支付卡片、未知结果及重复提交保护。
// 作者：2guan
import { test, expect } from "@playwright/test";
for (const kind of ["redpacket", "transfer"]) {
  test(`${kind} 人工领取`, async ({ page }) => {
    await page.route("**/api/state", async (route) => {
      const response = await route.fetch();
      const state = await response.json();
      state.messages = [
        {
          id: "synthetic-payment",
          target: "mock-contact-2",
          sender: "测试发送人",
          kind,
          content: "支付通知",
          self: false,
          status: "sent",
          time: new Date().toISOString(),
          rich: { amount: "￥0.01", status: "转账通知" },
        },
      ];
      await route.fulfill({ json: state });
    });
    let writes = 0;
    let status = "ready";
    await page.route("**/api/payments**", async (route) => {
      if (route.request().method() === "POST") {
        expect(route.request().postDataJSON()).toMatchObject({
          confirmed: true,
          target: "mock-contact-2",
          id: "synthetic-payment",
        });
        writes++;
        status = kind === "transfer" ? "unknown" : "confirmed";
        await route.fulfill({
          status: kind === "transfer" ? 502 : 200,
          json:
            kind === "transfer"
              ? { error: "结果未确认，请在微信核对" }
              : { status },
        });
      } else
        await route.fulfill({
          json: {
            status,
            kind,
            sender: "测试发送人",
            amount: "￥0.01",
            mock: true,
          },
        });
    });
    await page.goto("/#wechat?target=mock-contact-2");
    const card = page.getByRole("button", {
      name: kind === "transfer" ? "查看并确认收款" : "查看并领取红包",
      exact: true,
    });
    await card.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("测试发送人")).toBeVisible();
    expect(writes).toBe(0);
    const confirm = dialog.getByRole("button", {
      name: kind === "transfer" ? "确认收款" : "确认领取红包",
      exact: true,
    });
    await confirm.click();
    await expect(confirm).toBeDisabled();
    expect(writes).toBe(1);
    await dialog
      .getByRole("button", { name: "关闭", exact: true })
      .last()
      .click();
    await card.click();
    await expect(confirm).toBeDisabled();
    expect(writes).toBe(1);
  });
}

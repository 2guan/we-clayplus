// 文件名：wechat-login.spec.ts
// 主要功能：验证登录暂停后显式重启与重新读取二维码的交互，不连接真实微信。
// 作者：2guan
import { test, expect } from "@playwright/test";

test("登录暂停后重启并展示新二维码", async ({ page }) => {
  let restarted = false;
  await page.route("**/api/wechat-login", async (route) => {
    if (route.request().method() === "POST") {
      restarted = true;
      await route.fulfill({ json: { restarting: true } });
      return;
    }
    await route.fulfill({ json: {
      online: false,
      status: restarted ? "等待手机扫码并确认" : "登录流程已暂停：登录超时或取消登录（等待手机扫码并确认）",
      qr: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
    }});
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", {name:"重启 core，重新获取二维码"})).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toHaveCount(0);
  await dialog.getByRole("button", {name:"重启 core，重新获取二维码"}).click();
  await expect(dialog.getByText("等待手机扫码并确认", {exact:true})).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toBeVisible();
  expect(restarted).toBe(true);
});

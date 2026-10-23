// 文件名：wechat-login.spec.ts
// 主要功能：验证同页启停 core、扫码与登录重试，不连接真实微信。
// 作者：2guan
import { test, expect } from "@playwright/test";

const qr = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=";

test("登录暂停后在控制台重试并展示新二维码", async ({ page }) => {
  let retried = false;
  await page.route("**/api/core", async (route) => {
    await route.fulfill({ json: { state: "waiting_login", managed: true, status: "等待登录" } });
  });
  await page.route("**/api/wechat-login", async (route) => {
    if (route.request().method() === "POST") {
      retried = true;
      await route.fulfill({ json: { retrying: true } });
      return;
    }
    await route.fulfill({ json: {
      online: false,
      can_retry: !retried,
      status: retried ? "等待手机扫码并确认" : "登录流程已暂停：登录超时或取消登录",
      qr,
    }});
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", {name:"重新获取二维码"})).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toHaveCount(0);
  await dialog.getByRole("button", {name:"重新获取二维码"}).click();
  await expect(dialog.getByText("等待手机扫码并确认", {exact:true})).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toBeVisible();
  expect(retried).toBe(true);
});

test("在控制台启动和停止 core", async ({ page }) => {
  let state = "stopped";
  await page.route("**/api/core", async (route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON() as { action: string };
      state = body.action === "start" ? "waiting_login" : "stopped";
    }
    await route.fulfill({ json: { state, managed: true, status: state === "stopped" ? "已停止" : "等待登录" } });
  });
  await page.route("**/api/wechat-login", async (route) => {
    await route.fulfill({ json: { online: false, status: "等待扫码", qr } });
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("core 已停止")).toBeVisible();
  await dialog.getByRole("button", { name: "启动 core" }).click();
  await expect(dialog.getByAltText("微信登录二维码")).toBeVisible();
  await dialog.getByRole("button", { name: "停止 core" }).click();
  await expect(dialog.getByText("设备和登录数据会保留。", { exact: false })).toBeVisible();
  await dialog.getByRole("button", { name: "确认停止" }).click();
  await expect(dialog.getByText("core 已停止")).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toHaveCount(0);
});

test("外部 core 只显示在线状态，不提供登录和启停操作", async ({ page }) => {
  await page.route("**/api/core", async (route) => {
    await route.fulfill({ json: { state: "unmanaged", managed: false, status: "外部 core 已连接" } });
  });
  await page.route("**/api/wechat-login", async (route) => {
    await route.fulfill({ json: { online: true, status: "已连接", qr } });
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("此处仅显示在线状态", { exact: false })).toBeVisible();
  await expect(dialog.getByAltText("微信登录二维码")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "重新获取二维码" })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "启动 core" })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "停止 core" })).toHaveCount(0);
});

test("core 连接错误时仍可请求受控停止", async ({ page }) => {
  await page.route("**/api/core", async (route) => {
    await route.fulfill({ json: { state: "error", managed: true, status: "core 连接失败" } });
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: "停止 core" })).toBeVisible();
  await dialog.getByRole("button", { name: "停止 core" }).click();
  await expect(dialog.getByRole("button", { name: "确认停止" })).toBeVisible();
});

test("登录设备验证可在控制台提交验证码", async ({ page }) => {
  let submitted = "";
  await page.route("**/api/core", async (route) => {
    await route.fulfill({ json: { state: "waiting_login", managed: true, status: "等待验证" } });
  });
  await page.route("**/api/wechat-login", async (route) => {
    await route.fulfill({ json: submitted
      ? { online: false, phase: "waiting_scan", status: "等待登录结果", qr }
      : { online: false, phase: "verification_required", pin_required: true, status: "请输入验证码" } });
  });
  await page.route("**/api/wechat-login/verify-code", async (route) => {
    submitted = (route.request().postDataJSON() as { code: string }).code;
    await route.fulfill({ json: { submitted: true } });
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("微信提示的 PIN 验证码").fill("123456");
  await dialog.getByRole("button", { name: "提交验证码" }).click();
  await expect(dialog.getByText("等待登录结果", { exact: true })).toBeVisible();
  expect(submitted).toBe("123456");
});

test("设备确认由控制台固定接口完成", async ({ page }) => {
  let confirmed = false;
  await page.route("**/api/core", async (route) => {
    await route.fulfill({ json: { state: "waiting_login", managed: true, status: "等待验证" } });
  });
  await page.route("**/api/wechat-login", async (route) => {
    await route.fulfill({ json: confirmed
      ? { online: false, phase: "waiting_scan", status: "等待登录结果", qr }
      : { online: false, phase: "verification_required", device_confirm_available: true, status: "请确认设备" } });
  });
  await page.route("**/api/wechat-login/device-confirm", async (route) => {
    expect(route.request().postDataJSON()).toEqual({});
    confirmed = true;
    await route.fulfill({ json: { submitted: true } });
  });
  await page.goto("/#overview");
  await page.locator(".account-identity").click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "确认本次设备登录" }).click();
  await expect(dialog.getByText("等待登录结果", { exact: true })).toBeVisible();
  expect(confirmed).toBe(true);
});

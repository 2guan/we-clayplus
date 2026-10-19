// 文件名：dashboard.spec.ts
// 主要功能：以浏览器验证脱机页面、六类 AI、消息操作、持久化设置和手机主题。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("八个页面无异常并保持同源访问", async ({ page, request }) => {
  const state = await (await request.get("/api/state")).json();
  const errors: string[] = [];
  const outbound: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (
      !request.url().startsWith("http://127.0.0.1:18899") &&
      !request.url().startsWith("data:")
    )
      outbound.push(request.url());
  });
  await page.goto("/");
  for (const [route, title] of [
    ["overview", "总览"],
    ["wechat", "微信"],
    ["contacts", "通讯录"],
    ["models", "模型中心"],
    ["plugins", "插件中心"],
    ["insights", "智能洞察"],
    ["logs", "实时日志"],
    ["settings", "系统设置"],
  ]) {
    await page.goto("/#" + route);
    await expect(page).toHaveTitle(`${state.account.name} · ${title}`);
    await expect(page.locator(".topbar .connection-status")).toContainText(
      "服务连接正常",
    );
    await expect(page.locator(".main-content")).not.toBeEmpty();
  }
  expect(errors).toEqual([]);
  expect(outbound).toEqual([]);
});

test("六类能力都有可查看结果，聊天可发送和撤回", async ({ page }) => {
  await page.goto("/#models");
  for (const [tab, ability, button] of [
    ["对话模型", "chat", "开始测试"],
    ["对话模型", "vision", "开始测试"],
    ["对话模型", "audio", "开始测试"],
    ["语音模型", "tts", "生成试听"],
    ["图片模型", "image", "生成预览"],
    ["视频模型", "video_generation", "生成预览"],
  ]) {
    await page.getByRole("button", { name: tab, exact: true }).click();
    if (tab === "对话模型")
      await page.getByLabel("测试能力").selectOption(ability);
    await page.getByRole("button", { name: button, exact: true }).click();
    await expect(page.locator(".result-panel")).toContainText("已完成");
    await expect(page.locator(".task-result").first()).toBeVisible();
  }
  await page.goto("/#wechat");
  await page.getByRole("button", { name: /林间/ }).click();
  await page
    .getByRole("textbox", { name: "消息内容", exact: true })
    .fill("浏览器脱机消息");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  const message = page
    .locator(".message")
    .filter({ hasText: "浏览器脱机消息" });
  await expect(message).toBeVisible();
  await message.getByRole("button", { name: "消息操作" }).click();
  await page.getByRole("menuitem", { name: "撤回", exact: true }).click();
  await expect(page.locator(".system-message").last()).toHaveText(
    "你撤回了一条消息",
  );
});

test("设置保存后刷新仍然存在，错误场景可恢复", async ({ page }) => {
  await page.goto("/#settings");
  await page.getByLabel("控制台名称", { exact: true }).fill("脱机测试空间");
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("设置已保存");
  // 不刷新就进入两种预览，确保使用刚保存的名称而非启动时的认证快照。
  await page.getByRole("button", { name: "脱机 Mock" }).click();
  for (const [entry, title] of [
    ["登录页", "登录"],
    ["初始化页", "初始化"],
  ]) {
    await page.getByRole("button", { name: entry, exact: true }).click();
    await expect(page.locator(".wordmark")).toContainText("脱机测试空间");
    await expect(page).toHaveTitle(`${title} · 脱机测试空间`);
    await page.getByRole("button", { name: "返回控制台" }).click();
  }
  await page.reload();
  await expect(page.locator(".brand strong")).toHaveText("脱机测试空间");
  await page.getByRole("button", { name: "脱机 Mock" }).click();
  await page.getByLabel("Mock 场景").selectOption("error");
  await page.goto("/#models");
  await page.getByRole("button", { name: "开始测试", exact: true }).click();
  await expect(page.locator(".result-panel")).toContainText("这次任务没有完成");
});

test("手机微信独立列表与聊天、暗色和登录页预览", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#wechat");
  await expect(page.locator(".conversation-list")).toBeVisible();
  await expect(page.locator(".chat-window")).not.toBeVisible();
  await page.locator(".conversation").first().click();
  await expect(page.locator(".chat-window")).toBeVisible();
  await expect(page.locator(".conversation-list")).not.toBeVisible();
  await page.getByRole("button", { name: "返回会话列表" }).click();
  await page.getByRole("button", { name: /切换主题/ }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/mobile-dark.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "脱机 Mock" }).click();
  await page.getByRole("button", { name: "登录页", exact: true }).click();
  await expect(page.getByRole("heading", { name: "欢迎回来" })).toBeVisible();
  await page.getByRole("button", { name: "返回控制台" }).click();
  await expect(page.locator(".conversation-list")).toBeVisible();
});

// 密码入口属于全局用户信息，关闭后清空输入，手机侧栏也应可达。
test("用户信息打开修改密码，脱机提交后清空表单", async ({ page }) => {
  await page.goto("/#settings");
  await expect(page.locator(".tabs")).not.toContainText("个人安全");
  const entry = page.getByRole("button", { name: "修改密码", exact: true });
  await entry.click();
  const dialog = page.getByRole("dialog", { name: "修改密码", exact: true });
  await expect(dialog).toBeVisible();
  await dialog
    .getByLabel("当前密码", { exact: true })
    .fill("mock-old-password");
  await dialog.getByLabel("新密码", { exact: true }).fill("mock-new-password");
  await dialog.getByRole("button", { name: "更新密码", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole("status")).toContainText("密码修改流程已验证");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await entry.click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("当前密码", { exact: true })).toHaveValue("");
  await expect(dialog.getByLabel("新密码", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
});

// 使用稀疏日期验证实际日历筛选，防止把“最近七条记录”误当成“近七天”。
test("总览支持自定义日期、边界校验与稀疏统计", async ({ page, request }) => {
  const snapshot = await (await request.get("/api/state")).json();
  const trend = [snapshot.trend[0], snapshot.trend[4], snapshot.trend.at(-1)];
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({ response, json: { ...body, trend } });
  });
  await page.goto("/#overview");
  await expect(page.getByRole("link", { name: /已归档消息/ })).toHaveCount(0);
  await expect(page.locator(".account-strip .asset")).toHaveCount(4);
  await expect(page.locator(".bar-column")).toHaveCount(2);
  await page.getByRole("button", { name: "自定义", exact: true }).click();
  await page.getByLabel("开始日期", { exact: true }).fill(trend[0].date);
  await page.getByLabel("结束日期", { exact: true }).fill(trend[1].date);
  await page.getByRole("button", { name: "应用范围", exact: true }).click();
  await expect(page.locator(".bar-column")).toHaveCount(4);
  const total = trend
    .slice(0, 2)
    .reduce((sum, point) => sum + point.input + point.output, 0);
  await expect(page.locator(".chart-total")).toContainText(
    new Intl.NumberFormat("zh-CN").format(total),
  );
  await page.reload();
  await expect(page.getByLabel("开始日期", { exact: true })).toHaveValue(
    trend[0].date,
  );
  await expect(page.locator(".bar-column")).toHaveCount(4);
  await page.getByLabel("开始日期", { exact: true }).fill(trend[2].date);
  await page.getByRole("button", { name: "应用范围", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "开始日期不能晚于结束日期",
  );
  await expect(page.locator(".bar-column")).toHaveCount(4);
  await page.getByLabel("结束日期", { exact: true }).fill(trend[2].date);
  await page.getByRole("button", { name: "应用范围", exact: true }).click();
  await expect(page.locator(".bar-column")).toHaveCount(2);
  await page.getByLabel("开始日期", { exact: true }).fill("2000-01-01");
  await page.getByLabel("结束日期", { exact: true }).fill("2000-01-02");
  await page.getByRole("button", { name: "应用范围", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "暂无用量", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "暂无消息", exact: true }),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "近 30 天", exact: true }).click();
  await expect(page.locator(".bar-column")).toHaveCount(6);
  await page.goBack();
  await expect(page.getByLabel("开始日期", { exact: true })).toHaveValue(
    "2000-01-01",
  );
});

// 今日卡片使用任务创建日期；历史失败不能算作待处理，费用也不属于首页指标。
test("总览模型任务只统计今日并展示真实状态", async ({ page }) => {
  const now = new Date();
  const yesterday = new Date(now.getTime() - 48 * 60 * 60 * 1000);
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({
      response,
      json: {
        ...body,
        tasks: [
          { id: "today-done", created: now.toISOString(), status: "completed" },
          {
            id: "today-running",
            created: now.toISOString(),
            status: "running",
          },
          {
            id: "old-failed",
            created: yesterday.toISOString(),
            status: "failed",
          },
        ],
      },
    });
  });
  await page.goto("/#overview");
  const metric = page.locator(".metric").filter({ hasText: "模型任务" });
  await expect(metric.locator("strong")).toHaveText("2次");
  await expect(metric).toContainText("已完成 1 · 进行中 1 · 失败/中断 0");
  await expect(metric).not.toContainText("待处理");
  await expect(metric).not.toContainText("费用");
});

// 发出消息的总数与来源分项都来自成功归档，不能把失败请求或收到的消息算进去。
test("总览按发送来源统计今日成功消息", async ({ page }) => {
  const now = new Date();
  const old = new Date(now.getTime() - 48 * 60 * 60 * 1000);
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    const base = body.messages[0];
    const message = (
      id: string,
      source: string,
      status = "sent",
      time = now.toLocaleString("sv-SE",{timeZone:"Asia/Shanghai"}).replace(" ","T")+"+08:00",
      self = true,
    ) => ({
      ...base,
      id,
      source,
      status,
      time,
      self,
    });
    await route.fulfill({
      response,
      json: {
        ...body,
        messages: [
          message("manual", "manual"),
          message("ai", "ai"),
          message("plugin", "plugin:example"),
          message("wechat", "wechat"),
          message("failed", "manual", "unknown"),
          message("incoming", "wechat", "sent", now.toISOString(), false),
          message("old", "manual", "sent", old.toISOString()),
        ],
      },
    });
  });
  await page.goto("/#overview");
  const metric = page.locator(".metric").filter({ hasText: "今日发出消息" });
  await expect(metric.locator("strong")).toHaveText("4条");
  await expect(metric).toContainText("手动 1 · AI 1 · 插件 1 · 微信端 1");
});

// 补足三十天数据并测量每根柱子的边界，不能用裁切或横向滚动掩盖溢出。
test("十四天和三十天图表完整容纳在桌面及手机卡片内", async ({
  page,
  request,
}) => {
  const snapshot = await (await request.get("/api/state")).json();
  const latest = snapshot.trend.at(-1);
  const trend = Array.from({ length: 30 }, (_, index) => {
    const day = new Date(`${latest.date}T12:00:00`);
    day.setDate(day.getDate() - 29 + index);
    const date = `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, "0")}-${String(day.getDate()).padStart(2, "0")}`;
    return { ...latest, date };
  });
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      json: { ...(await response.json()), trend },
    });
  });
  for (const width of [1440, 1024, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    for (const days of [14, 30]) {
      await page.goto(`/#overview?days=${days}`);
      await expect(page.locator(".bar-column")).toHaveCount(days * 2);
      const fits = await page.locator(".chart").evaluateAll((charts) =>
        charts.every((chart) => {
          const bounds = chart.getBoundingClientRect();
          return (
            chart.scrollWidth <= chart.clientWidth &&
            Array.from(
              chart.querySelectorAll(".bar-stack, .bar-column > span"),
            ).every((item) => {
              const rect = item.getBoundingClientRect();
              return (
                rect.width > 0 &&
                rect.left >= bounds.left &&
                rect.right <= bounds.right
              );
            })
          );
        }),
      );
      expect(fits, `${width}px / ${days} 天图表溢出`).toBe(true);
    }
  }
});

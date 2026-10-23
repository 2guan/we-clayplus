import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("今日新闻与双图卦预览、原图和发送保持一致", async ({ page, request }) => {
  const before = (await (await request.get("/api/state")).json()).messages
    .length;
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "工作台 新闻", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("新闻类型", { exact: true })).toHaveValue("");
  await expect(dialog.getByLabel("关键词 / 内容")).toHaveCount(0);
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-image-gallery img")).toHaveCount(1);
  await expect(
    dialog.getByRole("link", { name: "查看原图：今日新闻", exact: true }),
  ).toHaveAttribute("href", /^\/media\//);
  await dialog.getByLabel("新闻类型", { exact: true }).selectOption("diagram");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-image-gallery img")).toHaveCount(2);
  await expect(dialog.locator(".business-result > pre")).toContainText("昨日");
  expect((await (await request.get("/api/state")).json()).messages.length).toBe(
    before,
  );
  await page.screenshot({
    path: "/tmp/weclay-news-preview.png",
    animations: "disabled",
  });
  page.once("dialog", (d) => d.accept());
  await dialog.getByRole("button", { name: "发送预览", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "已发送", exact: true }),
  ).toBeDisabled();
  const messages = (await (await request.get("/api/state")).json()).messages;
  expect(messages.length).toBe(before + 2);
  expect(messages.at(-2).kind).toBe("image");
  expect(messages.at(-2).content).toBe("今日图卦 1/2");
  expect(messages.at(-1).content).toBe("今日图卦 2/2");
  await dialog.getByLabel("新闻类型", { exact: true }).selectOption("rss");
  await dialog.getByLabel("关键词 / 内容").fill("科技");
  await dialog.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(dialog.locator(".business-image-gallery img")).toHaveCount(0);
  await expect(dialog.locator(".business-result > pre")).toContainText(
    "脱机示例",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("新闻配置保留原地址，计划可保存图卦类型", async ({ page, request }) => {
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 新闻", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("默认新闻类型", { exact: true })).toHaveValue(
    "today",
  );
  await expect(
    dialog.getByLabel("今日新闻图片地址", { exact: true }),
  ).toHaveValue(
    "https://cdn.jsdmirror.com/gh/vikiboss/60s-static-host@main/static/images/{date}.png",
  );
  await expect(dialog.getByLabel("图卦图片地址", { exact: true })).toHaveValue(
    "https://penti.5aihj.com/{month}/tugua/{date}{index}.jpg",
  );
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page
    .getByRole("button", { name: "工作台 定时任务", exact: true })
    .click();
  await dialog.getByRole("button", { name: "新建任务", exact: true }).click();
  await dialog.getByLabel("任务名称", { exact: true }).fill("每日图卦测试");
  await dialog.getByRole("combobox", { name: "调用插件", exact: true }).selectOption("news");
  await dialog
    .getByLabel("计划新闻类型", { exact: true })
    .selectOption("diagram");
  await dialog.getByRole("button", { name: "保存任务", exact: true }).click();
  await expect(dialog.locator(".business-job")).toContainText("每日图卦测试");
  const data = await (await request.get("/api/business")).json();
  expect(data.jobs[0].request.plugin).toBe("news");
  expect(data.jobs[0].request.mode).toBe("diagram");
});

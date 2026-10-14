// 文件名：insights.spec.ts
// 主要功能：验证洞察对象搜索、资料范围、示例提示词与响应式报告布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

// SSE 更新可能触发最后一次状态读取，关闭页面前等待路由处理结束。
test.afterEach(async ({ page }) => {
  await page.unrouteAll({ behavior: "wait" });
});

test("拼音选择好友，朋友圈只提交该好友资料，三类示例可直接填入", async ({
  page,
}) => {
  // 用同名非好友与群聊污染候选列表，确认筛选依据为关系类型和作者 ID。
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.contacts.push({
      ...state.contacts[1],
      id: "fixture-member",
      name: "林间群成员",
      kind: "member",
    });
    state.moments.push({
      ...state.moments[0],
      id: "fixture-moment",
      author_id: "fixture-member",
      content: "非好友内容不可提交",
    });
    await route.fulfill({ response, json: state });
  });
  await page.goto("/#insights");
  for (const [tab, example, text] of [
    ["会话总结", "提取待办", "负责人"],
    ["群聊分析", "参与情况", "发言次数"],
    ["朋友圈内容", "近期分享", "这位好友"],
  ]) {
    await page.getByRole("button", { name: tab, exact: true }).click();
    await page.getByRole("button", { name: example, exact: true }).click();
    await expect(page.getByLabel("你想重点了解什么？")).toHaveValue(
      new RegExp(text),
    );
  }
  const target = page.getByRole("combobox", { name: "分析对象" });
  await target.fill("");
  await expect(
    page
      .getByRole("listbox")
      .getByRole("option", { name: /设计日常|林间群成员/ }),
  ).toHaveCount(0);
  // 全拼与首字母都能匹配汉字，Enter 选择不会误提交分析表单。
  for (const query of ["linjian", "lj"]) {
    await target.fill(query);
    await expect(page.getByRole("option", { name: /林间/ })).toHaveCount(1);
    await target.press("Enter");
    await expect(target).toHaveValue("林间");
  }
  await page.getByLabel("朋友圈条数").fill("2000");
  await expect(page.locator(".insight-submit")).toContainText(
    "将读取最近 2000 条朋友圈及图片缩略图",
  );
  const submitted = page.waitForRequest(
    (r) => r.url().endsWith("/api/tasks") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  const body = (await submitted).postDataJSON();
  expect(body.target).toBe("mock-contact-1");
  expect(JSON.stringify(body.documents)).toContain("周末留给山野");
  expect(JSON.stringify(body.documents)).not.toContain("非好友内容不可提交");
  expect(JSON.stringify(body.documents)).not.toContain("读到一句话");
  await expect(page.locator(".insight-report")).toContainText("已完成");
  await expect(page.locator(".insight-report .task-result")).toBeVisible();
  // 输入不存在的对象不能沿用上一次的选择发送分析。
  await target.fill("完全不存在的好友");
  await expect(
    page.getByRole("status").filter({ hasText: "没有匹配的好友" }),
  ).toBeVisible();
  await target.press("Escape");
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "请从搜索结果中选择分析对象" }),
  ).toBeVisible();
});

test("自定义条数提交完整最近 2000 条，拒绝零和小数", async ({ page }) => {
  await page.route("**/api/state", async (route) => {
    const response = await route.fetch();
    const state = await response.json();
    state.messages = Array.from({ length: 2005 }, (_, i) => ({
      ...state.messages[0],
      id: `fixture-message-${i}`,
      content: `范围验证记录-${i}`,
      target: "mock-contact-0",
    }));
    await route.fulfill({ response, json: state });
  });
  await page.goto("/#insights");
  const range = page.getByRole("spinbutton", { name: "消息范围" });
  for (const invalid of ["0", "1.5", "-5"]) {
    await range.fill(invalid);
    expect(
      await range.evaluate((el: HTMLInputElement) => el.checkValidity()),
    ).toBe(false);
  }
  await range.fill("1000");
  await expect(page.locator(".insight-submit")).toContainText(
    "实际读取 1000 条消息",
  );
  await range.fill("2000");
  const submitted = page.waitForRequest(
    (r) => r.url().endsWith("/api/tasks") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  const { documents } = (await submitted).postDataJSON();
  const prompt = documents.map((m: { text: string }) => m.text).join("\n");
  expect(prompt.match(/范围验证记录-/g)).toHaveLength(2000);
  expect(prompt).toContain("范围验证记录-5\n");
  expect(prompt).toContain("范围验证记录-2004");
  expect(prompt).not.toContain("范围验证记录-4\n");
  await expect(page.locator(".insight-report")).toContainText("已完成");
});

test("洞察占满桌面可用区域，暗色和手机布局无横向溢出", async ({ page }) => {
  for (const width of [1440, 1920, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/#insights");
    const left = await page.locator(".insight-builder").boundingBox();
    const right = await page.locator(".insight-report").boundingBox();
    expect(left).not.toBeNull();
    expect(right).not.toBeNull();
    if (width > 800) {
      expect(Math.abs(left!.y - right!.y)).toBeLessThan(2);
      expect(right!.x).toBeGreaterThan(left!.x + left!.width);
      expect(right!.x + right!.width).toBeGreaterThan(width - 50);
      expect(right!.y + right!.height).toBeLessThan(865);
      const submit = await page
        .getByRole("button", { name: "生成分析报告", exact: true })
        .boundingBox();
      expect(submit!.y + submit!.height).toBeLessThan(
        left!.y + left!.height - 10,
      );
    } else {
      expect(right!.y).toBeGreaterThan(left!.y + left!.height);
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: `/tmp/weclay-insights-${width}.png`,
      fullPage: true,
      animations: "disabled",
    });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "切换主题，当前明亮" }).click();
  await page.screenshot({
    path: "/tmp/weclay-insights-dark.png",
    fullPage: true,
    animations: "disabled",
  });
});

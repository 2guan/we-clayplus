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
    await expect(page.getByLabel("原始提示词")).toHaveValue(
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
    (r) => r.url().endsWith("/api/reports") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  const body = (await submitted).postDataJSON();
  expect(body.target).toBe("mock-contact-1");
  expect(body.type).toBe("moments");
  expect(body.prompt).toBe(await page.getByLabel("原始提示词").inputValue());
  expect(body.range).toEqual({mode:"count",count:2000});
  expect(body.documents).toBeUndefined(); // 由后端读取已验证对象的资料。
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
  // 明确选定夹具会话，不能依赖联系人数组首项。
  await page.getByRole("combobox", { name: "分析对象" }).fill("mock-contact-0");
  await page.getByRole("listbox").getByRole("option").click();
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
    (r) => r.url().endsWith("/api/reports") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
  const body=(await submitted).postDataJSON();
  expect(body.range).toEqual({mode:"count",count:2000});
  expect(body.documents).toBeUndefined();
  // 浏览器仅提交范围；服务端按自身归档读取，不信任页面伪造的资料。

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
      // 设置区允许纵向滚动，提交按钮应能滚入可视区域。
      await page.getByRole("button", { name: "生成分析报告", exact: true }).scrollIntoViewIfNeeded();
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

test("删除报告支持取消、失败提示、详情删除及刷新后保持删除", async ({ page, request }) => {
  await page.goto("/#insights?tab=history");
  const rows = page.locator(".task-panel tbody tr");
  await expect(rows).toHaveCount(1);
  page.once("dialog", dialog => dialog.dismiss());
  await page.getByRole("button", { name: "删除报告", exact: true }).click();
  await expect(rows).toHaveCount(1);

  await page.route("**/api/action", route => route.request().postDataJSON().kind === "delete-report"
    ? route.fulfill({ status: 400, json: { error: "测试删除失败" } }) : route.continue());
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "删除报告", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "测试删除失败" })).toBeVisible();
  await expect(rows).toHaveCount(1);
  await page.unroute("**/api/action");

  await page.getByRole("button", { name: "查看任务", exact: true }).click();
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("dialog").getByRole("button", { name: "删除报告", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("暂无任务", { exact: true })).toBeVisible();
  await page.reload();
  await expect(rows).toHaveCount(0);
  expect((await (await request.get("/api/state")).json()).tasks).toHaveLength(0);

  // 报告预览只展示已记录对应类型的报告。
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  const response = await request.post("/api/tasks", { data: { kind: "insight", prompt: "测试预览删除", insight: { type: "conversation", prompt: "测试预览删除" } } });
  const { id } = await response.json();
  await expect.poll(async () => (await (await request.get("/api/state")).json()).tasks.find((t: { id: string }) => t.id === id)?.status).toBe("completed");
  await page.locator(".tabs").getByRole("button", { name: "会话总结", exact: true }).click();
  await expect(page.getByLabel("选择分析报告").locator("option")).toHaveCount(1);
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "删除报告", exact: true }).click();
  await expect(page.getByText("等待生成报告", { exact: true })).toBeVisible();
  await expect(page.getByLabel("选择分析报告")).toHaveCount(0);
});

test("历史报告、详情与预览展示完整信息，提示词两行预览与复制全文", async ({ page, request }) => {
  await page.addInitScript(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: {
    writeText: async (text: string) => {
      if (sessionStorage.getItem("fixture-copy-fail")) throw new Error("denied");
      sessionStorage.setItem("fixture-copied-prompt", text);
    },
  }}));
  const state = await (await request.get("/api/state")).json();
  const contact = state.contacts[0];
  const prompt = "分析重点：" + "请区分事实和推测，列出依据。".repeat(50) + "\n最后一行完整提示词";
  const ids: string[] = [];
  for (const type of ["conversation", "group", "moments"]) {
    const response = await request.post("/api/tasks", { data: {
      kind: "insight", target: contact.id, prompt: `${prompt}\n分析对象：附加信息`,
      insight: { type, prompt },
    }});
    expect(response.status()).toBe(202);
    const { id } = await response.json();
    ids.push(id);
    await expect.poll(async () => (await (await request.get("/api/state")).json()).tasks.find((t: { id: string }) => t.id === id)?.status).toBe("completed");
  }
  await page.goto("/#insights?tab=history");
  const table = page.locator(".report-table");
  for (const heading of ["任务类型", "任务提示词", "分析对象", "使用模型", "创建时间", "状态"]) {
    await expect(table.getByRole("columnheader", { name: heading, exact: true })).toBeVisible();
  }
  await expect(table.getByRole("button", { name: "未记录类型", exact: true })).toBeVisible();
  for (const label of ["会话总结", "群聊分析", "朋友圈内容"]) {
    const row = table.getByRole("row").filter({ has: page.getByRole("button", { name: label, exact: true }) });
    await expect(row).toContainText(contact.remark || contact.name);
    const preview = await row.locator(".report-prompt-link span").evaluate(el => ({ height: el.getBoundingClientRect().height, lineHeight: parseFloat(getComputedStyle(el).lineHeight) }));
    expect(preview.height).toBeCloseTo(preview.lineHeight * 2, 0);
    await row.getByRole("button", { name: "复制完整提示词" }).click();
    expect(await page.evaluate(() => sessionStorage.getItem("fixture-copied-prompt"))).toBe(prompt);
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await row.getByRole("button", { name: "查看任务提示词" }).click();
    await expect(page.getByRole("dialog").locator("pre")).toHaveText(prompt);
    await page.keyboard.press("Escape");
    await row.getByRole("button", { name: label, exact: true }).click();
    const info = page.getByRole("dialog").getByLabel("报告信息");
    await expect(info).toContainText(label);
    await expect(info).toContainText(contact.remark || contact.name);
    await expect(info).toContainText("已完成");
    await expect(info).toContainText("日常对话");
    await page.evaluate(() => sessionStorage.removeItem("fixture-copied-prompt"));
    await info.getByRole("button", { name: "复制完整提示词" }).click();
    expect(await page.evaluate(() => sessionStorage.getItem("fixture-copied-prompt"))).toBe(prompt);
    await expect(info.locator("details")).not.toHaveAttribute("open", "");
    await info.locator("summary").click();
    await expect(info.locator("pre")).toHaveText(prompt);
    await info.locator("summary").click();
    if (label === "会话总结") await page.screenshot({ path: "/tmp/weclay-report-detail.png", animations: "disabled" });
    await page.keyboard.press("Escape");
  }
  await page.screenshot({ path: "/tmp/weclay-report-history.png" });
  await page.locator(".tabs").getByRole("button", { name: "会话总结", exact: true }).click();
  for (const [index, label] of ["会话总结", "群聊分析", "朋友圈内容"].entries()) {
    await page.locator(".tabs").getByRole("button", { name: label, exact: true }).click();
    const select = page.getByLabel("选择分析报告");
    await expect(select.locator("option")).toHaveCount(1);
    await expect(select).toHaveValue(ids[index]);
    await expect(select.locator("option")).toHaveText(new RegExp(`^\\d{4}/\\d{2}/\\d{2} \\d{2}:\\d{2} · ${label} · ${contact.remark || contact.name}$`));
    await expect(page.locator(".insight-report").getByLabel("报告信息")).toContainText(label);
  }
  await page.locator(".tabs").getByRole("button", { name: "会话总结", exact: true }).click();
  await page.getByLabel("选择分析报告").selectOption(ids[0]);
  await expect(page.locator(".insight-report").getByLabel("报告信息")).toContainText("会话总结");
  await page.evaluate(() => sessionStorage.setItem("fixture-copy-fail", "1"));
  await page.locator(".insight-report").getByRole("button", { name: "复制完整提示词" }).click();
  await expect(page.getByRole("status").filter({ hasText: "复制失败，请检查浏览器剪贴板权限" })).toBeVisible();
  await page.evaluate(() => sessionStorage.removeItem("fixture-copy-fail"));
  await page.locator(".insight-report summary").click();
  await expect(page.locator(".insight-report pre.report-prompt-full")).toHaveText(prompt);
  await page.locator(".insight-report summary").click();
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.reload();
    await page.getByLabel("选择分析报告").selectOption(ids[0]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width < 800) {
      const metadata = page.locator(".insight-report").getByLabel("报告信息");
      await metadata.scrollIntoViewIfNeeded();
      await expect(metadata).toBeInViewport();
    }
    await page.screenshot({ path: `/tmp/weclay-report-metadata-${width}.png`, animations: "disabled" });
  }
});

test.describe("消息时间段", () => {
  test.use({ timezoneId: "Asia/Shanghai" });
  test("整行条数输入与本地时间筛选，包含结束分钟且不受条数限制", async ({ page }) => {
    await page.route("**/api/state", async route => {
      const response = await route.fetch();
      const state = await response.json();
      const base = { ...state.messages[0], target: "mock-contact-0" };
      state.messages = [
        { ...base, id: "before", content: "范围之前", time: "2026-10-14T15:59:59Z" },
        ...Array.from({ length: 61 }, (_, i) => ({ ...base, id: `within-${i}`, content: `时间范围记录-${i}`, time: new Date(Date.parse("2026-10-14T16:00:00Z") + i * 1000).toISOString() })),
        { ...base, id: "end-minute", content: "结束分钟最后一刻", time: "2026-10-14T16:01:59.999Z" },
        { ...base, id: "after", content: "范围之后", time: "2026-10-14T16:02:00Z" },
        { ...base, id: "other", target: "mock-contact-1", content: "其他会话不可提交", time: "2026-10-14T16:00:30Z" },
        { ...base, id: "invalid", content: "无效时间不可提交", time: "invalid" },
      ];
      await route.fulfill({ response, json: state });
    });
    await page.goto("/#insights");
    await page.getByRole("combobox", { name: "分析对象" }).fill("mock-contact-0");
    await page.getByRole("listbox").getByRole("option").click();
    const field = page.locator(".insight-range-fields");
    const input = page.getByRole("spinbutton", { name: "消息范围" });
    expect(Math.abs((await field.boundingBox())!.width - (await input.boundingBox())!.width)).toBeLessThan(2);
    await expect(field.getByText("最近", { exact: true })).toHaveCount(0);
    await expect(field.getByText("条", { exact: true })).toHaveCount(0);
    await input.fill("0");
    await field.getByRole("button", { name: "时间段", exact: true }).click();
    await page.getByLabel("开始时间", { exact: true }).fill("2026-10-15T00:00");
    const end = page.getByLabel("结束时间", { exact: true });
    await end.fill("2026-10-14T23:59");
    expect(await end.evaluate((el: HTMLInputElement) => el.checkValidity())).toBe(false);
    await expect(page.locator(".insight-submit")).toContainText("结束时间不能早于开始时间");
    await end.fill("");
    await expect(page.locator(".insight-submit")).toContainText("请选择开始和结束时间");
    await end.fill("2026-10-15T00:01");
    await expect(page.locator(".insight-submit")).toContainText("实际读取 62 条消息");
    await page.screenshot({ path: "/tmp/weclay-insight-time-range.png", animations: "disabled" });
    const submitted = page.waitForRequest(r => r.url().endsWith("/api/reports") && r.method() === "POST");
    await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
    const body = (await submitted).postDataJSON();
    expect(body.range.mode).toBe("time");
    expect(new Date(body.range.end).getTime()-new Date(body.range.start).getTime()).toBe(120000);
    expect(body.documents).toBeUndefined();
    await page.getByLabel("开始时间", { exact: true }).fill("2026-10-16T00:00");
    await end.fill("2026-10-16T00:01");
    await page.getByRole("button", { name: "生成分析报告", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "所选时间段内暂无可分析的已归档消息" })).toBeVisible();
    await field.getByRole("button", { name: "条数", exact: true }).click();
    await expect(input).toHaveValue("0");
    await input.fill("10");
    await expect(page.locator(".insight-submit")).toContainText("实际读取 10 条消息");
    await page.locator(".tabs").getByRole("button", { name: "群聊分析", exact: true }).click();
    await expect(field.getByRole("button", { name: "时间段", exact: true })).toBeVisible();
    await page.locator(".tabs").getByRole("button", { name: "朋友圈内容", exact: true }).click();
    const moments = page.getByRole("spinbutton", { name: "朋友圈条数" });
    expect(Math.abs((await field.boundingBox())!.width - (await moments.boundingBox())!.width)).toBeLessThan(2);
    await expect(field.getByRole("button", { name: "时间段", exact: true })).toHaveCount(0);
    await page.setViewportSize({ width: 390, height: 900 });
    await page.locator(".tabs").getByRole("button", { name: "群聊分析", exact: true }).click();
    await field.getByRole("button", { name: "时间段", exact: true }).click();
    await field.scrollIntoViewIfNeeded();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: "/tmp/weclay-insight-time-range-mobile.png", animations: "disabled" });
  });
});

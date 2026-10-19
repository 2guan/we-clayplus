// 文件名：models.spec.ts
// 主要功能：验证六页签模型中心、厂商目录、流水线持久化、人设优化和手机端布局。
// 作者：2guan
import { test, expect } from "@playwright/test";

test.beforeEach(async ({ request }) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
});

test("厂商模型发现、独立参数、流水线与测活", async ({ page, request }) => {
  await page.goto("/#models");
  await page.getByRole("button", { name: "添加厂商", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("厂商名称", { exact: true }).fill("本机 MiMo");
  await dialog.getByLabel("API 协议", { exact: true }).selectOption("mimo");
  await dialog.getByRole("button", { name: "获取模型", exact: true }).click();
  await dialog.getByLabel("mock-chat", { exact: true }).check();
  await dialog.getByRole("button", { name: "保存厂商", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(
    page.locator(".vendor-card").filter({ hasText: "本机 MiMo" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "配置 mock-chat", exact: true })
    .click();
  await dialog.getByLabel("超时时间（秒，0 继承）").fill("12");
  await dialog.getByLabel("温度（留空由厂商决定）").fill("0.4");
  await dialog.getByLabel("思维链", { exact: true }).selectOption("on");
  await dialog.getByLabel("上下文条数（0 继承）").fill("20");
  await dialog.getByLabel("上下文 Token 预算（0 继承）").fill("4096");
  await dialog.getByRole("button", { name: "保存参数", exact: true }).click();
  const state = await (await request.get("/api/state")).json();
  const model = state.models.find(
    (m: { model: string }) => m.model === "mock-chat",
  );
  expect(model.timeout).toBe(12);
  expect(model.temperature).toBe(0.4);
  expect(model.thinking).toBe("on");
  expect(model.context).toBe(20);
  expect(model.context_tokens).toBe(4096);
  await expect(page.locator(".vendor-model").filter({ hasText: "mock-chat" }).getByText("思维链开启", { exact: true })).toBeVisible();
  await page.getByLabel("添加流水线模型").selectOption(model.id);
  await page
    .getByRole("button", { name: "前移 mock-chat", exact: true })
    .click();
  await page.getByRole("button", { name: "保存顺序", exact: true }).click();
  await page.reload();
  await expect(page.locator(".pipeline-step").first()).toContainText(
    "mock-chat",
  );
  await page
    .getByRole("button", { name: "测活 mock-chat", exact: true })
    .click();
  await expect(
    page.locator(".vendor-model").filter({ hasText: "mock-chat" }),
  ).toContainText("已完成");
  await page.screenshot({
    path: "test-results/models-chat.png",
    fullPage: true,
  });
});

test("人设默认切换、优化预览及行为名单保存", async ({ page, request }) => {
  await page.goto("/#models?tab=prompts");
  await page
    .getByRole("button", { name: "设为默认 工作助手", exact: true })
    .click();
  await expect(page.locator(".persona-item").first()).toContainText("工作助手");
  await page.locator(".persona-select").first().click();
  await page.getByLabel("修改要求", { exact: true }).fill("更简洁，保留中文。");
  const old = await page.getByLabel("人设提示词全文").inputValue();
  await page
    .getByRole("button", { name: "使用默认模型优化", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "采用优化结果", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("人设提示词全文")).toHaveValue(old);
  await page.getByRole("button", { name: "采用优化结果", exact: true }).click();
  await page.getByRole("button", { name: "保存人设", exact: true }).click();
  await page.getByRole("button", { name: "行为与任务", exact: true }).click();
  await page.getByLabel("搜索行为", { exact: true }).fill("SetRemark");
  await page
    .getByRole("button", { name: /修改备注 SetRemark/ })
    .first()
    .click();
  await page
    .getByLabel("此操作的系统提示词")
    .fill("仅在用户明确要求时修改备注。");
  await page.getByLabel("目标 ID", { exact: true }).fill("mock-contact-1");
  await page.getByRole("button", { name: "保存行为配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("行为权限已保存");
  const state = await (await request.get("/api/state")).json();
  expect(
    state.policies.some((p: { targets: string[] }) =>
      p.targets.includes("mock-contact-1"),
    ),
  ).toBe(true);
});

test("六个页签在手机亮暗主题下无溢出", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#models");
  for (const mode of ["light", "dark"]) {
    await page.evaluate(
      (theme) => document.documentElement.setAttribute("data-theme", theme),
      mode,
    );
    for (const label of [
      "对话模型",
      "语音模型",
      "图片模型",
      "视频模型",
      "提示词管理",
      "行为与任务",
    ]) {
      await page.getByRole("button", { name: label, exact: true }).click();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        `${mode} / ${label}`,
      ).toBe(true);
    }
  }
  await page.getByRole("button", { name: "语音模型", exact: true }).click();
  await page.getByRole("button", { name: "VoiceClone", exact: true }).click();
  await expect(
    page.getByText("上传音色样本 · WAV / MP3", { exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/models-mobile-dark.png",
    fullPage: true,
  });
});

// 三种语音模式走同一后台队列，但使用独立的实际模型及输入参数。
test("三种 MiMo 语音模式与图片垫图在脱机环境可完整预览", async ({
  page,
  request,
}) => {
  await request.post("/api/action", {
    data: {
      kind: "vendor",
      vendor: {
        name: "语音示例",
        provider: "mimo",
        endpoint: "https://api.xiaomimimo.com/v1",
      },
      models: [
        "mimo-v2.5-tts",
        "mimo-v2.5-tts-voicedesign",
        "mimo-v2.5-tts-voiceclone",
      ].map((model) => ({
        model,
        name: model,
        capabilities: ["tts"],
        enabled: true,
      })),
    },
  });
  await page.goto("/#models?tab=tts");
  await page.getByRole("button", { name: "VoiceDesign", exact: true }).click();
  await page
    .getByRole("textbox", { name: "声音设计提示词", exact: true })
    .fill("轻柔自然的中文女声。");
  await page.getByRole("button", { name: "生成试听", exact: true }).click();
  await expect(page.locator(".result-panel audio")).toBeVisible();
  await page.getByRole("button", { name: "VoiceClone", exact: true }).click();
  await page
    .locator('input[type="file"]')
    .setInputFiles("../assets/sample-audio.wav");
  await page
    .getByRole("button", { name: "保存为默认克隆样本", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText("默认音色样本已保存");
  await page.getByRole("button", { name: "生成试听", exact: true }).click();
  await expect(page.locator(".result-panel audio")).toBeVisible();
  await page.getByRole("button", { name: "图片模型", exact: true }).click();
  await page
    .locator('input[type="file"]')
    .last()
    .setInputFiles({
      name: "reference.png",
      mimeType: "image/png",
      buffer: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
        "base64",
      ),
    });
  await expect(
    page.getByRole("img", { name: "默认垫图", exact: true }),
  ).toBeVisible();
  await page.getByLabel(/自拍 \/ 自己的照片/).check();
  await page.getByRole("button", { name: "生成预览", exact: true }).click();
  await expect(page.locator(".result-panel img")).toBeVisible();
});

// 三种媒体流水线分别持久化；视频工作台覆盖目录、参考图、参数、播放与下载。
test("媒体流水线独立保存，视频工作台完整预览", async ({ page, request }) => {
  await page.goto("/#models?tab=video");
  await expect(page.getByRole("heading", { name: "视频工作台" })).toBeVisible();
  await page.getByRole("button", { name: "添加厂商", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("厂商名称", { exact: true }).fill("视频示例服务");
  await dialog
    .getByLabel("API 地址", { exact: true })
    .fill("https://video.example.invalid/v1");
  await dialog.getByRole("button", { name: "获取模型", exact: true }).click();
  await dialog.getByLabel("mock-video", { exact: true }).check();
  await dialog.getByRole("button", { name: "保存厂商", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const state = await (await request.get("/api/state")).json();
  const video = state.models.find(
    (m: { model: string }) => m.model === "mock-video",
  );
  // 初次兼容路由按可用模型推导；前移后显式保存视频顺序。
  await page
    .getByRole("button", { name: "前移 mock-video", exact: true })
    .click();
  await page.getByRole("button", { name: "保存顺序", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("流水线已保存");
  for (const [label, kind] of [
    ["语音模型", "tts"],
    ["图片模型", "image"],
  ]) {
    await page.getByRole("button", { name: label, exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "模型调用流水线" }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "移出流水线 内容创作", exact: true })
      .click();
    await page.getByRole("button", { name: "保存顺序", exact: true }).click();
    await expect
      .poll(
        async () =>
          (await (await request.get("/api/state")).json()).pipelines[kind],
      )
      .toEqual([]);
  }
  await page.getByRole("button", { name: "视频模型", exact: true }).click();
  await page.reload();
  const saved = await (await request.get("/api/state")).json();
  expect(saved.pipelines.video_generation).toEqual([video.id, "demo-create"]);
  expect(saved.pipeline).toEqual(["demo-chat"]);
  await page
    .getByLabel("本次视频尺寸", { exact: true })
    .selectOption("720x1280");
  await page.getByLabel("本次视频时长", { exact: true }).selectOption("8");
  await page
    .locator('input[type="file"]')
    .last()
    .setInputFiles({
      name: "reference.png",
      mimeType: "image/png",
      buffer: Buffer.from(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=",
        "base64",
      ),
    });
  await expect(
    page.getByRole("img", { name: "默认参考图", exact: true }),
  ).toBeVisible();
  const outgoing = page.waitForRequest(
    (r) => r.url().endsWith("/api/tasks") && r.method() === "POST",
  );
  await page.getByRole("button", { name: "生成预览", exact: true }).click();
  expect((await outgoing).postDataJSON()).toMatchObject({
    kind: "video_generation",
    size: "720x1280",
    seconds: "8",
    model: "",
  });
  const preview = page.locator(".result-panel video");
  await expect(preview).toBeVisible();
  await expect
    .poll(() => preview.evaluate((v: HTMLVideoElement) => v.readyState))
    .toBeGreaterThan(0);
  // 实际解码播放后暂停，不能仅凭出现 video 标签就认定预览可用。
  await preview.evaluate(async (v: HTMLVideoElement) => {
    v.muted = true;
    await v.play();
  });
  await expect
    .poll(() => preview.evaluate((v: HTMLVideoElement) => v.currentTime))
    .toBeGreaterThan(0);
  await preview.evaluate((v: HTMLVideoElement) => v.pause());
  await expect(page.getByRole("link", { name: "下载视频" })).toHaveAttribute(
    "href",
    "/sample-video.mp4",
  );
  await page.locator(".studio-heading").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "test-results/video-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  // 等待抽屉退场动画完成，避免截到从桌面布局过渡的中间帧。
  await expect
    .poll(() =>
      page
        .locator(".sidebar")
        .evaluate((el) => el.getBoundingClientRect().right),
    )
    .toBeLessThanOrEqual(0);
  await preview.scrollIntoViewIfNeeded();
  await page.evaluate(() =>
    document.documentElement.setAttribute("data-theme", "dark"),
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/video-mobile-dark.png",
    fullPage: true,
  });
});

// 下拉预设仅预填连接信息，仍允许自定义名称和地址，不继承其他厂商的未保存密钥。
test("厂商预设与手动输入", async ({ page }) => {
  await page.goto("/#models");
  await page.getByRole("button", { name: "添加厂商", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const name = dialog.getByLabel("厂商名称", { exact: true });
  await expect(name).toHaveAttribute("list", "model-vendor-presets");
  await name.fill("DeepSeek");
  await expect(dialog.getByLabel("API 地址", { exact: true })).toHaveValue("https://api.deepseek.com");
  await dialog.getByLabel("API Key", { exact: true }).fill("fixture-secret");
  await name.fill("Google Gemini");
  await expect(dialog.getByLabel("API 协议", { exact: true })).toHaveValue("gemini");
  await expect(dialog.getByLabel("API Key", { exact: true })).toHaveValue("");
  await name.fill("我的自定义厂商");
  await dialog.getByLabel("API 地址", { exact: true }).fill("https://fixture.invalid/v1");
  await expect(name).toHaveValue("我的自定义厂商");
  await expect(dialog.getByLabel("API 地址", { exact: true })).toHaveValue("https://fixture.invalid/v1");
});

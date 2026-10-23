import { test, expect } from "@playwright/test";
test("业务插件默认空黑名单，保存与切换白名单保持一致", async ({
  page,
  request,
}) => {
  await request.post("/api/action", { data: { kind: "mock-reset" } });
  const plugins = await (await request.get("/api/plugins")).json();
  for (const p of plugins) {
    if (["dashboard", "ai", "prompt_guard", "context_cleaner"].includes(p.id))
      continue;
    const doc = await (
      await request.get(`/api/plugin-config?id=${p.id}`)
    ).json();
    expect(doc.values[p.id].scope).toBe("blacklist");
    expect(doc.values[p.id].targets).toBe("");
  }
  await page.goto("/#plugins");
  await page.getByRole("button", { name: "配置 海龟汤", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const mode = dialog.getByLabel("名单模式", { exact: true });
  await expect(mode).toHaveValue("blacklist");
  await expect(dialog.getByText(/空黑名单全局生效/)).toBeVisible();
  await dialog
    .getByLabel("黑白名单", { exact: true })
    .fill("fixture-excluded-group@chatroom");
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("已保存");
  let saved = await (
    await request.get("/api/plugin-config?id=turtlesoup")
  ).json();
  expect(saved.values.turtlesoup.scope).toBe("blacklist");
  expect(saved.values.turtlesoup.targets).toBe(
    "fixture-excluded-group@chatroom",
  );
  await mode.selectOption("whitelist");
  await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect
    .poll(async () => {
      saved = await (
        await request.get("/api/plugin-config?id=turtlesoup")
      ).json();
      return saved.values.turtlesoup.scope;
    })
    .toBe("whitelist");
  await page.reload();
  await page.getByRole("button", { name: "配置 海龟汤", exact: true }).click();
  await expect(
    page.getByRole("dialog").getByLabel("名单模式", { exact: true }),
  ).toHaveValue("whitelist");
});

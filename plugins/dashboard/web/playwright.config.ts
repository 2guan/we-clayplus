// 文件名：playwright.config.ts
// 主要功能：使用独立回环端口启动 Mock，验证页面与桌面、手机的响应式交互。
// 作者：2guan
import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  workers: 1,
  fullyParallel: false,
  use: { baseURL: "http://127.0.0.1:18899", trace: "retain-on-failure" },
  webServer: {
    command:
      "WE_CLAY_MOCK_ADDRESS=127.0.0.1:18899 WE_CLAY_MOCK_DATA_DIR=/tmp/weclay-playwright-fixtures make -C ../../.. mock",
    url: "http://127.0.0.1:18899/api/state",
    reuseExistingServer: false,
    timeout: 120000,
  },
});

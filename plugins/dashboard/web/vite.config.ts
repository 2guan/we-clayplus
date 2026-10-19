// 文件名：vite.config.ts
// 主要功能：构建可嵌入 Go 的控制台资源，并为编译产物保留作者与来源说明。
// 作者：2guan
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [
    react(),
    {
      name: "weclay-source-header",
      // 压缩完成后再附加标头，避免普通注释被压缩器移除；资源仍由同源服务提供。
      generateBundle(_, bundle) {
        for (const [name, output] of Object.entries(bundle)) {
          const header = `/*! 文件名：${name}\n * 主要功能：控制台编译产物，源代码位于 web/src，请勿手工编辑。\n * 作者：2guan\n */\n`;
          if (output.type === "chunk") output.code = header + output.code;
          else if (name.endsWith(".css"))
            output.source = header + output.source;
        }
      },
    },
  ],
  build: { outDir: "../assets", emptyOutDir: true },
  server: {
    proxy: {
      // 保留浏览器访问的 Host，使后台能够正确比较 Origin，继续拦截跨站请求。
      // 字符串简写默认改写 Host，会导致 8209 的合法请求被 8208 后台拒绝。
      "/api": { target: "http://127.0.0.1:8208", changeOrigin: false },
      "/media": { target: "http://127.0.0.1:8208", changeOrigin: false },
    },
  },
});

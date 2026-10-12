// 文件名: examples/bot/main.go
// 主要功能: 本地机器人与插件运行入口，一键启动 Core、扫码登录并装载 Dashboard 与 AI 插件
// 作者: 2guan

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/we-clay/we-clay/gateway"
)

func main() {
	// 捕获系统中断信号 (Ctrl+C / SIGTERM)，支持优雅退出并清理子进程
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	coreVersion := os.Getenv("WE_CLAY_CORE_VERSION")
	if coreVersion == "" {
		coreVersion = "v0.3.0"
	}

	pluginsDir := os.Getenv("WE_CLAY_PLUGINS_DIR")
	if pluginsDir == "" {
		pluginsDir = "./bin"
	}

	owner := os.Getenv("WE_CLAY_OWNER")

	cfg := gateway.Config{
		Core: &gateway.CoreConfig{
			Repository: "we-clay/core-release",
			Version:    coreVersion,
		},
		PluginsDir: pluginsDir,
		DataDir:    "./runtime/gateway/data",
		Owner:      owner,
	}

	slog.Info("正在启动 we-clayplus 网关服务...",
		"core_version", coreVersion,
		"plugins_dir", pluginsDir)
	slog.Info("💡 提示：Core 启动后请在浏览器打开 http://127.0.0.1:8086 完成微信 Mac 扫码登录")
	slog.Info("💡 提示：登录成功后 Dashboard 控制台将托管在 http://127.0.0.1:8080")

	err := gateway.Run(ctx, cfg, gateway.WithHandlers(gateway.Handlers{
		OnReady: func(ctx context.Context, client *gateway.Client) error {
			self := client.Contact.GetSelf()
			name := "微信用户"
			if self != nil && self.Nickname != "" {
				name = self.Nickname
			}
			slog.Info("🎉 微信已登录就绪！", "nickname", name)
			slog.Info("🚀 控制台已就绪：请访问 http://127.0.0.1:8080 查看高仿微信控制台与AI插件")
			return nil
		},
	}))

	if err != nil {
		slog.Error("网关运行异常退出", "err", err)
		os.Exit(1)
	}

	slog.Info("网关服务已安全停止")
}

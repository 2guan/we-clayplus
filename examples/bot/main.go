// 文件名: examples/bot/main.go
// 主要功能: 本地机器人与插件运行入口，一键启动 Core、扫码登录并装载 Dashboard 与 AI 插件
// 作者: 2guan

package main

import (
	"context"
	"flag"
	"fmt"
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

	// 支持 -mock 或 -offline 命令行参数启动免扫码脱机模式
	mockFlag := flag.Bool("mock", false, "以脱机/Mock 模式启动（免扫码、免 Core、零风控风险）")
	offlineFlag := flag.Bool("offline", false, "以脱机/Mock 模式启动（免扫码、免 Core、零风控风险）")
	flag.Parse()

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

	loginPort := os.Getenv("WE_CLAY_LOGIN_PORT")
	if loginPort == "" {
		loginPort = "5668"
		os.Setenv("WE_CLAY_LOGIN_PORT", loginPort)
	}
	dashboardPort := os.Getenv("WE_CLAY_DASHBOARD_PORT")
	if dashboardPort == "" {
		dashboardPort = "5699"
		os.Setenv("WE_CLAY_DASHBOARD_PORT", dashboardPort)
	}
	corePort := os.Getenv("WE_CLAY_CORE_PORT")
	if corePort == "" {
		corePort = "5666"
		os.Setenv("WE_CLAY_CORE_PORT", corePort)
	}

	isMock := *mockFlag || *offlineFlag || os.Getenv("WE_CLAY_MOCK") == "true" || os.Getenv("WE_CLAY_OFFLINE") == "true"
	if isMock {
		cfg.Offline = true
		cfg.Core = nil
		slog.Info("🚀 正在以【脱机 Mock 预览模式】启动 we-clayplus 服务...",
			"mode", "offline_mock",
			"plugins_dir", pluginsDir)
		slog.Info("💡 提示：脱机模式跳过 Core 进程与微信扫码，直接使用本地缓存与 Mock 数据，零风控风险")
		slog.Info(fmt.Sprintf("💡 提示：微信登录页面（脱机引导）托管在 http://127.0.0.1:%s", loginPort))
		slog.Info(fmt.Sprintf("💡 提示：Mock Core (gRPC) 监听端口: %s", corePort))
		slog.Info(fmt.Sprintf("💡 提示：Dashboard 控制台托管在 http://127.0.0.1:%s", dashboardPort))
	} else {
		slog.Info("正在启动 we-clayplus 网关服务...",
			"core_version", coreVersion,
			"plugins_dir", pluginsDir)
		slog.Info(fmt.Sprintf("💡 提示：Core 启动后请在浏览器打开 http://127.0.0.1:%s 完成微信 Mac 扫码登录", loginPort))
		slog.Info(fmt.Sprintf("💡 提示：登录成功后 Dashboard 控制台将托管在 http://127.0.0.1:%s", dashboardPort))
	}

	err := gateway.Run(ctx, cfg, gateway.WithHandlers(gateway.Handlers{
		OnReady: func(ctx context.Context, client *gateway.Client) error {
			self := client.Contact.GetSelf()
			name := "微信用户"
			if self != nil && self.Nickname != "" {
				name = self.Nickname
			}
			if isMock {
				slog.Info("🎉 脱机 Mock 服务已就绪！", "nickname", name, "mode", "offline_preview")
			} else {
				slog.Info("🎉 微信已登录就绪！", "nickname", name)
			}
			slog.Info(fmt.Sprintf("🚀 控制台已就绪：请访问 http://127.0.0.1:%s 查看高仿微信控制台与AI插件", dashboardPort))
			return nil
		},
	}))

	if err != nil {
		slog.Error("网关运行异常退出", "err", err)
		os.Exit(1)
	}

	slog.Info("网关服务已安全停止")
}

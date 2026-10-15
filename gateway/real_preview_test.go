// 文件名：real_preview_test.go
// 主要功能：显式启动真实微信人工验收环境，复用 Run 并隔离脱机演示数据。
// 作者：2guan
package gateway_test

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/sdk/control"
)

// TestRealPreview 仅由 make run 开启；普通测试不会下载 core 或连接微信。
func TestRealPreview(t *testing.T) {
	if os.Getenv("WE_CLAY_REAL_PREVIEW") != "1" {
		t.Skip("仅用于 make run 人工验证真实微信")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("真实控制台：http://127.0.0.1:8208；微信登录：http://127.0.0.1:8201")
	err = gateway.Run(ctx, realPreviewConfig(root))
	if err != nil {
		t.Fatal(err)
	}
}

func realPreviewConfig(root string) gateway.Config {
	// 固定版本、Mac 设备模式由 Run 管理；不导入 core 源码。
	version := os.Getenv("WE_CLAY_CORE_VERSION")
	if version == "" {
		version = "v0.4.3"
	}
	dataDir := os.Getenv("WE_CLAY_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(root, "runtime", "core")
	}
	cfg := gateway.Config{
		DataDir:    filepath.Join(root, "runtime", "gateway", "data"),
		PluginsDir: filepath.Join(root, "runtime", "gateway", "plugins"),
		Core: &gateway.CoreConfig{
			Version:      version,
			OSS:          &gateway.OSSConfig{Bucket: "weclay", Region: "cn-beijing", Prefix: "core"},
			CacheDir:     filepath.Join(root, "runtime", "core-bin"),
			DataDir:      dataDir,
			LoginAddress: "127.0.0.1:8201",
			// 人工验收留足扫码时间；中断时仍立即回收本次 core 子进程。
			LoginTimeout: 24 * time.Hour,
		},
		Console: &control.Config{Address: "127.0.0.1:8208", DataDir: filepath.Join(root, "runtime", "console")},
	}
	// 接管后台时连接已有 core，退出和更新后台均不停止微信进程。
	if os.Getenv("WE_CLAY_ATTACH_CORE") == "1" {
		cfg.Core = nil
	}
	return cfg
}

func TestRealPreviewConfiguration(t *testing.T) {
	t.Setenv("WE_CLAY_CORE_VERSION", "")
	t.Setenv("WE_CLAY_DATA_DIR", "")
	t.Setenv("WE_CLAY_ATTACH_CORE", "")
	root := t.TempDir()
	cfg, err := realPreviewConfig(root).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Core.Version != "v0.4.3" || cfg.Core.OSS == nil || cfg.Core.OSS.Bucket != "weclay" || cfg.Core.DataDir != filepath.Join(root, "runtime", "core") {
		t.Fatal("默认版本、下载源或账号目录不匹配")
	}
	t.Setenv("WE_CLAY_CORE_VERSION", "v0.4.1")
	t.Setenv("WE_CLAY_DATA_DIR", filepath.Join(root, "existing-core"))
	cfg = realPreviewConfig(root)
	if cfg.Core.Version != "v0.4.1" || cfg.Core.DataDir != filepath.Join(root, "existing-core") {
		t.Fatal("未保留显式版本或账号目录")
	}
	t.Setenv("WE_CLAY_ATTACH_CORE", "1")
	if realPreviewConfig(root).Core != nil {
		t.Fatal("attach 不应下载或启动 core")
	}
}

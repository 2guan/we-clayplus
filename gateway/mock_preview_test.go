// 文件名：mock_preview_test.go
// 主要功能：通过显式环境开关启动人工验收用的脱机控制台，不新增上层 main 或 CLI。
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

// TestMockPreview 默认跳过；make mock 明确开启后一直运行至终端中断。
func TestMockPreview(t *testing.T) {
	if os.Getenv("WE_CLAY_MOCK_PREVIEW") != "1" {
		t.Skip("仅用于 make mock 手动预览")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	address := os.Getenv("WE_CLAY_MOCK_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8208"
	}
	dataDir := os.Getenv("WE_CLAY_MOCK_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(root, "runtime", "mock-console")
	}
	t.Log("脱机控制台：http://" + address)
	if err = gateway.Run(ctx, gateway.Config{Console: &control.Config{Mock: true, Address: address, DataDir: dataDir}}); err != nil {
		t.Fatal(err)
	}
}

// TestMockRunBypassesCore 以不可用 core 地址运行，证明 Mock 先于下载、登录和 gRPC 建连。
func TestMockRunBypassesCore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	dir := t.TempDir()
	err := gateway.Run(ctx, gateway.Config{CoreAddress: "127.0.0.1:1", DataDir: filepath.Join(dir, "gateway"), Console: &control.Config{Mock: true, Address: "127.0.0.1:0", DataDir: filepath.Join(dir, "mock")}})
	if err != nil {
		t.Fatalf("Mock 不应访问不可用的 core：%v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "gateway")); !os.IsNotExist(err) {
		t.Fatal("Mock 不应建立微信账号缓存")
	}
}

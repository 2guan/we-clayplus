package gateway_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
)

func TestIndependentCoreDeviceNameArgumentAndRestartRequirement(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.Core.StartupTimeout = 15 * time.Second
	cfg.Core.LoginTimeout = 15 * time.Second
	cfg.Core.DeviceName = "我的 Mac - 机器人"
	cleanupIndependent(t, cfg)
	independentRound(t, cfg)
	raw, err := os.ReadFile(filepath.Join(cfg.Core.DataDir, "fixture-device-name"))
	if err != nil || string(raw) != cfg.Core.DeviceName {
		t.Fatalf("device name not passed intact: %q %v", raw, err)
	}
	pid, token := independentRecord(t, cfg)
	independentRound(t, cfg)
	changed := cfg
	coreConfig := *cfg.Core
	changed.Core = &coreConfig
	changed.Core.DeviceName = "另一个名称"
	if err = gateway.Run(context.Background(), changed); err == nil || !strings.Contains(err.Error(), "设备名称") {
		t.Fatalf("silently ignored new name on running core: %v", err)
	}
	pid2, token2 := independentRecord(t, cfg)
	if pid2 != pid || token2 != token {
		t.Fatal("name mismatch replaced core or token")
	}
	// 留空仅复用当前 core；停止操作不能被待应用的新名称阻止。
	blank := cfg
	blankConfig := *cfg.Core
	blank.Core = &blankConfig
	blank.Core.DeviceName = ""
	independentRound(t, blank)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if err = gateway.StopCore(ctx, changed); err != nil {
		t.Fatal("pending display-name change prevented explicit stop", err)
	}
}

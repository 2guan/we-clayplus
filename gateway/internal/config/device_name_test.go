package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCoreDeviceNameFromTOML(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.toml")
	raw := fmt.Sprintf("[core]\nversion = 'v1'\ndata_dir = %q\ndevice_name = '我的 Mac 机器人'\n", t.TempDir())
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.Core.DeviceName != "我的 Mac 机器人" {
		t.Fatal("device name config lost", err)
	}
}

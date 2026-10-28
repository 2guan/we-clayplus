package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCoreDeviceTypeDefaultsAndValidation(t *testing.T) {
	for _, typ := range []string{"", "mac", "win"} {
		cfg, err := (CoreConfig{Version: "v1", DataDir: t.TempDir(), DeviceType: typ}).Resolve()
		want := typ
		if want == "" {
			want = "mac"
		}
		if err != nil || cfg.DeviceType != want {
			t.Fatalf("type %q: got %q, err %v", typ, cfg.DeviceType, err)
		}
	}
	for _, typ := range []string{"windows", "windows_uwp", "windows_unified", "WIN", " win", "ipad", "win\n"} {
		if _, err := (CoreConfig{Version: "v1", DataDir: t.TempDir(), DeviceType: typ}).Resolve(); err == nil {
			t.Errorf("accepted device type %q", typ)
		}
	}
}

func TestCoreDeviceTypeFromTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	raw := fmt.Sprintf("[core]\nversion = 'v1'\ndata_dir = %q\ndevice_type = 'win'\n", t.TempDir())
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Core.DeviceType != "win" {
		t.Fatalf("device type lost: %v", err)
	}
}

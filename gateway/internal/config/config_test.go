package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGRPCConfigurationAndNoHTTPFallback(t *testing.T) {
	t.Setenv("WE_CLAY_CORE_TOKEN", "synthetic-token")
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(`core_address = "127.0.0.1:8088"`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.CoreToken != "synthetic-token" {
		t.Fatal("gRPC config", err)
	}
	if err = os.WriteFile(p, []byte(`core_url = "http://127.0.0.1:8086"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(p); err == nil {
		t.Fatal("obsolete HTTP config accepted")
	}
	for _, addr := range []string{"example.test:8088", "0.0.0.0:8088", "127.0.0.1:0", "127.0.0.1:70000", "http://127.0.0.1:8088"} {
		if (&Config{CoreAddress: addr}).Validate() == nil {
			t.Errorf("accepted %q", addr)
		}
	}
}

func TestManagedCoreConfig(t *testing.T) {
	cfg, err := (Config{Core: &CoreConfig{Version: "v1.0.0"}}).Resolve()
	if err != nil || cfg.Core.Repository != "we-clay/core-release" || cfg.Core.StartupTimeout != 2*time.Minute {
		t.Fatalf("defaults: %v", err)
	}
	for _, version := range []string{"", "latest", "../v1", "v1/tag"} {
		if _, err := (Config{Core: &CoreConfig{Version: version}}).Resolve(); err == nil {
			t.Errorf("accepted version %q", version)
		}
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[core]\nversion = \"v1.0.0\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(path); err != nil || cfg.Core == nil {
		t.Fatalf("nested config: %v", err)
	}
	if _, err = (Config{Core: &CoreConfig{Version: "v1", LoginAddress: "0.0.0.0:8086"}}).Resolve(); err == nil {
		t.Fatal("accepted public listener")
	}
}

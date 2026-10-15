package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalOnlyModeDoesNotStartExternalPlugin(t *testing.T) {
	dir := t.TempDir()
	// This file is deliberately not a valid plugin executable. If discovery runs,
	// plugin startup would fail before the built-in dashboard can register.
	path := filepath.Join(dir, "we-clay-plugin-untrusted")
	if err := os.WriteFile(path, []byte("invalid executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := Initial(context.Background(), dir, false, "WE_CLAY_AUTH_KEY"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Destroy)
	if err := RegisterFoundation(nil, true, true); err != nil {
		t.Fatal(err)
	}
	if findPlugin("dashboard") == nil || findPlugin("ai") == nil {
		t.Fatal("internal dashboard or AI was not registered")
	}
	for _, load := range []func() error{LoadPlugins, func() error { return LoadPlugin("untrusted") }} {
		if err := load(); err == nil || !strings.Contains(err.Error(), "未启用") {
			t.Fatalf("external plugin loading was not blocked: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("internal-only mode created external plugin configuration: %v", err)
	}
}

package config

import "testing"

func TestKeepAliveRequiresExplicitAbsoluteAccountDir(t *testing.T) {
	for _, dir := range []string{"", "runtime/core"} {
		if _, err := (CoreConfig{Version: "v0.3.1", KeepAlive: true, DataDir: dir}).Resolve(); err == nil {
			t.Fatal("accepted unstable account path")
		}
	}
	if _, err := (CoreConfig{Version: "v0.3.1", KeepAlive: true, DataDir: t.TempDir()}).Resolve(); err != nil {
		t.Fatal(err)
	}
}

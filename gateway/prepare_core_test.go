package gateway_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/gateway"
)

func TestPrepareCoreVerifiesCacheWithoutStartingCore(t *testing.T) {
	root := t.TempDir()
	cfg, err := (gateway.Config{Core: &gateway.CoreConfig{
		Version: "v1.2.3", CacheDir: filepath.Join(root, "cache"), DataDir: filepath.Join(root, "account"),
	}}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	name := "we-clay-core_" + cfg.Core.Version + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cacheKey := sha256.Sum256([]byte(cfg.Core.Repository + "\x00" + cfg.Core.Version + "\x00" + name))
	cacheDir := filepath.Join(cfg.Core.CacheDir, hex.EncodeToString(cacheKey[:]))
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := []byte("inert cached core fixture")
	if err := os.WriteFile(filepath.Join(cacheDir, name), binary, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	receipt, err := json.Marshal(map[string]any{
		"repository": cfg.Core.Repository,
		"version":    cfg.Core.Version,
		"asset":      name,
		"sha256":     hex.EncodeToString(digest[:]),
		"size":       len(binary),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := gateway.PrepareCore(context.Background(), cfg); err != nil {
		t.Fatalf("verified cached core was rejected: %v", err)
	}
	if _, err := os.Stat(cfg.Core.DataDir); !os.IsNotExist(err) {
		t.Fatalf("preparation accessed account directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("corrupt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := gateway.PrepareCore(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "缓存无效") {
		t.Fatalf("corrupted cache was accepted: %v", err)
	}
}

func TestPrepareCoreRequiresContextAndCoreConfig(t *testing.T) {
	if err := gateway.PrepareCore(nil, gateway.Config{}); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := gateway.PrepareCore(context.Background(), gateway.Config{}); err == nil {
		t.Fatal("missing core config accepted")
	}
}

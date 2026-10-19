package gateway_test

import (
	"context"
	"github.com/we-clay/we-clay/gateway"
	"strings"
	"testing"
	"time"
)

func TestIndependentCoreAuthorizationChangeRequiresExplicitRestart(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.Core.StartupTimeout = 15 * time.Second
	cfg.Core.LoginTimeout = 15 * time.Second
	cfg.Core.AuthKeyEnv = "SYNTHETIC_CORE_AUTH_KEY"
	t.Setenv(cfg.Core.AuthKeyEnv, "synthetic-key-one")
	cleanupIndependent(t, cfg)
	independentRound(t, cfg)
	pid, token := independentRecord(t, cfg)
	t.Setenv(cfg.Core.AuthKeyEnv, "synthetic-key-two")
	if err := gateway.Run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "授权 Key") {
		t.Fatal("new key silently ignored", err)
	}
	pid2, token2 := independentRecord(t, cfg)
	if pid2 != pid || token2 != token {
		t.Fatal("core replaced")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if err := gateway.StopCore(ctx, cfg); err != nil {
		t.Fatal("key change prevented stop", err)
	}
}

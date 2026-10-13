package gateway_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
)

func independentRecord(t *testing.T, cfg gateway.Config) (int, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cfg.Core.DataDir, "gateway-core.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		PID   int
		Token string
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(cfg.Core.DataDir, "gateway-core.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("record must be private")
	}
	if len(r.Token) < 16 || r.PID <= 0 {
		t.Fatal("invalid private record")
	}
	return r.PID, r.Token
}
func independentRound(t *testing.T, cfg gateway.Config) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ready := false
	err := gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(context.Context, *gateway.Client) error { ready = true; cancel(); return nil }}))
	if err != nil || !ready {
		t.Fatalf("independent Run failed: %v, ready=%t", err, ready)
	}
	conn, err := net.DialTimeout("tcp", cfg.CoreAddress, time.Second)
	if err != nil {
		t.Fatal("core did not survive Run", err)
	}
	conn.Close()
}
func cleanupIndependent(t *testing.T, cfg gateway.Config) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := gateway.StopCore(ctx, cfg); err != nil {
			t.Error(err)
			raw, _ := os.ReadFile(filepath.Join(cfg.Core.DataDir, "gateway-core.json"))
			var rec struct{ PID int }
			json.Unmarshal(raw, &rec)
			if rec.PID > 0 {
				p, e := os.FindProcess(rec.PID)
				if e == nil {
					p.Kill()
					p.Release()
				}
			}
		}
	})
}
func TestIndependentCoreReusesAndStopsExplicitly(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.Core.StartupTimeout = 15 * time.Second
	cfg.Core.LoginTimeout = 15 * time.Second
	cfg.CoreToken = ""
	cfg.CoreTokenEnv = "-"
	cleanupIndependent(t, cfg)
	independentRound(t, cfg)
	pid, token := independentRecord(t, cfg)
	independentRound(t, cfg)
	nextPid, nextToken := independentRecord(t, cfg)
	if pid != nextPid || token != nextToken {
		t.Fatal("reconnect replaced core or token")
	}
	// 配置或令牌不匹配不能接管、停止正在运行的 core。
	bad := cfg
	copyCore := *cfg.Core
	bad.Core = &copyCore
	bad.Core.Version = "v0.0.1-test"
	if err := gateway.Run(context.Background(), bad); err == nil {
		t.Fatal("version mismatch accepted")
	}
	bad = cfg
	bad.CoreToken = "synthetic-wrong-token"
	if err := gateway.StopCore(context.Background(), bad); err == nil {
		t.Fatal("wrong token stopped core")
	}
	independentRound(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if err := gateway.StopCore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.DataDir, "gateway-core.json")); !os.IsNotExist(err) {
		t.Fatal("stopped record retained")
	}
	listener, err := net.Listen("tcp", cfg.CoreAddress)
	if err != nil {
		t.Fatal("explicit stop left listener", err)
	}
	listener.Close()
}
func TestIndependentCoreRejectsUnknownListener(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.Core.StartupTimeout = 15 * time.Second
	cfg.Core.LoginTimeout = 15 * time.Second
	listener, err := net.Listen("tcp", cfg.CoreAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	err = gateway.Run(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "其他程序") {
		t.Fatalf("unknown listener: %v", err)
	}
	if _, err = os.Stat(filepath.Join(cfg.Core.DataDir, "gateway-core.json")); !os.IsNotExist(err) {
		t.Fatal("adopted unknown listener")
	}
}
func TestIndependentCoreSurvivesParentProcessExit(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.Core.StartupTimeout = 15 * time.Second
	cfg.Core.LoginTimeout = 15 * time.Second
	cleanupIndependent(t, cfg)
	b, _ := json.Marshal(cfg)
	file := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, self, "--fixture-gateway-parent", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("parent exit: %v: %s", err, output)
	}
	pid, token := independentRecord(t, cfg)
	independentRound(t, cfg)
	pid2, token2 := independentRecord(t, cfg)
	if pid != pid2 || token != token2 {
		t.Fatal("parent exit lost core")
	}
}
func independentParentMain(file string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		os.Exit(20)
	}
	var cfg gateway.Config
	if json.Unmarshal(raw, &cfg) != nil {
		os.Exit(21)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ready := false
	err = gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(context.Context, *gateway.Client) error { ready = true; cancel(); return nil }}))
	if err != nil || !ready {
		os.Exit(22)
	}
}

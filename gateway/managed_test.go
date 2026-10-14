package gateway_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// 用本测试程序充当 core 子进程，真实启动/退出但不连接微信。
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "--fixture-gateway-parent" {
		independentParentMain(os.Args[2])
		os.Exit(0)
	}
	if os.Getenv("WE_CLAY_TEST_MANAGED_CORE") != "" {
		mockCoreMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type managedService struct {
	corewire.UnimplementedCoreServiceServer
	data, mode string
	views      atomic.Int32
}

func (s *managedService) Invoke(ctx context.Context, r *corewire.CallRequest) (*corewire.CallResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+os.Getenv("WE_CLAY_GATEWAY_CORE_TOKEN") {
		return nil, status.Error(codes.Unauthenticated, "fixture token mismatch")
	}
	body := `{"code":0,"data":[]}`
	switch r.GetPath() {
	case "/api/login/view":
		body = fmt.Sprintf(`{"code":0,"data":{"online":%t}}`, s.views.Add(1) > 1 && s.mode != "wait")
	case "/api/user/profile":
		if s.views.Load() < 2 {
			return nil, status.Error(codes.FailedPrecondition, "login not ready")
		}
		body = `{"code":0,"data":{"baseResponse":{"code":0},"userInfo":{"userName":"synthetic-managed-account"}}}`
	}
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}, nil
}
func (s *managedService) Events(stream grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]) error {
	if _, err := stream.Recv(); err != nil {
		return err
	}
	if err := stream.Send(&corewire.CoreEvent{Ready: true}); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(s.data, "fixture-subscribed"), []byte("ok"), 0600)
	if s.mode == "crash" {
		os.Exit(7)
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}
func mockCoreMain() {
	f := flag.NewFlagSet("mock-core", flag.ExitOnError)
	data := f.String("data", "", "")
	addr := f.String("grpc-listen", "", "")
	stage := f.String("stage", "", "")
	device := f.String("device-type", "", "")
	deviceName := f.String("device-name", "", "")
	updateVersion := f.Bool("update-version", true, "")
	f.String("listen", "", "")
	f.String("api-token-env", "", "")
	f.Bool("storage-encrypt", false, "")
	f.Parse(os.Args[1:])
	mode := os.Getenv("WE_CLAY_TEST_MANAGED_CORE")
	if mode == "exit" {
		os.Exit(7)
	}
	if *device != "mac" || *updateVersion || os.Getenv("WE_CLAY_TEST_RELEASE_TOKEN") != "" {
		os.Exit(8)
	}
	os.WriteFile(filepath.Join(*data, "fixture-stage"), []byte(*stage), 0600)
	os.WriteFile(filepath.Join(*data, "fixture-device-name"), []byte(*deviceName), 0600)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		os.Exit(9)
	}
	server := grpc.NewServer()
	corewire.RegisterCoreServiceServer(server, &managedService{data: *data, mode: mode})
	go server.Serve(listener)
	<-ctx.Done()
	server.Stop()
}

func managedFixture(t *testing.T, mode string) gateway.Config {
	t.Helper()
	t.Setenv("WE_CLAY_TEST_MANAGED_CORE", mode)
	t.Setenv("WE_CLAY_TEST_RELEASE_TOKEN", "synthetic-release-credential")
	root := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	config := gateway.Config{
		CoreAddress: address, CoreToken: "synthetic-managed-token", DataDir: filepath.Join(root, "gateway"), PluginsDir: filepath.Join(root, "plugins"),
		Core: &gateway.CoreConfig{Version: "v0.0.0-test", CacheDir: filepath.Join(root, "bin"), DataDir: filepath.Join(root, "core"), LoginAddress: "-", GitHubTokenEnv: "WE_CLAY_TEST_RELEASE_TOKEN", StartupTimeout: 5 * time.Second, LoginTimeout: 5 * time.Second},
	}
	config, err = config.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	name := "we-clay-core-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	key := sha256.Sum256([]byte(config.Core.Repository + "\x00" + config.Core.Version + "\x00" + name))
	dir := filepath.Join(config.Core.CacheDir, hex.EncodeToString(key[:]))
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(out, h), in)
	out.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"repository": config.Core.Repository, "version": config.Core.Version, "asset": name, "sha256": hex.EncodeToString(h.Sum(nil)), "size": size})
	if err = os.WriteFile(filepath.Join(dir, "receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return config
}

func awaitFile(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(name); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("mock core did not reach expected stage")
}

func TestRunManagesCoreAndWaitsForLogin(t *testing.T) {
	cfg := managedFixture(t, "normal")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, cfg) }()
	awaitFile(t, filepath.Join(cfg.Core.DataDir, "fixture-subscribed"))
	raw, _ := os.ReadFile(filepath.Join(cfg.Core.DataDir, "fixture-stage"))
	if string(raw) != "login" {
		t.Fatalf("new data stage: %s", raw)
	}
	cancel()
	stopped(t, done)
	listener, err := net.Listen("tcp", cfg.CoreAddress)
	if err != nil {
		t.Fatal("managed child not reaped", err)
	}
	listener.Close()
}

func TestManagedCoreFailurePropagatesAndCancellationReaps(t *testing.T) {
	for _, mode := range []string{"exit", "crash", "wait"} {
		t.Run(mode, func(t *testing.T) {
			cfg := managedFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- gateway.Run(ctx, cfg) }()
			if mode == "wait" {
				awaitFile(t, filepath.Join(cfg.Core.DataDir, "fixture-stage"))
				cancel()
				stopped(t, done)
			} else {
				select {
				case err := <-done:
					if err == nil || !strings.Contains(err.Error(), "core 已退出") {
						t.Fatalf("lost process exit: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("Run did not return on core exit")
				}
			}
			listener, err := net.Listen("tcp", cfg.CoreAddress)
			if err != nil {
				t.Fatal("child listener survived", err)
			}
			listener.Close()
		})
	}
}

func TestManagedCoreDoesNotAdoptOccupiedPort(t *testing.T) {
	cfg := managedFixture(t, "normal")
	listener, err := net.Listen("tcp", cfg.CoreAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = gateway.Run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("occupied port: %v", err)
	}
	if _, err = os.Stat(cfg.Core.DataDir); !os.IsNotExist(err) {
		t.Fatal("occupied port created core state")
	}
}

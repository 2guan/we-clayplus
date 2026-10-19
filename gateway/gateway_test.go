// 文件名：gateway_test.go
// 主要功能：使用独立临时目录和临时控制台端口验证 gateway 生命周期，避免与开发预览冲突。
// 作者：2guan
package gateway_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/we-clay/we-clay/sdk/control"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeCore struct {
	corewire.UnimplementedCoreServiceServer
	acked    chan struct{}
	rejected atomic.Bool
	account  atomic.Int32
}

func (s *fakeCore) Invoke(ctx context.Context, req *corewire.CallRequest) (*corewire.CallResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer synthetic-token" {
		return nil, status.Error(codes.Unauthenticated, "fixture authentication failed")
	}
	if s.rejected.Load() {
		return nil, status.Error(codes.Unauthenticated, "synthetic login failure")
	}
	body := ""
	switch req.GetPath() {
	case "/api/user/profile":
		body = fmt.Sprintf(`{"code":0,"data":{"baseResponse":{"code":0},"userInfo":{"userName":"synthetic-account-%d","nickName":"离线测试"}}}`, s.account.Load())
	case "/api/contacts":
		body = `{"code":0,"data":[]}`
	default:
		return nil, status.Errorf(codes.Unimplemented, "unexpected path %s", req.GetPath())
	}
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}, nil
}
func (s *fakeCore) Events(stream grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]) error {
	control, err := stream.Recv()
	if err != nil {
		return err
	}
	if control.GetSubscribe().GetVersion() != corewire.Version {
		return status.Error(codes.InvalidArgument, "missing subscribe")
	}
	if err = stream.Send(&corewire.CoreEvent{Ready: true}); err != nil {
		return err
	}
	// 真实 gRPC 事件走完入口、解析、缓存变更及确认，不触碰真实微信。
	if err = stream.Send(&corewire.CoreEvent{EventId: "synthetic-event", SyncJson: []byte(`{"modifyContact":[{"username":"synthetic-contact","nickname":"离线联系人"}]}`)}); err != nil {
		return err
	}
	control, err = stream.Recv()
	if err != nil {
		return err
	}
	if control.GetAck().GetEventId() != "synthetic-event" {
		return status.Error(codes.InvalidArgument, "missing ack")
	}
	s.acked <- struct{}{}
	<-stream.Context().Done()
	return stream.Context().Err()
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for gateway event acknowledgment")
	}
}
func stopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not stop")
	}
}
func fixture(t *testing.T) (*fakeCore, gateway.Config) {
	t.Helper()
	server := &fakeCore{acked: make(chan struct{}, 4)}
	rpc := coretest.NewService(t, server)
	root := t.TempDir()
	return server, gateway.Config{Console: &control.Config{Address: "127.0.0.1:0"}, CoreAddress: rpc.Address, CoreToken: "synthetic-token", DataDir: filepath.Join(root, "data"), PluginsDir: filepath.Join(root, "plugins")}
}
func TestRunLifecycleAndAccountIsolation(t *testing.T) {
	server, cfg := fixture(t)
	cwd, _ := os.Getwd()
	for i := int32(1); i <= 2; i++ {
		server.account.Store(i)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		done := make(chan error, 1)
		go func() { done <- gateway.Run(ctx, cfg) }()
		await(t, server.acked)
		if err := gateway.Run(context.Background(), cfg); !errors.Is(err, gateway.ErrAlreadyRunning) {
			t.Errorf("concurrent run: %v", err)
		}
		cancel()
		stopped(t, done)
	}
	dirs, err := os.ReadDir(cfg.DataDir)
	if err != nil || len(dirs) != 2 {
		t.Fatalf("account caches not isolated: %v, %d", err, len(dirs))
	}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(cfg.DataDir, dir.Name(), "contact.json"))
		if err != nil || len(raw) <= 2 {
			t.Fatalf("event cache missing: %v", err)
		}
	}
	after, _ := os.Getwd()
	if after != cwd {
		t.Fatal("Run changed caller working directory")
	}
	if _, err = os.Stat(filepath.Join(cfg.PluginsDir, "config.toml")); err != nil {
		t.Fatal(err)
	}
}
func TestFailedStartCanBeRetried(t *testing.T) {
	server, cfg := fixture(t)
	server.rejected.Store(true)
	if err := gateway.Run(context.Background(), cfg); err == nil {
		t.Fatal("login rejection swallowed")
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Fatal("failed login created account data")
	}
	server.rejected.Store(false)
	if err := os.MkdirAll(cfg.PluginsDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.PluginsDir, "config.toml")
	if err := os.WriteFile(path, []byte("invalid = ["), 0600); err != nil {
		t.Fatal(err)
	}
	if err := gateway.Run(context.Background(), cfg); err == nil {
		t.Fatal("invalid plugin config swallowed")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, cfg) }()
	await(t, server.acked)
	cancel()
	stopped(t, done)
}
func TestCancelledAndInvalidRunDoNotCreateDirectories(t *testing.T) {
	root := t.TempDir()
	cfg := gateway.Config{Console: &control.Config{Address: "127.0.0.1:0"}, DataDir: filepath.Join(root, "data"), PluginsDir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gateway.Run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.CoreAddress = "https://invalid.example"
	if err := gateway.Run(context.Background(), cfg); err == nil {
		t.Fatal("invalid address accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid/cancelled run created files")
	}
	if err := gateway.Run(nil, cfg); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestRunLoadsPluginAndStopsItsProcess(t *testing.T) {
	server, cfg := fixture(t)
	if err := os.MkdirAll(cfg.PluginsDir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "we-clay-plugin-example"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(cfg.PluginsDir, name)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "../examples/plugin")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(cfg.PluginsDir, "config.toml"), []byte("[example]\nenable = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		done := make(chan error, 1)
		go func() { done <- gateway.Run(ctx, cfg) }()
		await(t, server.acked)
		cancel()
		stopped(t, done)
	}
	// 第二次成功加载同名插件还检查了 SDK 客户端注册表清理。
}

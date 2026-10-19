package gateway_test

import (
	"context"
	"errors"
	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type expiringCore struct {
	fakeCore
	offline atomic.Bool
}

func (s *expiringCore) Invoke(ctx context.Context, q *corewire.CallRequest) (*corewire.CallResponse, error) {
	if q.GetPath() == "/api/login/view" {
		body := `{"code":0,"data":{"online":true,"phase":"online"}}`
		if s.offline.Load() {
			body = `{"code":0,"data":{"online":false,"phase":"login_required","can_retry":true}}`
		}
		return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}, nil
	}
	return s.fakeCore.Invoke(ctx, q)
}
func TestRunNotifiesSessionLossAndInvalidatesClient(t *testing.T) {
	server := &expiringCore{fakeCore: fakeCore{acked: make(chan struct{}, 1)}}
	rpc := coretest.NewService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan *gateway.Client, 1)
	state := make(chan string, 4)
	done := make(chan error, 1)
	cfg := gateway.Config{CoreAddress: rpc.Address, CoreToken: "synthetic-token", DataDir: filepath.Join(t.TempDir(), "data")}
	go func() {
		done <- gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, c *gateway.Client) error { ready <- c; return nil }, OnConnectionChanged: func(_ context.Context, s gateway.ConnectionState) error { state <- s.Phase; return nil }}))
	}()
	var client *gateway.Client
	select {
	case client = <-ready:
	case <-ctx.Done():
		t.Fatal("not ready")
	}
	server.offline.Store(true)
	select {
	case err := <-done:
		if !errors.Is(err, gateway.ErrLoginRequired) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Run kept an expired session online")
	}
	select {
	case s := <-state:
		if s != "login_required" {
			t.Fatal(s)
		}
	default:
		t.Fatal("missing application notification")
	}
	if _, err := client.SendText("synthetic-contact", "not sent"); err == nil {
		t.Fatal("old client remained usable")
	}
}

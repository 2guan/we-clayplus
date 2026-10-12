package coreprocess

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type loginService struct {
	corewire.UnimplementedCoreServiceServer
	calls atomic.Int32
	mode  string
}

func (s *loginService) Invoke(ctx context.Context, r *corewire.CallRequest) (*corewire.CallResponse, error) {
	if r.GetPath() != "/api/login/view" || r.GetMethod() != "GET" {
		return nil, status.Error(codes.Unimplemented, "unexpected request")
	}
	if s.mode == "rejected" {
		return nil, status.Error(codes.Unauthenticated, "fixture")
	}
	if s.mode == "blocked" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	online := s.calls.Add(1) > 1 && s.mode == ""
	text := "等待扫码"
	if s.mode == "paused" {
		text = "登录流程已暂停：fixture"
	}
	return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: []byte(fmt.Sprintf(`{"code":0,"data":{"online":%t,"status":%q}}`, online, text))}, nil
}
func TestWaitForLoginOverGRPC(t *testing.T) {
	for _, mode := range []string{"", "rejected", "paused", "blocked", "pending"} {
		t.Run(mode, func(t *testing.T) {
			s := &loginService{mode: mode}
			server := coretest.NewService(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			client, err := api.NewGRPCClient(ctx, server.Address, "")
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			c := config.CoreConfig{StartupTimeout: 100 * time.Millisecond, LoginTimeout: 700 * time.Millisecond, LoginAddress: "-"}
			err = WaitOnline(ctx, client, c)
			if (err == nil) != (mode == "") {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if mode == "" && s.calls.Load() < 2 {
				t.Fatal("did not wait for scan")
			}
		})
	}
}

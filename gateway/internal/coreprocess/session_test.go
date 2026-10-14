package coreprocess

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type sessionCore struct {
	corewire.UnimplementedCoreServiceServer
	mu             sync.Mutex
	offline        bool
	reads, retries int
	recover        bool
}

func (s *sessionCore) Invoke(ctx context.Context, q *corewire.CallRequest) (*corewire.CallResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var data any
	if q.GetMethod() == "POST" && q.GetPath() == "/api/login/retry" {
		s.retries++
		s.offline = false
		data = "accepted"
	} else if q.GetMethod() == "GET" && q.GetPath() == "/api/login/view" {
		s.reads++
		if !s.recover && s.reads > 1 {
			s.offline = true
		}
		phase := "online"
		if s.offline {
			phase = "login_required"
		}
		data = ConnectionState{Online: !s.offline, Phase: phase, CanRetry: s.offline, Status: phase}
	} else {
		return nil, status.Error(codes.Unimplemented, "unexpected test path")
	}
	raw, _ := json.Marshal(map[string]any{"code": 0, "data": data})
	return &corewire.CallResponse{Status: 200, Body: raw, ContentType: "application/json"}, nil
}
func TestWatchReportsSessionLossWithoutRelogin(t *testing.T) {
	server := &sessionCore{}
	fixture := coretest.NewService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := api.NewGRPCClient(ctx, fixture.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var got []string
	err = watchOnline(ctx, client, time.Millisecond, func(s ConnectionState) error { got = append(got, s.Phase); return nil })
	if !errors.Is(err, ErrLoginRequired) || len(got) != 2 || got[1] != "login_required" {
		t.Fatal(err, got)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.retries != 0 {
		t.Fatal("monitor triggered login without application request")
	}
}
func TestExplicitRunRequestsCoreLoginOnce(t *testing.T) {
	server := &sessionCore{offline: true, recover: true}
	fixture := coretest.NewService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := api.NewGRPCClient(ctx, fixture.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var got []string
	err = WaitOnlineNotify(ctx, client, config.CoreConfig{StartupTimeout: time.Second, LoginTimeout: time.Second, LoginAddress: "-"}, func(s ConnectionState) error { got = append(got, s.Phase); return nil })
	if err != nil || len(got) != 2 || got[0] != "login_required" || got[1] != "online" {
		t.Fatal(err, got)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.retries != 1 {
		t.Fatal("not exactly one explicit core retry", server.retries)
	}
}

// 一次 Run 已开始等待扫码，扫码超时不能被 gateway 变成第二次自动登录。
type timedOutLogin struct{ sessionCore }

func (s *timedOutLogin) Invoke(ctx context.Context, q *corewire.CallRequest) (*corewire.CallResponse, error) {
	if q.GetPath() == "/api/login/view" {
		s.mu.Lock()
		s.reads++
		n := s.reads
		s.mu.Unlock()
		data := ConnectionState{Phase: "waiting_scan", Status: "等待扫码"}
		if n > 1 {
			data = ConnectionState{Phase: "login_failed", Status: "登录流程已暂停：synthetic timeout", CanRetry: true}
		}
		raw, _ := json.Marshal(map[string]any{"code": 0, "data": data})
		return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: raw}, nil
	}
	return s.sessionCore.Invoke(ctx, q)
}
func TestScanTimeoutDoesNotTriggerAutomaticRetry(t *testing.T) {
	server := &timedOutLogin{}
	fixture := coretest.NewService(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := api.NewGRPCClient(ctx, fixture.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	err = WaitOnline(ctx, client, config.CoreConfig{StartupTimeout: time.Second, LoginTimeout: time.Second, LoginAddress: "-"})
	if err == nil {
		t.Fatal("timeout was ignored")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.retries != 0 {
		t.Fatal("gateway retried scan without another application request", server.retries)
	}
}

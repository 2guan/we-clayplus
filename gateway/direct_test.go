package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/corewire"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type directCore struct {
	fakeCore
	mu          sync.Mutex
	writes      []*corewire.CallRequest
	acks        chan string
	connections atomic.Int32
	replay      bool
}

func (s *directCore) Invoke(ctx context.Context, req *corewire.CallRequest) (*corewire.CallResponse, error) {
	if (req.GetPath() == "/api/message/text" || req.GetPath() == "/api/message/card" || req.GetPath() == "/api/message/revoke") || strings.HasPrefix(req.GetPath(), "/api/moments/like/") {
		s.mu.Lock()
		s.writes = append(s.writes, req)
		s.mu.Unlock()
		body := `{"code":0,"data":{"baseResponse":{"code":0}}}`
		if req.GetPath() == "/api/message/text" || req.GetPath() == "/api/message/card" {
			body = `{"code":0,"data":{"list":[{"newId":"9223372036854770011","clientId":"9007199254740993","createTime":1789890000}]}}`
		}
		if req.GetPath() == "/api/message/revoke" {
			var q struct {
				NewID    uint64 `json:"new_id"`
				ClientID uint64 `json:"client_id"`
				Created  uint64 `json:"create_time"`
			}
			if json.Unmarshal(req.GetBody(), &q) != nil || q.NewID != 9223372036854770011 || q.ClientID != 9007199254740993 || q.Created != 1789890000 {
				return &corewire.CallResponse{Status: 400, Body: []byte(`{"code":-1,"message":"incomplete receipt"}`)}, nil
			}
		}
		return &corewire.CallResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}, nil
	}
	return s.fakeCore.Invoke(ctx, req)
}

func (s *directCore) Events(stream grpc.BidiStreamingServer[corewire.EventControl, corewire.CoreEvent]) error {
	s.connections.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}
	if err := stream.Send(&corewire.CoreEvent{Ready: true}); err != nil {
		return err
	}
	fixtures := []string{
		`{"modifyContact":[{"username":"synthetic-sender","nickname":"测试发送者"},{"username":"synthetic-account-0","nickname":"测试账号"}]}`,
		`{"newMessage":[{"newId":"9223372036854770001","type":1,"sender":"synthetic-sender","receiver":"synthetic-account-0","content":"/ping","createTime":1,"source":"<msgsource/>"}]}`,
		`{"modifyContact":[{"username":"synthetic-sender","remark":"测试备注"}]}`,
	}
	for i, body := range fixtures {
		id := fmt.Sprintf("direct-%d", i)
		if err := stream.Send(&corewire.CoreEvent{EventId: id, SyncJson: []byte(body)}); err != nil {
			return err
		}
		ack, err := stream.Recv()
		if err != nil {
			return err
		}
		if ack.GetAck().GetEventId() != id {
			return status.Error(codes.InvalidArgument, "wrong acknowledgment")
		}
		s.acks <- id
	}
	if s.replay && s.connections.Load() == 1 {
		return status.Error(codes.Unavailable, "fixture disconnect")
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func directFixture(t *testing.T) (*directCore, gateway.Config) {
	t.Helper()
	s := &directCore{acks: make(chan string, 32)}
	rpc := coretest.NewService(t, s)
	root := t.TempDir()
	return s, gateway.Config{CoreAddress: rpc.Address, CoreToken: "synthetic-token", DataDir: root + "/data", PluginsDir: root + "/no-plugins"}
}

func TestDirectClientSendLikeCallbacksReplayAndExpiry(t *testing.T) {
	s, cfg := directFixture(t)
	s.replay = true
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var ready, messages, events atomic.Int32
	clientCh := make(chan *gateway.Client, 1)
	done := make(chan error, 1)
	exited := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
	})
	go func() {
		defer close(exited)
		done <- gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{
			OnReady: func(ctx context.Context, c *gateway.Client) error {
				ready.Add(1)
				if c.Contact.GetSelf().Username != "synthetic-account-0" {
					return errors.New("profile not ready")
				}
				clientCh <- c
				_, err := c.Moments.Like(9223372036854770012)
				return err
			},
			OnMessage: func(ctx context.Context, c *gateway.Client, m *message.Message) error {
				messages.Add(1)
				if m.GetContent() != "/ping" || m.GetId() != 9223372036854770001 {
					return errors.New("message mapping changed")
				}
				if _, err := c.SendText(m.GetSender().GetUsername(), "pong"); err != nil {
					return err
				}
				// 回调收到独立副本，不能污染后续事件或缓存。
				m.Content = "changed"
				m.Sender.Nickname = "changed"
				return nil
			},
			OnEvent: func(ctx context.Context, c *gateway.Client, e *plugin.Event) error {
				events.Add(1)
				if m := e.GetMessage(); m != nil && (m.Content != "/ping" || m.Sender.Nickname != "测试发送者") {
					return errors.New("message callback contaminated event")
				}
				return nil
			},
		}))
	}()
	var c *gateway.Client
	select {
	case c = <-clientCh:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// 主动调用发生在 OnReady 外；取消点赞和发送仍使用同一个客户端。
	if _, err := c.Moments.Unlike(9223372036854770012); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SendText("synthetic-receiver", "主动发送"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		select {
		case <-s.acks:
		case err := <-done:
			t.Fatalf("Run exited: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cancel()
	stopped(t, done)
	if ready.Load() != 1 || messages.Load() != 1 || events.Load() < 2 {
		t.Fatalf("callback counts: %d %d %d", ready.Load(), messages.Load(), events.Load())
	}
	if _, err := os.Stat(cfg.PluginsDir); !os.IsNotExist(err) {
		t.Fatal("WithoutPlugins created plugin directory", err)
	}
	if _, err := c.SendText("synthetic-receiver", "late"); !errors.Is(err, gateway.ErrClientClosed) {
		t.Fatal("stale send", err)
	}
	if _, err := c.Moments.Like(1); !errors.Is(err, gateway.ErrClientClosed) {
		t.Fatal("stale like", err)
	}
	if c.Contact.GetSelf() != nil {
		t.Fatal("closed cache query not empty")
	}
	s.mu.Lock()
	writes := append([]*corewire.CallRequest(nil), s.writes...)
	s.mu.Unlock()
	if len(writes) != 4 {
		t.Fatalf("writes repeated: %d", len(writes))
	}
	var post, del, pong, active bool
	for _, w := range writes {
		if w.Path == "/api/moments/like/9223372036854770012" {
			post = post || w.Method == "POST"
			del = del || w.Method == "DELETE"
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body, &body); err != nil {
			t.Fatal(err)
		}
		pong = pong || (body["receiver"] == "synthetic-sender" && body["content"] == "pong")
		active = active || (body["receiver"] == "synthetic-receiver" && body["content"] == "主动发送")
	}
	if !post || !del || !pong || !active {
		t.Fatal("outbound requests differ from expected core contract")
	}
	// 新 Run 登录另一账号，旧客户端仍不允许进入新的全局能力实例。
	s.account.Store(2)
	second, stop := context.WithCancel(context.Background())
	defer stop()
	err := gateway.Run(second, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, next *gateway.Client) error {
		if next.Contact.GetSelf().Username != "synthetic-account-2" {
			return errors.New("wrong second account")
		}
		if _, err := c.Moments.Like(2); !errors.Is(err, gateway.ErrClientClosed) {
			return errors.New("old client entered new run")
		}
		stop()
		return nil
	}}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestDirectCallbackFailureStopsWithoutAckOrRetry(t *testing.T) {
	for _, which := range []string{"ready", "message", "event", "panic"} {
		t.Run(which, func(t *testing.T) {
			s, cfg := directFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			failure := errors.New("synthetic handler failure")
			h := gateway.Handlers{}
			switch which {
			case "ready":
				h.OnReady = func(context.Context, *gateway.Client) error { return failure }
			case "message":
				h.OnMessage = func(context.Context, *gateway.Client, *message.Message) error { return failure }
			case "event":
				h.OnEvent = func(context.Context, *gateway.Client, *plugin.Event) error { return failure }
			case "panic":
				h.OnMessage = func(context.Context, *gateway.Client, *message.Message) error { panic("do not print private payload") }
			}
			err := gateway.Run(ctx, cfg, gateway.WithoutPlugins(), gateway.WithHandlers(h))
			if which == "panic" {
				if err == nil || !strings.Contains(err.Error(), "OnMessage") || strings.Contains(err.Error(), "private payload") {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal("callback error lost", err)
			}
			if s.connections.Load() != 1 {
				t.Fatal("callback retried")
			}
			for len(s.acks) > 0 {
				if ack := <-s.acks; ack != "direct-0" {
					t.Fatal("failed message acknowledged", ack)
				}
			}
			if _, err = os.Stat(cfg.PluginsDir); !os.IsNotExist(err) {
				t.Fatal("plugin side effect", err)
			}
		})
	}
}

func TestDirectCardRequestAndValidation(t *testing.T) {
	server, cfg := directFixture(t)
	done := errors.New("fixture complete")
	err := gateway.Run(context.Background(), cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, c *gateway.Client) error {
		if _, err := c.SendCard("", "test-card", "", ""); err == nil {
			return errors.New("empty receiver accepted")
		}
		got, err := c.SendCard("test-receiver", "test-card", "测试名片", "test-alias")
		if err != nil {
			return err
		}
		if got.GetNewId() != 9223372036854770011 {
			return errors.New("card receipt lost")
		}
		return done
	}}))
	if !errors.Is(err, done) {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.writes) != 1 {
		t.Fatal("card duplicated or invalid request sent")
	}
	var body map[string]string
	if err := json.Unmarshal(server.writes[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if server.writes[0].Path != "/api/message/card" || body["receiver"] != "test-receiver" || body["card_username"] != "test-card" || body["card_nickname"] != "测试名片" || body["card_alias"] != "test-alias" {
		t.Fatal("card mapping incorrect")
	}
}

func TestRunRevokeReceiptsAreClearedAcrossAccounts(t *testing.T) {
	server, cfg := directFixture(t)
	finished := errors.New("fixture complete")
	err := gateway.Run(context.Background(), cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, c *gateway.Client) error {
		sent, err := c.SendText("test-peer", "test revoke")
		if err != nil {
			return err
		}
		if _, err = c.Message.Revoke("test-peer", sent.GetNewId()); err != nil {
			return err
		}
		card, err := c.SendCard("test-card-receiver", "test-card", "", " ")
		if err != nil {
			return err
		}
		if _, err = c.Message.Revoke("test-card-receiver", card.GetNewId()); err != nil {
			return err
		}
		return finished
	}}))
	if !errors.Is(err, finished) {
		t.Fatal(err)
	}
	server.account.Store(3)
	err = gateway.Run(context.Background(), cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, c *gateway.Client) error {
		if _, err := c.Message.Revoke("test-peer", 9223372036854770011); !errors.Is(err, message.ErrRevokeReceiptUnavailable) {
			return fmt.Errorf("receipt crossed Run: %v", err)
		}
		return finished
	}}))
	if !errors.Is(err, finished) {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.writes) != 4 {
		t.Fatalf("extra business requests: %d", len(server.writes))
	}
}

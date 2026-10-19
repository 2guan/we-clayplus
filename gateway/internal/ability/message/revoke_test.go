// 文件名：revoke_test.go
// 主要功能：验证发送、回执恢复及撤回参数完整传递到 core 契约。
// 作者：2guan
package messageability

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	coreapi "github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// 对照 core 的撤回契约：必须带回原始的两个 ID 和时间，不能以零值或当前时间补齐。
func TestSendThenRevokePreservesOriginalReceipt(t *testing.T) {
	const newID = uint64(9223372036854770001)
	const clientID = uint64(9007199254740993)
	const created = uint64(1789890000)
	revoked := make(chan struct{}, 4)
	var revokes atomic.Int32
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/message/text":
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"list":[{"code":0,"new_id":"9223372036854770001","client_id":"9007199254740993","create_time":1789890000}]}}`)
		case "/api/message/revoke":
			revokes.Add(1)
			var q struct {
				Receiver   string
				NewID      uint64 `json:"new_id"`
				ClientID   uint64 `json:"client_id"`
				CreateTime uint64 `json:"create_time"`
			}
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Error(err)
			}
			if q.Receiver != "test-peer" || q.NewID != newID || q.ClientID != clientID || q.CreateTime != created {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"code":-1,"message":"revocation requires original new_id, client_id and create_time"}`)
				return
			}
			revoked <- struct{}{}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	conn, err := coreapi.NewGRPCClient(context.Background(), core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	old := coreapi.GetCore
	coreapi.GetCore = func() *coreapi.Client { return conn }
	defer func() { coreapi.GetCore = old }()
	for _, mode := range []string{"direct", "plugin", "auto"} {
		t.Run(mode, func(t *testing.T) {
			impl := &ability{api: messageapi.Get()}
			var target sdk.Ability = impl
			if mode == "plugin" {
				rpc := grpc.NewServer()
				listener := newMessageListener(t)
				sdk.RegisterMessageServiceServer(rpc, &sdk.Server{Impl: impl})
				go rpc.Serve(listener)
				defer rpc.Stop()
				c, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				target = sdk.Client{Client: sdk.NewMessageServiceClient(c)}
			}
			var delay uint32
			if mode == "auto" {
				delay = 1
			}
			sent, err := target.Send(&sdk.Message{Timestamp: delay, Type: sdk.TypeText, Receiver: &contact.Contact{Username: "test-peer"}, Content: "synthetic revoke"})
			if err != nil {
				t.Fatal(err)
			}
			if sent.GetNewId() != newID || uint64(sent.GetCreateTime()) != created {
				t.Fatal("send receipt lost precision")
			}
			if mode != "auto" {
				if _, err = target.Revoke("test-peer", sent.GetNewId()); err != nil {
					t.Fatalf("send succeeded, revoke failed: %v", err)
				}
			}
			select {
			case <-revoked:
			case <-time.After(3 * time.Second):
				t.Fatal("revoke did not reach core")
			}
			before := revokes.Load()
			if _, err = target.Revoke("wrong-peer", sent.GetNewId()); err == nil || revokes.Load() != before {
				t.Fatal("receipt used for wrong conversation")
			}
			impl.receipts.clear()
			if _, err = impl.Revoke("test-peer", sent.GetNewId()); !errors.Is(err, sdk.ErrRevokeReceiptUnavailable) || revokes.Load() != before {
				t.Fatal("missing receipt reached core")
			}

			// 模拟宿主从持久化归档恢复，仍需通过原始字段校验，不能只验证成功响应。
			impl.receipts.remember("test-peer", newID, clientID, created)
			if _, err = impl.Revoke("test-peer", newID); err != nil {
				t.Fatal(err)
			}
			<-revoked
		})
	}
	if revokes.Load() != 6 {
		t.Fatal("revoke repeated or missing")
	}
}

func newMessageListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

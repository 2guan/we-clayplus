// 文件名：business_test.go
// 主要功能：验证好友验证桥接的原始票据、场景、错误传播及离线边界，全部使用本机协议夹具。
// 作者：2guan
package console

import (
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/control"
	"io"
	"net/http"
	"testing"
)

func TestFriendVerifyBridgeContract(t *testing.T) {
	calls := 0
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/contacts/friend-requests/verify" {
			t.Error("错误请求路径")
			w.WriteHeader(404)
			return
		}
		var q struct {
			V1, V2 string
			Scene  int
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil || q.V1 != "fixture-v1" || q.V2 != "fixture-ticket" || q.Scene != 3 {
			t.Error("丢失原始验证参数")
			w.WriteHeader(400)
			return
		}
		calls++
		if calls == 2 {
			io.WriteString(w, `{"code":400,"message":"synthetic failure"}`)
			return
		}
		io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"username":"synthetic-peer"}}`)
	}))
	client, e := api.NewGRPCClient(context.Background(), core.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	b := &Bridge{}
	cmd := control.Command{Operation: "friend-verify", Params: json.RawMessage(`{"v1":"fixture-v1","v2":"fixture-ticket","scene":3}`)}
	if _, e = b.Execute(context.Background(), cmd); e == nil || calls != 0 {
		t.Fatal("离线仍执行")
	}
	b.Ready.Store(true)
	if _, e = b.Execute(context.Background(), cmd); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	if _, e = b.Execute(context.Background(), cmd); e == nil || calls != 2 {
		t.Fatal("失败未传递或错误重试", e, calls)
	}
	cmd.Params = json.RawMessage(`{"v1":"","v2":"fixture-ticket","scene":3}`)
	if _, e = b.Execute(context.Background(), cmd); e == nil || calls != 2 {
		t.Fatal("缺失票据仍请求")
	}
}

// 文件名：mentions_test.go
// 主要功能：验证普通文字和引用的成员 ID 到达 core，禁止私聊及全体提醒注入。
// 作者：2guan
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"testing"
)

func TestMentionParametersReachCore(t *testing.T) {
	calls := 0
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/message/receipt" {
			w.WriteHeader(404)
			return
		}
		var q map[string]any
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if q["receiver"] != "synthetic@chatroom" {
			t.Error("群目标丢失")
		}
		calls++
		switch r.URL.Path {
		case "/api/message/text":
			if q["remind"] != "member-1,member-2" {
				t.Error("未携带真正的 @ 成员 ID")
			}
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0},"list":[{"code":0,"new_id":"100","client_id":"42","create_time":1234}]}}`)
		case "/api/message/app":
			if q["extend_xml"] != "<msgsource><atuserlist>member-1,member-2</atuserlist><bizflag>0</bizflag></msgsource>" {
				t.Error("引用丢失 atuserlist")
			}
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0},"new_id":"101","client_id":"synthetic_1234","create_time":1234}}`)
		default:
			t.Error("未知发送接口")
			w.WriteHeader(400)
		}
	}))
	client, err := api.NewGRPCClient(context.Background(), core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	b := &Bridge{}
	b.Ready.Store(true)
	for _, quote := range []string{"", `<appmsg><type>57</type><title>回复</title><refermsg><svrid>1</svrid></refermsg></appmsg>`} {
		p, _ := json.Marshal(map[string]any{"kind": "text", "mentions": []string{"member-1", "member-2"}, "quote_xml": quote})
		if _, err := b.Execute(context.Background(), control.Command{Operation: "send", Target: "synthetic@chatroom", Content: "@张三 @李四 消息", Params: p}); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{"synthetic-peer", "synthetic@chatroom"} {
		p := json.RawMessage(`{"mentions":["notify@all"]}`)
		if _, err := b.Execute(context.Background(), control.Command{Operation: "send", Target: target, Content: "文字", Params: p}); err == nil {
			t.Fatal("非法提醒未拒绝")
		}
	}
	if calls != 2 {
		t.Fatal("非法参数到达 core")
	}
}

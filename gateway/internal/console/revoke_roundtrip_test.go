package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
)

// 对照 core v0.4.2 api_business.go：撤回必须匹配本次发送的原始字段。
// 经过控制台归档序列化并清空内存缓存，覆盖实际点击撤回的参数路径。
func TestSendArchiveThenRevokePreservesReceipt(t *testing.T) {
	oldContact := contact.Instance
	contact.Instance = messageContactFixture{}
	t.Cleanup(func() { contact.Instance = oldContact })
	wasReady := messageability.OutboundReady()
	messageability.SetOutboundReady(true)
	t.Cleanup(func() { messageability.SetOutboundReady(wasReady); messageability.ResetReceipts() })
	for _, tc := range []struct {
		name, route, params string
		numeric             bool
	}{
		{"text", "text", `{}`, true},
		{"image", "image", `{"kind":"image","media":"aW1hZ2U="}`, false},
		{"file", "file", `{"kind":"file","media":"ZmlsZQ=="}`, false},
		{"quote", "app", `{"quote_xml":"<appmsg><type>57</type><title>回复</title><refermsg><svrid>123</svrid></refermsg></appmsg>"}`, false},
		{"emoji", "emoji", `{"kind":"emoji","emoji_md5":"0123456789abcdef0123456789abcdef"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messageability.ResetReceipts()
			created := strconv.FormatInt(time.Now().Unix()-1, 10)
			const newID = "9007199254740993"
			clientID := "fixture-peer_" + created
			if tc.numeric {
				clientID = created
			}
			sends, revokes := 0, 0
			reject := false
			core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/message/"+tc.route {
					sends++
					item := map[string]any{"code": 0, "new_id": newID, "client_id": clientID, "create_time": created}
					data := map[string]any{"base_response": map[string]any{"code": 0}, "new_id": newID, "client_id": clientID, "create_time": created}
					if tc.name == "text" {
						data["list"] = []any{item}
					}
					if tc.name == "emoji" {
						data["result"] = []any{item}
						data["revoke_client_id"], data["revoke_create_time"] = clientID, created
					}
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
					return
				}
				if r.URL.Path != "/api/message/revoke" {
					t.Errorf("意外请求 %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				revokes++
				var q map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				field := func(name string) string {
					var s string
					if json.Unmarshal(q[name], &s) == nil {
						return s
					}
					return string(q[name])
				}
				valid := field("receiver") == "fixture-peer" && field("new_id") == newID && field("create_time") == created && field("original_client_id") == ""
				if tc.numeric {
					valid = valid && field("client_id") == clientID && field("string_client_id") == ""
				} else {
					valid = valid && field("string_client_id") == clientID && field("client_id") == ""
				}
				if !valid || reject {
					if !valid {
						t.Error("撤回未保留发送回执原始字段")
					}
					w.WriteHeader(400)
					fmt.Fprint(w, `{"code":-1,"message":"fixture rejects revoke"}`)
					return
				}
				fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
			}))
			ctx := context.Background()
			client, err := api.NewGRPCClient(ctx, core.Address, "")
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			old := api.GetCore
			api.GetCore = func() *api.Client { return client }
			defer func() { api.GetCore = old }()
			b := &Bridge{}
			b.Ready.Store(true)
			result, err := b.Execute(ctx, control.Command{Operation: "send", Target: "fixture-peer", Content: "fixture", Params: json.RawMessage(tc.params)})
			if err != nil {
				t.Fatal(err)
			}
			out := result.(map[string]string)
			persisted, err := json.Marshal(control.RevokeReceipt{Account: out["account"], ClientID: out["client_id"], OriginalClientID: out["original_client_id"], Created: out["create_time"]})
			if err != nil {
				t.Fatal(err)
			}
			messageability.ResetReceipts()
			command := control.Command{Operation: "revoke", Target: "fixture-peer", ID: out["id"], Params: persisted}
			if _, err = b.Execute(ctx, command); err != nil {
				t.Fatal(err)
			}
			reject = true
			if _, err = b.Execute(ctx, command); err == nil {
				t.Fatal("core 拒绝被吞掉")
			}
			if sends != 1 || revokes != 2 {
				t.Fatalf("重复或缺少请求：send=%d revoke=%d", sends, revokes)
			}
		})
	}
}

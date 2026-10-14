// 文件名：original_revoke_test.go
// 主要功能：校验归档字符串回执进入正确的 core 撤回字段，拒绝混用 ID 类型。
// 作者：2guan
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRevokeOriginalReceipt(t *testing.T) {
	ctx := context.Background()
	created := strconv.FormatInt(time.Now().Unix(), 10)
	calls := 0
	want := ""
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/message/revoke" {
			t.Errorf("意外请求 %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		var q map[string]string
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if q["original_client_id"] != want || q["client_id"] != "" || q["create_time"] != created || q["new_id"] != "9007199254740993" || q["receiver"] != "synthetic-peer" {
			t.Error("原始回执被修改")
			w.WriteHeader(400)
			return
		}
		calls++
		fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
	}))
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
	for _, original := range []string{"synthetic-peer_1234", "1234"} {
		want = original
		receipt := control.RevokeReceipt{Account: contact.Instance.GetSelf().GetUsername(), OriginalClientID: original, Created: created}
		raw, _ := json.Marshal(receipt)
		if _, err = b.Execute(ctx, control.Command{Operation: "revoke", Target: "synthetic-peer", ID: "9007199254740993", Params: raw}); err != nil {
			t.Fatal(err)
		}
		// 即使字符串内容全是数字，也不能同时填入数值型字段。
		receipt.ClientID = "1234"
		raw, _ = json.Marshal(receipt)
		if _, err = b.Execute(ctx, control.Command{Operation: "revoke", Target: "synthetic-peer", ID: "9007199254740993", Params: raw}); err == nil {
			t.Fatal("允许混用两种 ID")
		}
	}
	if calls != 2 {
		t.Fatal("非法请求到达 core")
	}
}

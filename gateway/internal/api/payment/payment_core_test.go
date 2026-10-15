// 文件名：payment_core_test.go
// 主要功能：对照 core v0.3.2 契约验证领取参数、嵌套回执与失败不重试。
// 作者：2guan
package paymentapi

import (
	"context"
	"encoding/json"
	api "github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"io"
	"net/http"
	"testing"
)

func TestReceivePaymentWire(t *testing.T) {
	const native = "wxpay://c2cbizmessagehandler/hongbao/receivehongbao?msgtype=1&channelid=2&sendid=synthetic"
	var path string
	var body map[string]any
	calls := 0
	reject := false
	server := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		path = r.URL.Path
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid JSON")
		}
		if reject {
			w.WriteHeader(502)
			io.WriteString(w, `{"code":1}`)
			return
		}
		if path == "/api/payment/collect" {
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"text":{"value":"{\"retcode\":0}"}}}`)
		} else {
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"text":{"size":13,"data":"eyJyZXRjb2RlIjowfQ=="}}}`)
		}
	}))
	defer server.Close()
	client, e := api.NewGRPCClient(context.Background(), server.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	remote := remote{}
	for _, tc := range []struct {
		path string
		run  func() error
	}{
		{"receive", func() error {
			_, e := remote.ReceiveHongBao(ReceiveHongBaoParams{NativeURL: native, InWay: 1})
			return e
		}},
		{"open", func() error {
			_, e := remote.OpenHongBao(OpenHongBaoParams{NativeURL: native, TimingIdentifier: "timing", SendUserName: "sender"})
			return e
		}},
		{"grab", func() error {
			r, e := remote.GrabHongBao(GrabHongBaoParams{NativeURL: native, InWay: 1})
			if e == nil && string(r.GetText().GetData()) != `{"retcode":0}` {
				t.Error("回执被改变")
			}
			return e
		}},
		{"detail", func() error {
			_, e := remote.QueryHongBaoDetail(QueryHongBaoDetailParams{NativeURL: native, SendUserName: "sender"})
			return e
		}},
		{"list", func() error {
			_, e := remote.QueryHongBaoList(QueryHongBaoListParams{NativeURL: native, SendUserName: "sender", Offset: 2, Limit: 3})
			return e
		}},
	} {
		before := calls
		if e := tc.run(); e != nil {
			t.Fatal(e)
		}
		if path != "/api/payment/hongbao/"+tc.path || body["native_url"] != native || calls != before+1 {
			t.Fatal("红包调用参数错误")
		}
	}
	r, e := remote.CollectMoney(CollectMoneyParams{InvalidTime: "123", TransferID: "transfer", TransactionID: "transaction", ToUserName: "sender"})
	if e != nil || r.GetText().GetValue() != `{"retcode":0}` || body["transaction_id"] != "transaction" || body["to_username"] != "sender" {
		t.Fatal("收款票据或回执丢失", e)
	}
	reject = true
	before := calls
	if _, e = remote.CollectMoney(CollectMoneyParams{}); e == nil || calls != before+1 {
		t.Fatal("失败被吞掉或重复调用")
	}
}

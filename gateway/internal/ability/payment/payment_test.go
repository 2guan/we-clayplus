// 文件名：payment_test.go
// 主要功能：验证收款参数与原回执映射、缺失回执及失败不自动重试。
// 作者：2guan
package paymentability

import (
	"errors"
	base "github.com/we-clay/we-clay/gateway/internal/api/base"
	api "github.com/we-clay/we-clay/gateway/internal/api/payment"
	sdk "github.com/we-clay/we-clay/sdk/payment"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

type fakePayment struct {
	api.PaymentService
	calls    int
	response *api.TenPayResponse
	packet   *api.HongBaoResponse
	err      error
	params   api.CollectMoneyParams
}

func (f *fakePayment) CollectMoney(p api.CollectMoneyParams) (*api.TenPayResponse, error) {
	f.calls++
	f.params = p
	return f.response, f.err
}
func (f *fakePayment) GrabHongBao(p api.GrabHongBaoParams) (*api.HongBaoResponse, error) {
	f.calls++
	return f.packet, f.err
}
func TestCollectMoneyReceipt(t *testing.T) {
	good := &api.TenPayResponse{BaseResponse: &base.BaseResponse{Code: proto.Int32(0)}, Text: wrapperspb.String(`{"retcode":0}`)}
	for _, v := range []struct {
		r  *api.TenPayResponse
		e  error
		ok bool
	}{{good, nil, true}, {nil, nil, false}, {nil, errors.New("unknown"), false}, {&api.TenPayResponse{}, nil, false}, {&api.TenPayResponse{BaseResponse: good.BaseResponse, ErrorType: proto.Int32(7)}, nil, false}} {
		f := &fakePayment{response: v.r, err: v.e}
		a := ability{api: f}
		r, e := a.CollectMoney(sdk.CollectMoneyParams{ToUserName: "peer", TransferID: "transfer", TransactionID: "transaction", InvalidTime: "123"})
		if (e == nil) != v.ok || f.calls != 1 || f.params.TransactionID != "transaction" {
			t.Fatal("回执或参数丢失", e)
		}
		if v.ok && r.Text != good.GetText().GetValue() {
			t.Fatal("原始回执被改变")
		}
	}
	f := &fakePayment{}
	a := ability{api: f}
	if _, e := a.CollectMoney(sdk.CollectMoneyParams{}); e == nil || f.calls != 0 {
		t.Fatal("空票据进入 core")
	}
}
func TestGrabHongBaoReceipt(t *testing.T) {
	f := &fakePayment{packet: &api.HongBaoResponse{BaseResponse: &base.BaseResponse{Code: proto.Int32(0)}, Text: &base.Buffer{Size: proto.Uint32(2), Data: []byte(`{}`)}}}
	a := ability{api: f}
	if _, e := a.GrabHongBao(sdk.GrabHongBaoParams{NativeURL: "https://example.com"}); e == nil || f.calls != 0 {
		t.Fatal("错误票据进入 core")
	}
	q := sdk.GrabHongBaoParams{NativeURL: "wxpay://c2cbizmessagehandler/hongbao/receivehongbao?msgtype=1&channelid=2&sendid=synthetic", InWay: 1}
	r, e := a.GrabHongBao(q)
	if e != nil || string(r.Text.Data) != "{}" || f.calls != 1 {
		t.Fatal(e)
	}
	f.packet.Text.Size = proto.Uint32(99)
	if _, e = a.GrabHongBao(q); e == nil || f.calls != 2 {
		t.Fatal("损坏回执被接受")
	}
}

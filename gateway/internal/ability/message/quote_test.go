// 文件名：quote_test.go
// 主要功能：验证引用使用应用消息 57 协议，完整保留回执且不降级为普通文本。
// 作者：2guan
package messageability

import (
	"errors"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

type quoteAPI struct {
	api.MessageService
	response *api.SendAppMessageResponse
	err      error
	calls    int
}

func (q *quoteAPI) SendApp(receiver, raw, extend string, typ int32) (*api.SendAppMessageResponse, error) {
	q.calls++
	if typ != 57 || !strings.Contains(raw, "<refermsg>") {
		panic("错误引用协议")
	}
	return q.response, q.err
}
func TestQuoteSendReceipt(t *testing.T) {
	SetOutboundReady(true)
	m := &sdk.Message{Type: sdk.TypeAppQuote, Receiver: &contact.Contact{Username: "synthetic-peer"}, Data: &sdk.Message_App{App: &sdk.AppData{SubType: 57, Xml: `<appmsg><type>57</type><title>回复</title><refermsg><svrid>123</svrid></refermsg></appmsg>`}}}
	for _, tc := range []struct {
		response *api.SendAppMessageResponse
		err      error
		success  bool
	}{
		{&api.SendAppMessageResponse{NewId: proto.Uint64(9007199254740993), CreateTime: proto.Int32(1234), ClientId: proto.String("synthetic-peer_1234")}, nil, true},
		{nil, nil, false}, {&api.SendAppMessageResponse{}, nil, false}, {nil, errors.New("发送结果未知"), false},
	} {
		q := &quoteAPI{response: tc.response, err: tc.err}
		a := &ability{api: q}
		r, e := a.Send(m)
		if (e == nil) != tc.success || q.calls != 1 {
			t.Fatal("错误被吞掉或发生重试")
		}
		if tc.success && (r.NewId != 9007199254740993 || r.CreateTime != 1234) {
			t.Fatal("回执丢失")
		}
	}
}

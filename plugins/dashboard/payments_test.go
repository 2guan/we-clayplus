// 文件名：payments_test.go
// 主要功能：脱机验证原消息票据绑定、人工确认、账号隔离和未知结果不重复提交。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type paymentHost struct {
	calls atomic.Int32
	fail  bool
}

func (h *paymentHost) Execute(_ context.Context, c control.Command) (any, error) {
	h.calls.Add(1)
	if c.Operation != "payment-claim" {
		return nil, fmt.Errorf("wrong operation")
	}
	if h.fail {
		return nil, fmt.Errorf("unknown outcome")
	}
	return map[string]bool{"confirmed": true}, nil
}
func paymentFixture(t *testing.T) (*Server, *paymentHost, Message) {
	s := newServer(t, true)
	h := &paymentHost{}
	s.host = h
	m := Message{ID: "payment-synthetic", Target: "synthetic-peer", Sender: "测试发送人", Kind: "transfer", Status: "sent", Rich: &RichMessage{Amount: "￥0.01"}}
	s.store.change(func(st *State) error {
		st.Mock = false
		st.Account.ID = "synthetic-self"
		st.Account.Status = "online"
		st.Messages = []Message{m}
		return nil
	})
	raw := `<msg><appmsg><type>2000</type><wcpayinfo><paysubtype>1</paysubtype><transferid>private-transfer</transferid><transcationid>private-transaction</transcationid><invalidtime>123</invalidtime></wcpayinfo></appmsg></msg>`
	if e := s.RememberQuoteSource(m, raw); e != nil {
		t.Fatal(e)
	}
	return s, h, m
}
func paymentCall(s *Server, m Message, confirmed bool, method string, role string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"target":%q,"id":%q,"confirmed":%t}`, m.Target, m.ID, confirmed)
	r := httptest.NewRequest(method, "/api/payments?target="+m.Target+"&id="+m.ID, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.paymentAPI(w, r, User{Role: role})
	return w
}
func TestPaymentBoundTicketAndOnce(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, h, m := paymentFixture(t)
			h.fail = fail
			w := paymentCall(s, m, false, "GET", "admin")
			if w.Code != 200 || strings.Contains(w.Body.String(), "private-") {
				t.Fatal("预览泄露支付票据", w.Body.String())
			}
			if h.calls.Load() != 0 {
				t.Fatal("预览调用了资金接口")
			}
			if paymentCall(s, m, false, "POST", "admin").Code == 200 || paymentCall(s, m, true, "POST", "viewer").Code != 403 {
				t.Fatal("绕过人工确认或权限")
			}
			w = paymentCall(s, m, true, "POST", "admin")
			if (w.Code == 200) == fail {
				t.Fatal(w.Body.String())
			}
			if paymentCall(s, m, true, "POST", "admin").Code != 409 || h.calls.Load() != 1 {
				t.Fatal("重复支付提交")
			}
			if e := s.initPayments(); e != nil {
				t.Fatal(e)
			}
			if paymentCall(s, m, true, "POST", "admin").Code != 409 || h.calls.Load() != 1 {
				t.Fatal("重启后重复提交")
			}
			_, _, _, e := s.paymentTicket("another-peer", m.ID)
			if e == nil {
				t.Fatal("跨会话票据")
			}
			s.store.change(func(st *State) error { st.Account.ID = "other-account"; return nil })
			if paymentCall(s, m, true, "POST", "admin").Code == 200 || h.calls.Load() != 1 {
				t.Fatal("跨账号使用旧票据")
			}
		})
	}
}
func TestRedpacketParsingAndTradeDedup(t *testing.T) {
	s, _, m := paymentFixture(t)
	m.Kind = "redpacket"
	s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil })
	raw := `<msg><appmsg><type>2001</type><wcpayinfo><nativeurl><![CDATA[wxpay://c2cbizmessagehandler/hongbao/receivehongbao?msgtype=1&channelid=2&sendid=synthetic-packet]]></nativeurl></wcpayinfo></appmsg></msg>`
	s.RememberQuoteSource(m, raw)
	_, ticket, key, e := s.paymentTicket(m.Target, m.ID)
	if e != nil || ticket.NativeURL == "" || ticket.InWay != 1 {
		t.Fatal(ticket, e)
	}
	b, _ := json.Marshal(ticket)
	if !strings.Contains(string(b), "synthetic-self") {
		t.Fatal("未绑定登录账号")
	}
	m.ID = "another-id"
	s.store.change(func(st *State) error { st.Messages = append(st.Messages, m); return nil })
	s.RememberQuoteSource(m, raw)
	_, _, other, e := s.paymentTicket(m.Target, m.ID)
	if e != nil || key != other {
		t.Fatal("同一红包被不同消息 ID 绕过")
	}
}

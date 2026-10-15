// 文件名：quote_test.go
// 主要功能：验证真实引用的会话绑定、原文转义及接收引用消息关联。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"testing"
)

func TestQuoteProtocol(t *testing.T) {
	s := newServer(t, true)
	st := s.store.snapshot()
	st.Account.ID = "self"
	st.Messages = []Message{{ID: "123", Target: "peer", SenderID: "peer", Sender: "测试", Kind: "text", Content: "原文<&>\n换行", Status: "sent"}}
	raw, rich, err := s.buildQuote(st, "peer", "123", "回复<&>")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Title string `xml:"title"`
		Type  int    `xml:"type"`
		Ref   struct {
			Content string `xml:"content"`
			ID      string `xml:"svrid"`
		} `xml:"refermsg"`
	}
	if xml.Unmarshal([]byte(raw), &v) != nil || v.Type != 57 || v.Title != "回复<&>" || v.Ref.Content != st.Messages[0].Content || rich.QuoteID != "123" {
		t.Fatal("引用协议不完整")
	}
	if _, _, err = s.buildQuote(st, "other", "123", "reply"); err == nil {
		t.Fatal("允许跨会话引用")
	}
	st.Messages[0].Status = "revoked"
	if _, _, err = s.buildQuote(st, "peer", "123", "reply"); err == nil {
		t.Fatal("允许引用撤回消息")
	}
	m, ok := parseRichMessage(Message{Kind: "app"}, `<msg><appmsg><type>57</type><title>回复</title><refermsg><type>3</type><svrid>123</svrid><displayname>测试</displayname><content>image</content></refermsg></appmsg></msg>`)
	if !ok || m.Rich.QuoteID != "123" || m.Rich.QuoteType != 3 {
		t.Fatal("未保存引用关联")
	}
}

// 文件名：quote.go
// 主要功能：按当前账号、会话和原消息构造微信引用协议，原始 XML 加密保存。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// 原文仅由可信接收链路写入；浏览器只能提交引用 ID，不能伪造发言人或 XML。
func (s *Server) RememberQuoteSource(m Message, raw string) error {
	if len(raw) > 4<<20 {
		return fmt.Errorf("引用原文过大")
	}
	if m.Kind == "emoji" {
		if err := s.collectSticker(raw); err != nil {
			return err
		}
	}
	return s.store.putSecret("quote:"+s.messageMediaKey(m), raw)
}
func (s *Server) buildQuote(st State, target, id, text string) (string, *RichMessage, error) {
	var original *Message
	for i := range st.Messages {
		m := &st.Messages[i]
		if m.ID == id && m.Target == target {
			original = m
			break
		}
	}
	if original == nil || !contextAvailable(*original) {
		return "", nil, fmt.Errorf("引用原消息不存在、已撤回或尚未确认发送")
	}
	if _, err := strconv.ParseUint(id, 10, 64); !st.Mock && (err != nil || id == "0") {
		return "", nil, fmt.Errorf("引用原消息缺少微信消息 ID")
	}
	kind := map[string]int{"text": 1, "image": 3, "audio": 34, "video": 43, "emoji": 47, "location": 48, "link": 49, "app": 49, "quote": 49, "file": 49, "transfer": 49, "redpacket": 49}[original.Kind]
	if kind == 0 {
		return "", nil, fmt.Errorf("该消息不支持引用")
	}
	raw, err := s.store.getSecret("quote:" + s.messageMediaKey(*original))
	if err != nil || raw == "" {
		if original.Kind == "text" {
			raw = original.Content
		} else {
			return "", nil, fmt.Errorf("该历史消息缺少引用原文，无法构造真实引用")
		}
	}
	sender := original.SenderID
	if original.Self {
		sender = st.Account.ID
	} else if sender == "" && !strings.HasSuffix(target, "@chatroom") {
		sender = target
	}
	if sender == "" {
		return "", nil, fmt.Errorf("引用原消息缺少发言人标识")
	}
	var v struct {
		XMLName xml.Name `xml:"appmsg"`
		Title   string   `xml:"title"`
		Type    int      `xml:"type"`
		Ref     struct {
			Type    int    `xml:"type"`
			ID      string `xml:"svrid"`
			From    string `xml:"fromusr"`
			Chat    string `xml:"chatusr"`
			Name    string `xml:"displayname"`
			Content string `xml:"content"`
		} `xml:"refermsg"`
	}
	v.Title = text
	v.Type = 57
	v.Ref.Type = kind
	v.Ref.ID = id
	v.Ref.From = sender
	v.Ref.Chat = sender
	if strings.HasSuffix(target, "@chatroom") {
		v.Ref.From = target
	}
	v.Ref.Name = original.Sender
	v.Ref.Content = raw
	data, err := xml.Marshal(v) // XML 转义保留原文特殊字符，不能拼接用户输入。
	return string(data), &RichMessage{Author: original.Sender, Quote: quotePreview(kind, raw), QuoteID: id, QuoteType: kind}, err
}

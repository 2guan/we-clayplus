// 文件名：wechat_data_test.go
// 主要功能：验证空联系人列表的消息会话恢复及微信图片来源限制。
// 作者：2guan
package dashboard

import "testing"

func TestMessagesRestoreMissingConversations(t *testing.T) {
	s := newServer(t, false)
	m := Message{ID: "fixture-msg", Target: "fixture-group@chatroom", Sender: "群成员", Kind: "text", Content: "测试消息", Time: "2026-10-13T10:00:00Z"}
	if err := s.Record(m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetContacts([]Contact{}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	if len(st.Contacts) != 1 || st.Contacts[0].ID != m.Target || st.Contacts[0].Name == m.Sender || st.Contacts[0].Preview != m.Content {
		t.Fatal("空通讯录隐藏了已有群聊，或把成员名当作群名")
	}
	if err := s.Record(m); err != nil {
		t.Fatal(err)
	}
	if len(s.store.snapshot().Messages) != 1 {
		t.Fatal("消息重复归档")
	}
}
func TestWechatImageOrigins(t *testing.T) {
	for _, raw := range []string{"https://wx.qlogo.cn/avatar", "http://mmsns.qpic.cn/image"} {
		if _, ok := wechatImageURL(raw); !ok {
			t.Fatal("拒绝微信图片来源")
		}
	}
	for _, raw := range []string{"http://127.0.0.1/avatar", "https://wx.qlogo.cn.evil.invalid/a", "https://user:password@wx.qlogo.cn/a", "https://wx.qlogo.cn:8088/a", "file:///tmp/a"} {
		if _, ok := wechatImageURL(raw); ok {
			t.Fatal("接受非微信图片来源")
		}
	}
}

// 同步游标不是聊天；保留普通 XML 文本及正常消息。
func TestConversationStatusFiltering(t *testing.T) {
	if !isConversationStatus(`<msg><op id="5"><name>lastMessage</name><arg>{}</arg></op></msg>`) {
		t.Fatal("同步事件未识别")
	}
	for _, v := range []string{"正常文字", `<msg><content>正常消息</content></msg>`, `<msg broken`} {
		if isConversationStatus(v) {
			t.Fatal("误删真实文字")
		}
	}
}

// 明确的远端取消置顶必须覆盖旧本地值，资料增量缺失状态则保留上次同步结果。
func TestWechatConversationStatePrecedence(t *testing.T) {
	s := newServer(t, true)
	yes, no := true, false
	if err := s.SetContacts([]Contact{{ID: "fixture-group", Kind: "group", Pinned: true, WechatPinned: &yes, Muted: &yes}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetContacts([]Contact{{ID: "fixture-group", Kind: "group", WechatPinned: &no, Muted: &no}}); err != nil {
		t.Fatal(err)
	}
	c := s.store.snapshot().Contacts[0]
	if c.Pinned || c.WechatPinned == nil || *c.WechatPinned || c.Muted == nil || *c.Muted {
		t.Fatal("旧本地状态覆盖了远端明确关闭")
	}
	if err := s.SetContacts([]Contact{{ID: "fixture-group", Kind: "group"}}); err != nil {
		t.Fatal(err)
	}
	c = s.store.snapshot().Contacts[0]
	if c.WechatPinned == nil || c.Muted == nil {
		t.Fatal("资料增量丢失已知状态")
	}
}

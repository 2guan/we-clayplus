// 文件名：wechat_message_test.go
// 主要功能：验证公众号多图文、安全链接、系统配置过滤及历史会话名称修复。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWechatArticleCards(t *testing.T) {
	raw := `<msg><appmsg><title>摘要</title><type>5</type><url>https://example.com/fallback</url><mmreader><category><name>测试公众号</name><item><title><![CDATA[第一篇 & 详情]]></title><url>https://example.com/one?a=1&amp;b=2</url><digest>文章摘要</digest><cover>https://mmbiz.qpic.cn/fixture</cover></item><item><title>第二篇</title><url>https://example.com/two</url></item><item><title>非法链接</title><url>javascript:alert(1)</url></item></category></mmreader></appmsg></msg>`
	m, keep := ParseWechatMessage(Message{Target: "gh_fixture", Kind: "link"}, raw)
	if !keep || len(m.Articles) != 2 || m.Articles[0].URL != "https://example.com/one?a=1&b=2" || m.Content != "第一篇 & 详情" || m.Publisher != "测试公众号" {
		t.Fatal("图文解析不完整")
	}
	v := State{Contacts: []Contact{{ID: "gh_fixture", Name: "gh_fixture"}}, Messages: []Message{m}}
	normalizeWechatArchive(&v)
	if v.Contacts[0].Name != "测试公众号" || len(v.Messages[0].Articles) != 2 {
		t.Fatal("名称补齐或重复归档丢失卡片")
	}
	plain, keep := ParseWechatMessage(Message{Kind: "text", Content: raw}, raw)
	if !keep || len(plain.Articles) != 0 {
		t.Fatal("把用户粘贴的 XML 误当成公众号消息")
	}
}
func TestWechatSystemMessages(t *testing.T) {
	raw := `<sysmsg type="dynacfg"><dynacfg><Item key="fixture">0</Item></dynacfg></sysmsg>`
	if _, keep := ParseWechatMessage(Message{Kind: "system", Content: raw}, raw); keep {
		t.Fatal("配置通知泄漏到聊天")
	}
	if _, keep := ParseWechatMessage(Message{Kind: "text", Content: raw}, raw); !keep {
		t.Fatal("误删普通用户文字")
	}
	if _, keep := ParseWechatMessage(Message{Kind: "system"}, `<sysmsg type="revokemsg"><revokemsg><replacemsg><![CDATA[对方撤回了一条消息]]></replacemsg></revokemsg></sysmsg>`); keep {
		t.Fatal("撤回通知重复进入聊天")
	}
	if id, content, ok := parseRevokeNotice(`<sysmsg type="revokemsg"><revokemsg><newmsgid>123</newmsgid><replacemsg><![CDATA[对方撤回了一条消息]]></replacemsg></revokemsg></sysmsg>`); !ok || id != "123" || content != "对方撤回了一条消息" {
		t.Fatal("撤回原消息关联解析失败")
	}
	if _, keep := ParseWechatMessage(Message{}, `<msg><op id="5"><name>HandOffMaster</name></op></msg>`); keep {
		t.Fatal("同步通知泄漏")
	}
	v := State{Contacts: []Contact{{ID: "weixin", Preview: raw}}, Messages: []Message{{Target: "weixin", Kind: "system", Content: raw}}}
	normalizeWechatArchive(&v)
	if len(v.Messages) != 0 || v.Contacts[0].Preview != "" {
		t.Fatal("历史配置通知未清理")
	}
}

func TestWechatRevokeArchive(t *testing.T) {
	for _, self := range []bool{false, true} {
		content, status := "对方撤回了一条消息", "revoked_by_other"
		if self {
			content, status = "你撤回了一条消息", "revoked"
		}
		notice := Message{Kind: "system", Content: `<sysmsg type="revokemsg"><revokemsg><newmsgid>123</newmsgid><replacemsg>` + content + `</replacemsg></revokemsg></sysmsg>`}
		original := Message{ID: "123", Target: "fixture-peer", Kind: "text", Content: "原消息", Self: self}
		for _, messages := range [][]Message{{original, notice}, {notice, original}} {
			v := State{Messages: messages}
			normalizeWechatArchive(&v)
			normalizeWechatArchive(&v)
			if len(v.Messages) != 1 || v.Messages[0].ID != "123" || v.Messages[0].Status != status {
				t.Fatalf("历史撤回状态丢失或通知重复：%+v", v.Messages)
			}
			if !self && v.Messages[0].Content != content {
				t.Fatal("对方撤回提示丢失")
			}
		}
	}
}

func TestWechatRevokeRecord(t *testing.T) {
	s := newServer(t, false)
	if err := s.store.change(func(v *State) error {
		v.Messages = []Message{{ID: "123", Target: "fixture-peer", Kind: "text", Content: "原消息"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	notice := Message{ID: "456", Target: "fixture-peer", Kind: "system", Content: `<sysmsg type="revokemsg"><revokemsg><newmsgid>123</newmsgid><replacemsg>对方撤回了一条消息</replacemsg></revokemsg></sysmsg>`}
	for range 2 {
		if err := s.Record(notice); err != nil {
			t.Fatal(err)
		}
	}
	v := s.store.snapshot()
	if len(v.Messages) != 1 || v.Messages[0].Status != "revoked_by_other" || v.Messages[0].Content != "对方撤回了一条消息" {
		t.Fatalf("撤回事件未更新原消息：%+v", v.Messages)
	}
}

// 转发文章只携带加密 CDN 缩略图时，票据应落入加密存储并在重载后保留图片入口。
func TestForwardedArticleThumbnail(t *testing.T) {
	raw := `<msg><appmsg><title>转发文章</title><url>https://example.com/article</url><appattach><cdnthumburl>fixture-cdn-id</cdnthumburl><cdnthumbaeskey>fixture-secret</cdnthumbaeskey></appattach></appmsg></msg>`
	m, _ := ParseWechatMessage(Message{Kind: "link"}, raw)
	s := newServer(t, false)
	if err := s.prepareArticleThumbnails(&m); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(m)
	if strings.Contains(string(data), "fixture-secret") || strings.Contains(string(data), "fixture-cdn-id") {
		t.Fatal("缩略图票据泄漏")
	}
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	restored, _ = ParseWechatMessage(restored, restored.Content)
	if len(restored.Articles) != 1 || !strings.HasPrefix(restored.Articles[0].Image, "/api/message-thumbnail?id=") {
		t.Fatal("重载后缩略图丢失")
	}
	ref := strings.TrimPrefix(restored.Articles[0].Image, "/api/message-thumbnail?id=")
	secret, err := s.store.getSecret("message-thumbnail:" + ref)
	if err != nil || !strings.Contains(secret, "fixture-secret") {
		t.Fatal("缺少加密票据")
	}
}

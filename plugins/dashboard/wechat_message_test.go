// 文件名：wechat_message_test.go
// 主要功能：验证公众号多图文、安全链接、系统配置过滤及历史会话名称修复。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"github.com/we-clay/we-clay/plugins/ai"
	"strings"
	"testing"
	"time"
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

const fixtureInviteNotice = `<sysmsg type="sysmsgtemplate"><sysmsgtemplate><content_template><plain/><template><![CDATA["$username$"邀请"$names$"加入了群聊]]></template><link_list><link name="username" type="link_profile"><memberlist><member><username>fixture-inviter</username><nickname>邀请成员</nickname></member></memberlist></link><link name="names" type="link_profile"><memberlist><member><username>fixture-a</username><nickname>成员甲</nickname></member><member><username>fixture-b</username><nickname>成员乙</nickname></member></memberlist><separator>、</separator></link></link_list></content_template></sysmsgtemplate></sysmsg>`
const fixtureCallNotice = `<voipmsg type="VoIPBubbleMsg"><VoIPBubbleMsg><msg><![CDATA[对方已取消]]></msg><room_type>1</room_type><roomid>fixture-room</roomid><roomkey>fixture-room-key</roomkey><identity>fixture-identity</identity><duration>0</duration></VoIPBubbleMsg></voipmsg>`

func TestWechatReadableTemplateAndCall(t *testing.T) {
	m, keep := ParseWechatMessage(Message{Kind: "system"}, fixtureInviteNotice)
	if !keep || m.Content != `"邀请成员"邀请"成员甲、成员乙"加入了群聊` {
		t.Fatal("邀请模板未展开", m.Content)
	}
	rename := `<sysmsg type="sysmsgtemplate"><sysmsgtemplate><content_template><template>"$username$"修改群名为“$remark$”</template><link_list><link name="username"><memberlist><member><nickname>测试成员</nickname></member></memberlist></link><link name="remark"><plain>新群名</plain></link></link_list></content_template></sysmsgtemplate></sysmsg>`
	m, _ = ParseWechatMessage(Message{Kind: "system"}, rename)
	if m.Content != `"测试成员"修改群名为“新群名”` {
		t.Fatal("纯文本模板变量未展开", m.Content)
	}
	m, keep = ParseWechatMessage(Message{Kind: "call"}, fixtureCallNotice)
	if !keep || m.Kind != "call" || m.Rich == nil || m.Rich.Status != "对方已取消" || m.Rich.Title != "语音通话" || m.Content != "[语音通话] 对方已取消" {
		t.Fatal("通话未转为展示内容", m)
	}
	encoded, _ := json.Marshal(m)
	for _, secret := range []string{"fixture-room", "fixture-identity", "voipmsg"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("通话协议信息泄漏", secret)
		}
	}
	for _, raw := range []string{fixtureCallNotice, fixtureInviteNotice} {
		text, _ := ParseWechatMessage(Message{Kind: "text", Content: raw}, raw)
		if text.Kind != "text" || text.Content != raw {
			t.Fatal("用户粘贴 XML 被误识别")
		}
	}
	for _, raw := range []string{`<voipmsg`, `<voipmsg type="unknown"><roomkey>fixture-secret</roomkey></voipmsg>`} {
		m, _ := ParseWechatMessage(Message{Kind: "call", Content: raw}, raw)
		if m.Content != "[通话通知]" {
			t.Fatal("未知通话协议泄漏", m.Content)
		}
	}
}

func TestRestoreWechatNoticesFromEncryptedSources(t *testing.T) {
	s := newServer(t, false)
	// 切换后的 epoch 存在 store 中，不在持久化的 State 内。
	replyRegressionCheck(t, s.SetAccount(Account{ID: "fixture-notice-account"}))
	account := s.store.snapshot().Account.ID
	group := Message{ID: "fixture-invite", Target: "fixture-room@chatroom", Kind: "system", Content: "[微信系统通知，请在微信中查看详情]", Status: "sent"}
	call := Message{ID: "fixture-call", Target: "fixture-peer", Kind: "text", Content: fixtureCallNotice, Status: "sent", Time: "2026-10-22T12:00:00+08:00"}
	pasted := call
	pasted.ID = "fixture-pasted"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Messages = []Message{group, call, pasted}
		st.Contacts = []Contact{{ID: call.Target, Time: call.Time, Preview: call.Content}}
		return nil
	}))
	replyRegressionCheck(t, s.RememberQuoteSource(group, fixtureInviteNotice))
	for _, m := range []Message{call, pasted} {
		replyRegressionCheck(t, s.RememberQuoteSource(m, fixtureCallNotice))
		code := `{"type":{"code":50}}`
		if m.ID == pasted.ID {
			code = `{"type":{"code":1}}`
		}
		replyRegressionCheck(t, s.store.putSecret(accountMessageMediaKey(account, m), code))
	}
	replyRegressionCheck(t, s.restoreNoticeArchive())
	st := s.store.snapshot()
	if st.Messages[0].Content != `"邀请成员"邀请"成员甲、成员乙"加入了群聊` || st.Messages[1].Kind != "call" || st.Messages[1].Rich.Status != "对方已取消" || st.Messages[2].Kind != "text" || st.Messages[2].Content != fixtureCallNotice || st.Contacts[0].Preview != "[语音通话] 对方已取消" {
		t.Fatal("历史恢复错误", st.Messages)
	}
	original, err := s.store.getSecret("quote:" + accountMessageMediaKey(account, call))
	replyRegressionCheck(t, err)
	if original != fixtureCallNotice {
		t.Fatal("原始加密证据被改写")
	}
	revision := st.Revision
	replyRegressionCheck(t, s.restoreNoticeArchive())
	if s.store.snapshot().Revision != revision {
		t.Fatal("重复恢复仍改写状态")
	}
}

func TestWechatRevokeArchive(t *testing.T) {
	for _, self := range []bool{false, true} {
		content, status := "对方撤回了一条消息", "revoked_by_other"
		if self {
			content, status = "你撤回了一条消息", "revoked"
		}
		notice := Message{Target: "fixture-peer", Kind: "system", Content: `<sysmsg type="revokemsg"><revokemsg><newmsgid>123</newmsgid><replacemsg>` + content + `</replacemsg></revokemsg></sysmsg>`}
		original := Message{ID: "123", Target: "fixture-peer", Kind: "text", Content: "原消息", Self: self}
		for _, messages := range [][]Message{{original, notice}, {notice, original}} {
			v := State{Messages: messages}
			normalizeWechatArchive(&v)
			normalizeWechatArchive(&v)
			if len(v.Messages) != 1 || v.Messages[0].ID != "123" || v.Messages[0].Status != status {
				t.Fatalf("历史撤回状态丢失或通知重复：%+v", v.Messages)
			}
			if v.Messages[0].Content != original.Content || contextAvailable(v.Messages[0]) {
				t.Fatal("撤回原文丢失或进入模型上下文")
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
	if len(v.Messages) != 1 || v.Messages[0].Status != "revoked_by_other" || v.Messages[0].Content != "原消息" || contextAvailable(v.Messages[0]) {
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

func TestRestoreRevokedContentForDisplayOnly(t *testing.T) {
	s, current := replyRegressionFixture(t)
	st := s.store.snapshot()
	original := Message{ID: "fixture-revoked-text", Target: current.Target, Sender: "测试成员", SenderID: current.SenderID, Kind: "text", Content: "撤回前的唯一原文", Status: "sent", Time: current.Time}
	replyRegressionCheck(t, s.recordMemoryArchive(st.Account.ID, original))
	legacy := original
	legacy.Status, legacy.Content = "revoked_by_other", "对方撤回了一条消息"
	fallback := legacy
	fallback.ID = "fixture-revoked-quote-source"
	replyRegressionCheck(t, s.RememberQuoteSource(fallback, "加密引用中保留的旧原文"))
	missing := legacy
	missing.ID = "fixture-revoked-missing"
	deleted := legacy
	deleted.ID = "fixture-revoked-deleted"
	replyRegressionCheck(t, s.RememberQuoteSource(deleted, "已删除原文不能恢复"))
	_, err := s.store.db.Exec("INSERT INTO memory_deleted_sources(account,target,message_id) VALUES(?,?,?)", st.Account.ID, deleted.Target, deleted.ID)
	replyRegressionCheck(t, err)
	quoted := current
	quoted.Kind, quoted.Rich = "quote", &RichMessage{QuoteID: original.ID, QuoteUnavailable: true}
	replyRegressionCheck(t, s.store.change(func(v *State) error { v.Messages = []Message{legacy, fallback, missing, deleted, quoted}; return nil }))
	replyRegressionCheck(t, s.restoreNoticeArchive())
	after := s.store.snapshot()
	if after.Messages[0].Content != original.Content || after.Messages[1].Content != "加密引用中保留的旧原文" || after.Messages[2].Content != missing.Content || after.Messages[3].Content != deleted.Content {
		t.Fatal("旧撤回原文恢复错误", after.Messages)
	}
	for _, m := range after.Messages[:4] {
		if !strings.HasPrefix(m.Status, "revoked") || contextAvailable(m) {
			t.Fatal("恢复展示改变了撤回状态", m)
		}
	}
	conv, history := buildReplyContext(after, quoted, time.Now())
	for _, turn := range ai.ConversationMessages(ai.Request{Prompt: quoted.Content, Conversation: &conv, Context: history}) {
		if strings.Contains(turn.Text, original.Content) || strings.Contains(turn.Text, "加密引用中保留的旧原文") {
			t.Fatal("撤回原文泄露到模型上下文", turn.Text)
		}
	}
	revision := after.Revision
	replyRegressionCheck(t, s.restoreNoticeArchive())
	if s.store.snapshot().Revision != revision {
		t.Fatal("重复恢复改写状态")
	}
}

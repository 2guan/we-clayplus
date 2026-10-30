package dashboard

import (
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

func TestPatNamesPreserveIdentityTemplateAndSuffix(t *testing.T) {
	raw := `<sysmsg type="pat"><pat><fromusername>fixture-actor</fromusername><pattedusername>fixture-recipient</pattedusername><template><![CDATA["${fixture-actor}" 拍了拍 "${fixture-recipient}" 的
^ ^ 👀 "⚫️" 👅 对方 ${literal}]]></template></pat></sysmsg>`
	m, keep := ParseWechatMessage(Message{ID: "fixture-pat", Target: "fixture-room", Kind: "system"}, raw)
	if !keep || m.Interaction.Template == "" {
		t.Fatal("原始拍一拍模板丢失")
	}
	st := State{Account: Account{ID: "fixture-self"}, Messages: []Message{
		{Target: m.Target, SenderID: "fixture-actor", Sender: "发起者", Kind: "text"},
		{Target: m.Target, SenderID: "fixture-recipient", Sender: "被拍者", Kind: "text"},
	}}
	resolved := resolveNotice(st, m)
	if resolved.Content != `"发起者" 拍了拍 "被拍者" 的`+"\n^ ^ 👀 \"⚫️\" 👅 对方 ${literal}" || resolved.Interaction.ActorName != "发起者" || resolved.Interaction.TargetName != "被拍者" || !resolved.Interaction.Verified || resolved.Interaction.ActorID != "fixture-actor" || resolved.Interaction.TargetID != "fixture-recipient" {
		t.Fatal("姓名、身份、后缀或换行错误", resolved)
	}
	if m.Interaction.ActorName != "" || m.Content != m.Interaction.Template {
		t.Fatal("展示解析污染原始记录")
	}
	st.Contacts = []Contact{{ID: "fixture-actor", Name: "联系人昵称", Remark: "我的备注"}}
	if got := resolveNotice(st, m); !strings.HasPrefix(got.Content, `"我的备注"`) {
		t.Fatal("备注没有优先")
	}
	st.Account.ID = "fixture-actor"
	if got := resolveNotice(st, m); got.Interaction.ActorName != "我" {
		t.Fatal("本人被误当作其他成员")
	}
}

func TestPatLegacyViewsResolveSameConversationAndKeepArchive(t *testing.T) {
	s := newServer(t, true)
	legacy := `"对方" 拍了拍 "对方" 的 "对方" 👀`
	err := s.store.change(func(st *State) error {
		st.Contacts = nil
		st.Messages = []Message{
			{Target: "fixture-other-room", Kind: "text", SenderID: "fixture-target", Sender: "不能跨群借用的名字"},
			{Target: "fixture-room", Kind: "text", SenderID: "fixture-actor", Sender: "同名昵称"},
			{Target: "fixture-room", Kind: "text", SenderID: "fixture-target", Sender: "旧昵称"},
			{ID: "fixture-pat", Target: "fixture-room", Kind: "system", Content: legacy, Interaction: &contextcleaner.Interaction{Kind: "pat", ActorID: "fixture-actor", TargetID: "fixture-target", Verified: true}},
		}
		return nil
	})
	replyRegressionCheck(t, err)
	st := s.store.snapshot()
	m := st.Messages[3]
	if m.Content != `"同名昵称" 拍了拍 "旧昵称" 的 "对方" 👀` || m.Interaction.ActorName != "同名昵称" || m.Interaction.TargetName != "旧昵称" {
		t.Fatal("旧消息未修复，或后缀被误改", m)
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		if st.Messages[3].Content != legacy || st.Messages[3].Interaction.ActorName != "" {
			t.Error("读取展示副本改写了归档")
		}
		st.Messages = append(st.Messages, Message{Target: "fixture-room", Kind: "text", SenderID: "fixture-target", Sender: "新昵称"})
		return nil
	}))
	if got := s.store.snapshot().Messages[3]; got.Interaction.TargetName != "新昵称" || !strings.Contains(got.Content, `"新昵称"`) {
		t.Fatal("后续姓名没有更新到旧记录")
	}
	// 名称相同也保留不同协议ID，不靠正文的称呼合并身份。
	m.Interaction.Template = `"${fixture-actor}" 拍了拍 "${fixture-missing}"`
	m.Interaction.TargetID, m.Interaction.TargetName = "fixture-missing", ""
	m = resolveNotice(st, m)
	if m.Interaction.TargetName != "" || !strings.Contains(m.Content, "未知成员（fixture-missing）") {
		t.Fatal("未知身份被冒充熟人", m)
	}
	m.Interaction.TargetID = "fixture-target"
	st.Messages = st.Messages[:1]
	m = resolveNotice(st, m)
	if m.Interaction.TargetName != "" {
		t.Fatal("从其他群拼接成员名字")
	}
}

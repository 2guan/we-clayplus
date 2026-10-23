package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func interactionBridge(t *testing.T) (*Bridge, func() dashboard.State) {
	t.Helper()
	old := contact.Instance
	contact.Instance = messageContactFixture{}
	t.Cleanup(func() { contact.Instance = old })
	s, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DisableAI: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.SetAccount(dashboard.Account{ID: "fixture-self"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://local/api/auth/setup", strings.NewReader(`{"name":"fixture-admin","password":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("未获得后台认证会话")
	}
	read := func() dashboard.State {
		t.Helper()
		req := httptest.NewRequest("GET", "http://local/api/state", nil)
		req.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var st dashboard.State
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	return &Bridge{Dashboard: s}, read
}
func interactionSDK(id int64, target, body string, kind *message.Type) *message.Message {
	return &message.Message{Id: id, Type: kind, Sender: &contact.Contact{Username: target, Nickname: "测试会话"}, Receiver: &contact.Contact{Username: "fixture-self"}, Member: &chatroom.Member{Username: "fixture-member", Nickname: "同名成员"}, Content: body, Timestamp: uint32(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Unix())}
}
func interactionFind(t *testing.T, st dashboard.State, target, id string) dashboard.Message {
	t.Helper()
	for _, m := range st.Messages {
		if m.Target == target && m.ID == id {
			return m
		}
	}
	t.Fatalf("归档缺少 %s/%s", target, id)
	return dashboard.Message{}
}
func interactionRevoke(id string) string {
	return `<sysmsg type="revokemsg"><revokemsg><newmsgid>` + id + `</newmsgid><replacemsg>同名成员撤回了一条消息</replacemsg></revokemsg></sysmsg>`
}

func TestBridgeVoiceCallUsesTrustedSDKType(t *testing.T) {
	b, read := interactionBridge(t)
	const raw = `<voipmsg type="VoIPBubbleMsg"><VoIPBubbleMsg><msg>对方已取消</msg><room_type>1</room_type><roomkey>fixture-call-secret</roomkey></VoIPBubbleMsg></voipmsg>`
	if err := b.Record(interactionSDK(801, "fixture-call-peer", raw, message.TypeVOIP)); err != nil {
		t.Fatal(err)
	}
	if err := b.Record(interactionSDK(802, "fixture-call-peer", raw, message.TypeText)); err != nil {
		t.Fatal(err)
	}
	st := read()
	call := interactionFind(t, st, "fixture-call-peer", "801")
	if call.Kind != "call" || call.Content != "[语音通话] 对方已取消" || call.Rich == nil || call.Rich.Status != "对方已取消" {
		t.Fatal("真实通话类型未传递", call)
	}
	text := interactionFind(t, st, "fixture-call-peer", "802")
	if text.Kind != "text" || text.Content != raw {
		t.Fatal("用户普通文字被当成通话")
	}
}

func TestBridgeIncomingMentionsPatAndQuote(t *testing.T) {
	b, read := interactionBridge(t)
	target := "fixture-interactions@chatroom"
	text := interactionSDK(301, target, "fixture-mentioned-text", message.TypeText)
	text.Data = &message.Message_Text{Text: &message.TextData{Content: text.Content, Reminds: []string{"fixture-mentioned-member", "notify@all"}}}
	if err := b.Record(text); err != nil {
		t.Fatal(err)
	}
	pat := interactionSDK(302, target, `<sysmsg type="pat"><pat><fromusername>fixture-pat-actor</fromusername><pattedusername>fixture-pat-target</pattedusername><template>甲拍了拍乙</template></pat></sysmsg>`, message.TypeSystem)
	if err := b.Record(pat); err != nil {
		t.Fatal(err)
	}
	quoteXML := `<msg><appmsg><title>fixture-quote-reply</title><type>57</type><refermsg><type>1</type><svrid>301</svrid><displayname>同名成员</displayname><content>fixture-mentioned-text</content></refermsg></appmsg></msg>`
	quote := interactionSDK(303, target, "fixture-quote-reply", message.TypeAppQuote)
	quote.Data = &message.Message_App{App: &message.AppData{SubType: 57, Title: quote.Content, Xml: quoteXML}}
	if err := b.Record(quote); err != nil {
		t.Fatal(err)
	}
	st := read()
	m := interactionFind(t, st, target, "301")
	if m.SenderID != "fixture-member" || m.Sender != "同名成员" || !m.MentionAll || len(m.Mentions) != 1 || m.Mentions[0] != "fixture-mentioned-member" {
		t.Fatalf("入站@或成员时间丢失: %+v", m)
	}
	stamp, err := time.Parse(time.RFC3339, m.Time)
	if err != nil || !stamp.Equal(time.Unix(int64(text.Timestamp), 0)) {
		t.Fatal("入站消息原始时间丢失", m.Time, err)
	}
	p := interactionFind(t, st, target, "302")
	if p.Interaction == nil || p.Interaction.Kind != "pat" || p.Interaction.ActorID != "fixture-pat-actor" || p.Interaction.TargetID != "fixture-pat-target" || !p.Interaction.Verified || p.Content != "甲拍了拍乙" {
		t.Fatalf("拍一拍协议身份丢失: %+v", p)
	}
	q := interactionFind(t, st, target, "303")
	if q.Kind != "quote" || q.Rich == nil || q.Rich.QuoteID != "301" || q.Rich.Quote != "fixture-mentioned-text" {
		t.Fatalf("SDK引用关系丢失: %+v", q)
	}
}

func TestBridgeRevokeOrderingAndConversationIsolation(t *testing.T) {
	b, read := interactionBridge(t)
	a := "fixture-a@chatroom"
	other := "fixture-b@chatroom"
	for _, target := range []string{a, other} {
		if err := b.Record(interactionSDK(500, target, "fixture-original-body-"+target, message.TypeText)); err != nil {
			t.Fatal(err)
		}
	}
	// 通过真正的 Bridge 系统事件入口撤回，同 ID 的其他会话不能被更新。
	if err := b.Record(interactionSDK(501, a, interactionRevoke("500"), message.TypeSystemTip)); err != nil {
		t.Fatal(err)
	}
	st := read()
	revoked := interactionFind(t, st, a, "500")
	untouched := interactionFind(t, st, other, "500")
	if !strings.HasPrefix(revoked.Status, "revoked") || strings.Contains(revoked.Content, "fixture-original-body") {
		t.Fatal("Bridge撤回未到达原消息", revoked)
	}
	if strings.HasPrefix(untouched.Status, "revoked") || untouched.Content != "fixture-original-body-"+other {
		t.Fatal("同ID跨会话误撤回", untouched)
	}
	// 撤回先到、正文后到，正文仍须受持久化 tombstone 约束。
	early := "fixture-early@chatroom"
	if err := b.Record(interactionSDK(701, early, interactionRevoke("700"), message.TypeSystem)); err != nil {
		t.Fatal(err)
	}
	if err := b.Record(interactionSDK(700, early, "fixture-late-revoked-body", message.TypeText)); err != nil {
		t.Fatal(err)
	}
	if err := b.Record(interactionSDK(700, other, "fixture-same-id-other-conversation", message.TypeText)); err != nil {
		t.Fatal(err)
	}
	st = read()
	late := interactionFind(t, st, early, "700")
	cross := interactionFind(t, st, other, "700")
	if !strings.HasPrefix(late.Status, "revoked") || strings.Contains(late.Content, "fixture-late-revoked-body") {
		t.Fatal("先到撤回未约束迟到正文", late)
	}
	if strings.HasPrefix(cross.Status, "revoked") || cross.Content != "fixture-same-id-other-conversation" {
		t.Fatal("先到撤回污染其他会话", cross)
	}
	for _, m := range st.Messages {
		if m.ID == "501" || m.ID == "701" {
			t.Fatal("撤回控制通知误作聊天正文", m)
		}
	}
}

func TestBridgeUserXMLCannotForgeInteractionOrRevoke(t *testing.T) {
	b, read := interactionBridge(t)
	target := "fixture-pasted@chatroom"
	if err := b.Record(interactionSDK(800, target, "fixture-keep-original", message.TypeText)); err != nil {
		t.Fatal(err)
	}
	examples := []string{interactionRevoke("800"), `<sysmsg type="pat"><pat><fromusername>forged-actor</fromusername><pattedusername>forged-target</pattedusername><template>伪造拍一拍</template></pat></sysmsg>`}
	for i, body := range examples {
		if err := b.Record(interactionSDK(int64(810+i), target, body, message.TypeText)); err != nil {
			t.Fatal(err)
		}
	}
	st := read()
	original := interactionFind(t, st, target, "800")
	if strings.HasPrefix(original.Status, "revoked") || original.Content != "fixture-keep-original" {
		t.Fatal("普通正文伪造撤回", original)
	}
	for i, body := range examples {
		pasted := interactionFind(t, st, target, fmt.Sprint(810+i))
		if pasted.Kind != "text" || pasted.Interaction != nil || pasted.Content != body {
			t.Fatal("用户粘贴XML升级为系统互动", pasted)
		}
	}
}

// SDK App 消息没有 TextData，@ 关系来自原始协议 source，不能从标题/正文猜测。
func TestBridgeAppRawSourceMentions(t *testing.T) {
	b, read := interactionBridge(t)
	target := "fixture-app-source@chatroom"
	source := `<msgsource><atuserlist>fixture-app-member,notify@all</atuserlist><bizflag>0</bizflag></msgsource>`
	for i, kind := range []*message.Type{message.TypeAppQuote, message.TypeAppLink} {
		subtype := uint32(57)
		xmlBody := `<msg><appmsg><type>57</type><title>fixture-app-reply</title><refermsg><type>1</type><svrid>fixture-quoted-message</svrid><content>旧消息</content></refermsg></appmsg></msg>`
		if i == 1 {
			subtype = 5
			xmlBody = `<msg><appmsg><type>5</type><title>fixture-app-link</title><url>https://example.com/fixture</url></appmsg></msg>`
		}
		m := interactionSDK(int64(900+i), target, "fixture-app-body", kind)
		m.Data = &message.Message_App{App: &message.AppData{SubType: subtype, Title: "fixture-app-body", Xml: xmlBody}}
		raw, _ := json.Marshal(map[string]any{"source": source})
		m.Raw = string(raw)
		if err := b.Record(m); err != nil {
			t.Fatal(err)
		}
		saved := interactionFind(t, read(), target, fmt.Sprint(900+i))
		if !saved.MentionAll || len(saved.Mentions) != 1 || saved.Mentions[0] != "fixture-app-member" {
			t.Fatalf("App/引用原始source的@丢失: %+v", saved)
		}
	}
	pasted := interactionSDK(910, target, source, message.TypeText)
	if err := b.Record(pasted); err != nil {
		t.Fatal(err)
	}
	saved := interactionFind(t, read(), target, "910")
	if saved.MentionAll || len(saved.Mentions) != 0 || saved.Content != source {
		t.Fatal("普通正文被当作@协议", saved)
	}
}

type interactionMissingContactCache struct{ messageContactFixture }

func (interactionMissingContactCache) Get(string) *contact.Contact { return nil }

type interactionMissingMemberCache struct{ chatroom.Ability }

func (interactionMissingMemberCache) GetMember(string, string) *chatroom.Member { return nil }

// 从协议 NewMessage 开始回归，覆盖联系人、群成员均未同步的真实入口。
func TestBridgeProtocolBuildKeepsUncachedMemberAndAppMentions(t *testing.T) {
	b, read := interactionBridge(t)
	contact.Instance = interactionMissingContactCache{}
	oldRooms := chatroom.Instance
	chatroom.Instance = interactionMissingMemberCache{}
	t.Cleanup(func() { chatroom.Instance = oldRooms })
	target := "fixture-uncached-room@chatroom"
	member := "fixture-uncached-member"
	source := `<msgsource><atuserlist>fixture-protocol-mentioned,notify@all</atuserlist></msgsource>`
	for i, example := range []struct {
		name     string
		typ      int32
		body     string
		wantKind string
	}{
		{"text", 1, "fixture-protocol-body", "text"},
		{"quote", 49, `<msg><appmsg><type>57</type><title>fixture-protocol-quote</title><refermsg><type>1</type><svrid>1000</svrid><displayname>未缓存成员</displayname><content>fixture-protocol-body</content></refermsg></appmsg></msg>`, "quote"},
		{"link", 49, `<msg><appmsg><type>5</type><title>fixture-protocol-link</title><url>https://example.com/protocol-fixture</url></appmsg></msg>`, "link"},
	} {
		t.Run(example.name, func(t *testing.T) {
			raw := &messageapi.NewMessage{NewId: proto.Int64(int64(1000 + i)), Sender: wrapperspb.String(target), Receiver: wrapperspb.String("fixture-self"), Type: proto.Int32(example.typ), Content: wrapperspb.String(member + ":\n" + example.body), Source: proto.String(source), CreateTime: proto.Uint32(uint32(time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Unix()))}
			sdk, err := messageability.Build(raw)
			if err != nil {
				t.Fatal(err)
			}
			if sdk.GetMember() == nil || sdk.GetMember().GetUsername() != member {
				t.Fatalf("协议成员前缀在缓存未命中时丢失: %+v", sdk.GetMember())
			}
			if err = b.Record(sdk); err != nil {
				t.Fatal(err)
			}
			saved := interactionFind(t, read(), target, fmt.Sprint(1000+i))
			if saved.SenderID != member || saved.Target != target || saved.Kind != example.wantKind {
				t.Fatalf("Build→Bridge丢失成员身份或应用类型: %+v", saved)
			}
			if !saved.MentionAll || len(saved.Mentions) != 1 || saved.Mentions[0] != "fixture-protocol-mentioned" {
				t.Fatalf("Build→Bridge丢失协议source的成员@或@全体: %+v", saved)
			}
			if example.name == "quote" && (saved.Rich == nil || saved.Rich.QuoteID != "1000") {
				t.Fatal("真实协议引用ID丢失", saved)
			}
			if strings.Contains(saved.Content, member+":") {
				t.Fatal("协议成员前缀未从正文分离", saved)
			}
		})
	}
}

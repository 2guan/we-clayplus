package dashboard

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestPlainReplyMentionsExactGroupMembers(t *testing.T) {
	st := State{Account: Account{ID: "fixture-self"}, Contacts: []Contact{
		{ID: "fixture-a", Name: "本地昵称", Remark: "好友备注"},
		{ID: "fixture-outsider", Name: "群外好友"},
	}}
	members := []RoomMember{
		{ID: "fixture-a", Name: "甲", DisplayName: "甲 同学"},
		{ID: "fixture-b", Name: "乙"},
		{ID: "fixture-c", Name: "重名"}, {ID: "fixture-d", Name: "重名"},
		{ID: "fixture-self", Name: "本人"},
		{ID: "notify@all", Name: "所有人"},
	}
	for _, tc := range []struct {
		text, want string
		ids        []string
	}{
		{"@甲 说句话", "@甲\u2005说句话", []string{"fixture-a"}},
		{"@甲 同学，吃饭了吗", "@甲 同学\u2005，吃饭了吗", []string{"fixture-a"}},
		{"@好友备注\u2005@乙 你好", "@好友备注\u2005@乙\u2005你好", []string{"fixture-a", "fixture-b"}},
		{"@甲 @甲 好", "@甲\u2005@甲\u2005好", []string{"fixture-a"}},
		{"@乙", "@乙\u2005", []string{"fixture-b"}},
	} {
		cmd, err := plainReplyMentions(st, members, tc.text)
		if err != nil || cmd.Content != tc.want || !slices.Equal(cmd.Mentions, tc.ids) {
			t.Fatal(tc.text, cmd, err)
		}
	}
	for _, text := range []string{"@重名 你好", "@群外好友 你好", "@本人 你好", "@所有人 你好", "@甲乙 你好", "@甲 @未知 好"} {
		if _, err := plainReplyMentions(st, members, text); err == nil {
			t.Fatal("没有拒绝无法核实的提醒", text)
		}
	}
}

// 复现实际故障：模型仅返回普通 @昵称，没有 reply 标签，目标也不在近期消息中。
func TestAIPlainAtReplyReachesNativeSend(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"@群内昵称 吃饭了吗"}}]}`)
	})
	m.Target, m.SenderID = "fixture-plain-at@chatroom", "fixture-current-person"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		st.Settings["tts.reply"] = "语音"
		return nil
	}))
	var reads, sends atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, cmd control.Command) (any, error) {
		switch cmd.Operation {
		case "room-info":
			reads.Add(1)
			if cmd.Target != m.Target {
				t.Error("读取了其他会话")
			}
			return RoomInfo{Members: []RoomMember{{ID: "fixture-earlier-person", Name: "昵称", DisplayName: "群内昵称"}}}, nil
		case "send":
			sends.Add(1)
			var params struct {
				Mentions []string `json:"mentions"`
				Kind     string   `json:"kind"`
			}
			_ = json.Unmarshal(cmd.Params, &params)
			if !slices.Equal(params.Mentions, []string{"fixture-earlier-person"}) || params.Kind != "text" || cmd.Content != "@群内昵称\u2005吃饭了吗" {
				t.Error("只发送了文字 @ 或转成了语音", cmd.Content, params)
			}
			return map[string]string{"id": "fixture-native-at-receipt"}, nil
		default:
			t.Error("意外操作", cmd.Operation)
			return nil, nil
		}
	})
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || reads.Load() != 1 || sends.Load() != 1 {
		t.Fatal(task, reads.Load(), sends.Load())
	}
	for _, archived := range s.store.snapshot().Messages {
		if archived.ID == "fixture-native-at-receipt" && !slices.Equal(archived.Mentions, []string{"fixture-earlier-person"}) {
			t.Fatal("归档丢失真正的提醒参数", archived)
		}
	}
}

func TestPlainAtPlanFailsBeforeAnyWrite(t *testing.T) {
	for _, fail := range []string{"ambiguous", "read-failed", "empty-roster", "wrong-account"} {
		t.Run(fail, func(t *testing.T) {
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
			m.Target = "fixture-at-failure@chatroom"
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{m}
				st.Settings["reply.max_parts"] = float64(3)
				return nil
			}))
			q := replyRequest(s.store.snapshot(), m)
			var err error
			q, err = s.refreshReplyContext(q)
			replyRegressionCheck(t, err)
			if fail == "wrong-account" {
				q.AccountID = "fixture-other-account"
			}
			s.host = replyRegressionHost(func(_ context.Context, cmd control.Command) (any, error) {
				if cmd.Operation != "room-info" {
					t.Error("无效 @ 前就发送了部分消息", cmd.Operation)
				}
				if fail == "read-failed" {
					return nil, fmt.Errorf("成员查询失败")
				}
				if fail == "empty-roster" {
					return RoomInfo{}, nil
				}
				return RoomInfo{Members: []RoomMember{{ID: "fixture-a", Name: "同名"}, {ID: "fixture-b", Name: "同名"}}}, nil
			})
			if err = s.deliverReplyEvidence(context.Background(), m.Target, "前一句\n\n@同名 后一句", "text", nil, &q); err == nil {
				t.Fatal("无效 @ 未停止发送")
			}
		})
	}
}

func TestPlainAtCompatibilityOnlyAppliesToGroupReplyPrefix(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	m.Target = "fixture-at-scope@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	base, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	s.host = replyRegressionHost(func(context.Context, control.Command) (any, error) {
		t.Error("无关回复不应查询群成员")
		return nil, nil
	})
	for _, kind := range []string{"manual", "private", "proactive", "body", "code", "native-tag"} {
		q := base
		conv := *base.Conversation
		q.Conversation = &conv
		request := &q
		p := replyPart{Kind: "text", Text: "@甲 你好"}
		switch kind {
		case "manual":
			request = nil
		case "private":
			conv.Scene = "private"
		case "proactive":
			q.Source = "wechat-proactive"
		case "body":
			p.Text = "你可以输入 @甲 来提醒"
		case "code":
			p.Text = "```\n@甲 例子\n```"
		case "native-tag":
			p.ReplyRef, p.ReplyMode = "m12345678", "at"
		}
		commands, err := s.preparePlainReplyMentions(context.Background(), s.store.snapshot(), request, []replyPart{p})
		if err != nil || len(commands) != 0 {
			t.Fatal(kind, commands, err)
		}
	}
}

func TestAIReplyAddressReachesSendWithReceipt(t *testing.T) {
	for _, mode := range []string{"at", "quote", "both", "private-quote"} {
		t.Run(mode, func(t *testing.T) {
			var modelCalls atomic.Int32
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				modelCalls.Add(1)
				var body struct {
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				ref := ""
				for _, turn := range body.Messages {
					found := regexp.MustCompile(`〔(m[0-9a-f]{8})〕[^\n]*：较早发言`).FindStringSubmatch(turn.Content)
					if len(found) == 2 {
						ref = found[1]
					}
				}
				if ref == "" {
					t.Error("模型没拿到可选择的实际消息短编号")
				}
				selected := strings.TrimPrefix(mode, "private-")
				answer := fmt.Sprintf(`<reply ref="%s" mode="%s">这句我接的是你的话，A&amp;B 都可以。</reply>`, ref, selected)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
			})
			m.ID, m.Target, m.SenderID = "1234002", "fixture-address@chatroom", "fixture-last-speaker"
			kind := "group"
			if mode == "private-quote" {
				m.Target, m.SenderID, kind = "fixture-address-friend", "fixture-address-friend", "friend"
			}
			old := m
			old.ID, old.SenderID, old.Sender, old.Content = "1234001", "fixture-original-speaker", "同名成员", "较早发言"
			if kind == "friend" {
				old.SenderID = m.SenderID
			}
			m.Sender, m.Content = old.Sender, "这是另一条消息"
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: kind, Mode: "auto"}}
				st.Messages = []Message{old, m}
				st.Settings["tts.reply"] = "语音" // 定向文字不能丢失 @/引用而被改成普通语音。
				return nil
			}))
			var calls atomic.Int32
			var sent control.Command
			account := s.store.snapshot().Account.ID
			s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
				calls.Add(1)
				sent = c
				return map[string]string{"id": "1234999", "client_id": "fixture-client", "original_client_id": "fixture-original-client", "create_time": "1792657800", "account": account}, nil
			})
			task := retrySubmit(t, s, m)
			if task.Status != "completed" || calls.Load() != 1 || modelCalls.Load() != 1 {
				t.Fatal(task, calls.Load(), modelCalls.Load())
			}
			var params struct {
				Mentions []string `json:"mentions"`
				QuoteXML string   `json:"quote_xml"`
				Kind     string   `json:"kind"`
			}
			if err := json.Unmarshal(sent.Params, &params); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(sent.Content, "<reply") || strings.Contains(sent.Content, "&amp;") || !strings.Contains(sent.Content, "A&B") || sent.Target != m.Target {
				t.Fatal("协议标签泄漏或正文/目标错误", sent.Content)
			}
			mentions := mode == "at" || mode == "both"
			if mentions && (len(params.Mentions) != 1 || params.Mentions[0] != old.SenderID || !strings.HasPrefix(sent.Content, "@同名成员\u2005")) {
				t.Fatal("没有对所选消息的真实发言人发出 @", params)
			}
			if !mentions && len(params.Mentions) != 0 {
				t.Fatal("擅自提醒成员", params)
			}
			quote := mode != "at"
			if quote {
				var v struct {
					Type  int    `xml:"type"`
					Title string `xml:"title"`
					Ref   struct {
						ID      string `xml:"svrid"`
						Chat    string `xml:"chatusr"`
						Content string `xml:"content"`
					} `xml:"refermsg"`
				}
				if xml.Unmarshal([]byte(params.QuoteXML), &v) != nil || v.Type != 57 || v.Ref.ID != old.ID || v.Ref.Chat != old.SenderID || v.Ref.Content != old.Content || v.Title != sent.Content {
					t.Fatal("原生引用未绑定选中的真实原消息", params.QuoteXML)
				}
			} else if params.QuoteXML != "" {
				t.Fatal("单独 @ 被变成引用")
			}
			st := s.store.snapshot()
			found := false
			for _, msg := range st.Messages {
				if msg.ID != "1234999" {
					continue
				}
				found = true
				if msg.RevokeReceipt == nil || msg.RevokeReceipt.ClientID != "fixture-client" || msg.RevokeReceipt.OriginalClientID != "fixture-original-client" || msg.RevokeReceipt.Created != "1792657800" || msg.Status != "sent" || quote && (msg.Rich == nil || msg.Rich.QuoteID != old.ID) {
					t.Fatal("发送回执或引用关联丢失", msg)
				}
			}
			if !found {
				t.Fatal("真实回执 ID 未写回归档")
			}
		})
	}
}

func TestAddressedReplyRejectsInvalidTargetsAndNestedActions(t *testing.T) {
	for _, raw := range []string{
		`<reply ref="m1" mode="at"><voice>台词</voice></reply>`,
		`<reply ref="m1" mode="quote" target="other">正文</reply>`,
		`<reply ref="m1" mode="at" mode="quote">正文</reply>`,
		`<reply ref="m1" mode="unknown">正文</reply>`,
		`<reply ref="m1" mode="at"/>`,
		"```xml\n<reply ref=\"m1\" mode=\"at\">正文</reply>\n```",
	} {
		if _, err := parseReply(raw, 5); err == nil {
			t.Fatal("接受了不受支持的发送计划", raw)
		}
	}
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	m.ID, m.Target, m.SenderID = "1234567", "fixture-address-validation@chatroom", "fixture-person"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	q, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	ref := ""
	for key := range ai.ReplyTargets(q) {
		ref = key
	}
	for _, tc := range []string{"unknown-ref", "revoked", "cross-conversation", "self", "private-at", "planning", "hidden-thinking", "silence"} {
		t.Run(tc, func(t *testing.T) {
			st := s.store.snapshot()
			q2 := q
			conv := *q.Conversation
			q2.Conversation = &conv
			p := replyPart{Kind: "text", Text: "接一下你的话", ReplyRef: ref, ReplyMode: "both"}
			switch tc {
			case "unknown-ref":
				p.ReplyRef = "mdeadbeef"
			case "revoked":
				st.Messages[0].Status = "revoked_self"
			case "cross-conversation":
				st.Messages[0].Target = "fixture-other@chatroom"
			case "self":
				st.Messages[0].Self = true
			case "private-at":
				q2.Conversation.Scene = "private"
			case "planning":
				p.Text = "I notice the current message is about dinner and I should stay quiet."
			case "hidden-thinking":
				p.Text = "<think>私下分析</think>正文"
			case "silence":
				p.Text = "NO_REPLY: 无需补充"
			}
			raw := fmt.Sprintf(`<reply ref="%s" mode="%s">%s</reply>`, p.ReplyRef, p.ReplyMode, xmlEscapedText(p.Text))
			if err := replyPlanError(st, q2, ai.Result{Text: raw}); err == nil {
				t.Fatal("无效目标或正文通过了发送前检查")
			}
		})
	}
}

func xmlEscapedText(text string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(text))
	return out.String()
}

func TestAddressedReplyCannotBecomeProactiveOrSendWithoutEvidence(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	text := `<reply ref="m1234abcd" mode="both">这是一条回复</reply>`
	if err := s.deliverReply(context.Background(), m.Target, text); err == nil {
		t.Fatal("没有来源证据也发送了定向回复")
	}
	if err := s.deliverProactive(context.Background(), ai.Request{Target: m.Target}, text); err == nil {
		t.Fatal("主动发言接受了定向操作")
	}
	if sends.Load() != 0 {
		t.Fatal("非法定向回复发生外发")
	}
}

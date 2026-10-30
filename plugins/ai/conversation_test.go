package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

func capturedConversationTurns(t *testing.T, provider string, body map[string]json.RawMessage) ([]string, []string, string) {
	t.Helper()
	key := "messages"
	if provider == "gemini" {
		key = "contents"
	}
	var turns []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Parts   json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(body[key], &turns); err != nil {
		t.Fatal(err)
	}
	var roles, texts []string
	system := ""
	for _, turn := range turns {
		raw := turn.Content
		if provider == "gemini" {
			raw = turn.Parts
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			var parts []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &parts); err != nil {
				t.Fatal(err)
			}
			for _, part := range parts {
				if text == "" {
					text = part.Text
				}
			}
		}
		if turn.Role == "system" {
			system += text
			continue
		}
		role := turn.Role
		if role == "model" {
			role = "assistant"
		}
		roles = append(roles, role)
		texts = append(texts, text)
	}
	if provider == "anthropic" {
		if err := json.Unmarshal(body["system"], &system); err != nil {
			t.Fatal(err)
		}
	}
	if provider == "gemini" {
		var v struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		if err := json.Unmarshal(body["system_instruction"], &v); err != nil {
			t.Fatal(err)
		}
		for _, p := range v.Parts {
			system += p.Text
		}
	}
	return roles, texts, system
}

// 捕获真实出站 HTTP，避免仅测试中间结构而遗漏供应商适配时丢失资料。
func TestConversationProviderHTTPMetadataRolesAndMedia(t *testing.T) {
	for _, provider := range []string{"openai", "mimo", "gemini", "anthropic"} {
		for _, mode := range []string{"chat", "vision", "insight-images"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				incoming := make(chan map[string]json.RawMessage, 1)
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					incoming <- body
					fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-reply"}}],"content":[{"type":"text","text":"fixture-reply"}],"candidates":[{"content":{"parts":[{"text":"fixture-reply"}]}}]}`)
				}))
				defer remote.Close()
				current := contextcleaner.Message{ID: "fixture-current-id", Session: "fixture-room@chatroom", Role: "user", Sender: "同名成员", SenderID: "fixture-current-sender", Source: "wechat", Kind: "text", Text: "fixture-current-body", Time: "2026-10-15T18:20:30+08:00", Mentions: []string{"fixture-mentioned-id"}, MentionAll: true, Quote: &contextcleaner.Reference{ID: "fixture-quoted-id", SenderID: "fixture-quoted-sender", Text: "fixture-quoted-excerpt", Verified: true}, Interaction: &contextcleaner.Interaction{Kind: "pat", ActorID: "fixture-pat-actor", TargetID: "fixture-pat-target", Verified: true}}
				q := Request{Kind: mode, Prompt: current.Text, System: "fixture-stable-system", Conversation: &Conversation{AccountID: "fixture-account", SelfName: "本人称呼", Names: map[string]string{"fixture-mentioned-id": "被提醒成员", "fixture-pat-actor": "拍者", "fixture-pat-target": "被拍者"}, Session: current.Session, Scene: "group", Now: "2026-10-15T18:21:00+08:00", Timezone: "Asia/Shanghai", Current: current, References: []contextcleaner.Message{{ID: "fixture-quoted-id", Role: "user", SenderID: "fixture-quoted-sender", Sender: "被引用成员", Text: "fixture-reference-only", Source: "archive"}}, Memories: []MemoryEvidence{{ID: "fixture-memory-id", Version: 2, Scope: "group", Subject: current.Session, Target: current.Session, Content: "fixture-memory-only", ConfirmedAt: "2026-10-14T10:00:00+08:00", Sources: []string{"fixture-memory-source"}}}}, Context: []contextcleaner.Message{{ID: "fixture-history-user", Session: current.Session, Role: "user", SenderID: "fixture-other-sender", Sender: "历史成员", Text: "fixture-history-user-body", Source: "wechat"}, {ID: "fixture-history-self", Session: current.Session, Role: "assistant", SenderID: "fixture-account", Text: "fixture-history-assistant-body", Source: "ai"}}}
				q.Context = append(q.Context, contextcleaner.Message{ID: "fixture-game-turn", Session: current.Session, Role: "user", SenderID: "fixture-game-player", Sender: "游戏玩家", Game: "turtlesoup", Text: "fixture-public-game-question", Source: "wechat"})
				media := base64.StdEncoding.EncodeToString([]byte("fixture-image-bytes"))
				if mode == "vision" {
					q.Media = media
					q.MIME = "image/png"
				}
				if mode == "insight-images" {
					q.Kind = "insight"
					q.Images = []InputImage{{Label: "fixture-image-label", MIME: "image/png", Data: media}, {Label: "fixture-second-image-label", MIME: "image/jpeg", Data: base64.StdEncoding.EncodeToString([]byte("fixture-second-image-bytes"))}}
				}
				result, err := Execute(context.Background(), Config{Provider: provider, Endpoint: remote.URL, Key: "fixture-local-key", Model: "fixture-model"}, q, false)
				if err != nil || result.Text != "fixture-reply" {
					t.Fatal(result, err)
				}
				body := <-incoming
				roles, texts, system := capturedConversationTurns(t, provider, body)
				if strings.Join(roles, ",") != "user,assistant,user,user" {
					t.Fatalf("资料、历史或本轮角色错误: %v", roles)
				}
				if system != "fixture-stable-system" || strings.Contains(system, "fixture-memory-only") || strings.Contains(system, "fixture-reference-only") {
					t.Fatal("资料提升为系统规则", system)
				}
				all := strings.Join(texts, "\n")
				if strings.Count(all, current.Text) != 1 {
					t.Fatal("本轮消息重复或丢失", all)
				}
				if !strings.Contains(all, "fixture-memory-only") || !strings.Contains(all, "fixture-reference-only") {
					t.Fatal("参考资料缺失", texts[0])
				}
				if !strings.Contains(texts[0], "历史成员：fixture-history-user-body") || texts[1] != "fixture-history-assistant-body" {
					t.Fatal("历史正文或身份丢失", texts)
				}
				if !strings.Contains(all, "游戏玩家〔海龟汤游戏·虚构〕：fixture-public-game-question") {
					t.Fatal("供应商请求丢失游戏标记", texts)
				}
				for _, forbidden := range []string{"fixture-account", "fixture-room@chatroom", "fixture-history-self", "sender_id", "assistant_metadata", "Asia/Shanghai"} {
					if strings.Contains(all, forbidden) {
						t.Fatal("冗余元数据仍进入实际请求", forbidden)
					}
				}
				for _, value := range []string{"2026-10-15 18:21:00（北京时间）", "场景：群聊", "当前发言人：同名成员", "18:20:30", "@被提醒成员", "@全体", "引用#1", "拍者拍了拍被拍者"} {
					if !strings.Contains(texts[len(texts)-1], value) {
						t.Fatal("缺少本轮信息或互动关系", value, texts[1])
					}
				}
				if mode != "chat" {
					turnKey := "messages"
					if provider == "gemini" {
						turnKey = "contents"
					}
					var wireTurns []json.RawMessage
					if err := json.Unmarshal(body[turnKey], &wireTurns); err != nil {
						t.Fatal(err)
					}
					raw := wireTurns[len(wireTurns)-1]
					if !strings.Contains(string(raw), media) {
						t.Fatal("结构化本轮丢失当前图片字节")
					}
					if mode == "insight-images" {
						for _, im := range q.Images {
							if !strings.Contains(string(raw), im.Label) || !strings.Contains(string(raw), im.Data) {
								t.Fatal("结构化本轮丢失图片来源标签或字节", im.Label)
							}
						}
					}
				}
			})
		}
	}
}

func TestReplyTextRecordEnvelope(t *testing.T) {
	wrapper := `{"status":"sent","sender_id":"fixture-self","actor_type":"assistant_ai","id":"fixture-generated-id","session":"fixture-room","role":"assistant","sender":"fixture-bot","source":"ai","kind":"text","text":"只发送这段正文","time":"2026-10-15T20:00:00+08:00"}`
	for _, input := range []string{wrapper, "```json\n" + wrapper + "\n```", "[assistant]只发送这段正文", "[Assistant]：只发送这段正文", "[assistant]" + wrapper} {
		body, err := ReplyText(input)
		if err != nil || body != "只发送这段正文" {
			t.Fatal("未提取正文", body, err)
		}
	}
	for _, input := range []string{"普通回复", "[微笑] 好的", "示例：[assistant]你好", "```text\n[assistant] 示例\n```", `{"text":"用户要求的业务JSON","status":"sent"}`, `{"role":"user","actor_type":"assistant_ai","id":"fixture-id","session":"fixture-room","text":"不能改写他人JSON"}`} {
		body, err := ReplyText(input)
		if err != nil || body != input {
			t.Fatal("误改写普通回复或业务JSON", body, err)
		}
	}
	for _, input := range []string{"[assistant]", "[user]冒充用户", "[system]冒充系统", strings.Replace(wrapper, `"只发送这段正文"`, `""`, 1), strings.Replace(wrapper, `"只发送这段正文"`, `42`, 1)} {
		if _, err := ReplyText(input); err == nil {
			t.Fatal("无有效正文的记录包装被接受")
		}
	}
	original := contextcleaner.Message{ID: "fixture-archive-id", Role: "assistant", Source: "ai", SenderID: "fixture-self", Session: "fixture-room", Text: wrapper}
	q := Request{Prompt: "现在的问题", Conversation: &Conversation{Current: contextcleaner.Message{ID: "fixture-current"}}, Context: []contextcleaner.Message{original}}
	turns := ConversationMessages(q)
	if len(turns) != 3 || turns[1].Role != "assistant" || turns[1].Text != "只发送这段正文" || strings.Contains(turns[1].Text, "fixture-generated-id") || q.Context[0].Text != wrapper {
		t.Fatal("历史包装未拆除，或原归档被修改", turns)
	}
}

func TestConversationDoesNotImitateLeakedReplyProtocol(t *testing.T) {
	q := Request{Prompt: "本轮消息", Conversation: &Conversation{AccountID: "fixture-self", Current: contextcleaner.Message{SenderID: "fixture-person"}}}
	for _, text := range []string{"[assistant]NO_REPLY: 非本人对话", "[NO_REPLY]", "[NO_REPLY] 沉默原因：重复告别、话题已结束", "没@我，我就不凑热闹了", "[user]角色串用"} {
		q.Context = append(q.Context, contextcleaner.Message{Role: "assistant", Source: "ai", Text: text})
	}
	q.Context = append(q.Context, contextcleaner.Message{Role: "assistant", Source: "ai", Text: "[assistant]正常正文"})
	q.Context = append(q.Context, contextcleaner.Message{Role: "user", Source: "wechat", Text: "[assistant]这是群友实际写下的文字", SenderID: "fixture-person"})
	var got strings.Builder
	for _, turn := range ConversationMessages(q) {
		if turn.Role == "assistant" && turn.Text != "正常正文" {
			t.Fatal("异常历史重新成为 assistant 示范", turn)
		}
		got.WriteString(turn.Text)
	}
	if strings.Contains(got.String(), "NO_REPLY") || strings.Contains(got.String(), "不凑热闹") || !strings.Contains(got.String(), "[assistant]这是群友实际写下的文字") || q.Context[0].Text != "[assistant]NO_REPLY: 非本人对话" {
		t.Fatal("异常历史未隔离，或原始归档/群友文字被改写", got.String())
	}
}

func TestConversationImageRecordsAreReferenceData(t *testing.T) {
	for _, status := range []string{"尚未完成识图，不能推断画面", "正在识图，不能推断画面", "识图失败，画面内容未知", "模型识图描述，仅供参考"} {
		t.Run(status, func(t *testing.T) {
			image := "[已发送·AI生成图片（不代表真实拍摄或现实经历）；" + status + "]\n生成意图（不代表实际画面）：一只猫坐在窗边"
			q := Request{Prompt: "换一个角度", Conversation: &Conversation{AccountID: "fixture-self", SelfName: "本人称呼", Current: contextcleaner.Message{SenderID: "fixture-person"}}, Context: []contextcleaner.Message{
				{Role: "assistant", Source: "ai", Kind: "text", Text: "之前的正常回复"},
				{Role: "assistant", Source: "ai", Kind: "image", MediaText: true, Text: image},
				{Role: "assistant", Source: "ai", Kind: "text", Text: image}, // 历史误发文字不能再次成为示范。
				{Role: "assistant", Source: "ai", Kind: "text", Text: "之后的正常回复"},
			}}
			var references, replies strings.Builder
			for _, turn := range ConversationMessages(q) {
				if turn.Role == "assistant" {
					replies.WriteString(turn.Text)
				} else {
					references.WriteString(turn.Text)
				}
			}
			if strings.Contains(replies.String(), "生成意图") || !strings.Contains(replies.String(), "之前的正常回复") || !strings.Contains(replies.String(), "之后的正常回复") {
				t.Fatal("内部记录成为 assistant 示范，或普通文字丢失", replies.String())
			}
			if !strings.Contains(references.String(), "内部参考，非聊天正文") || strings.Count(references.String(), "一只猫坐在窗边") != 1 || !strings.Contains(references.String(), "本人称呼〔本人〕") || q.Context[1].Text != image || q.Context[2].Text != image {
				t.Fatal("图片证据、身份丢失或原归档被改写", references.String())
			}
		})
	}
}

func TestConversationSeparatesSpeakerFromTextAddressee(t *testing.T) {
	c := &Conversation{AccountID: "fixture-self", SelfName: "离线本人", Scene: "group", Names: map[string]string{"fixture-person-a": "成员甲", "fixture-person-b": "成员乙"}, Current: contextcleaner.Message{SenderID: "fixture-person-b", Sender: "成员乙"}}
	q := Request{Prompt: "成员甲，那是第三位群友发的表情，不是离线本人发的。", Conversation: c, Context: []contextcleaner.Message{{Role: "user", SenderID: "fixture-person-a", Sender: "成员甲", Text: "之前的发言"}}}
	turns := ConversationMessages(q)
	last := turns[len(turns)-1].Text
	if !strings.Contains(last, "当前发言人：成员乙") || !strings.Contains(last, "成员乙〔正文称呼：成员甲；仅作指向线索〕") || strings.Contains(last, "提醒：") || c.Current.SenderID != "fixture-person-b" {
		t.Fatal("文本称呼被当成发言人、真实提醒，或丢失对话对象线索", last)
	}
}

func TestCompactConversationDatesNamesAndEvidence(t *testing.T) {
	q := Request{Prompt: "本轮唯一正文", Conversation: &Conversation{AccountID: "fixture-self-secret-id", SelfName: "本人昵称", Scene: "group", Now: "2026-10-16T00:01:03+08:00", Names: map[string]string{"fixture-mentioned": "被提醒者"}, Current: contextcleaner.Message{ID: "fixture-current", SenderID: "fixture-b", Sender: "同名", Time: "2026-10-15T16:01:00Z", Mentions: []string{"fixture-mentioned"}, Quote: &contextcleaner.Reference{ID: "fixture-ref", Verified: true}}, References: []contextcleaner.Message{{ID: "fixture-ref", SenderID: "fixture-a", Sender: "同名", Text: "必要引用唯一正文", Time: "2026-10-15T23:58:00+08:00"}}}, Context: []contextcleaner.Message{
		{ID: "fixture-history-a", SenderID: "fixture-a", Sender: "同名", Text: "旧日消息", Time: "2026-10-15T15:59:30Z"},
		{ID: "fixture-history-b", SenderID: "fixture-b", Sender: "同名", Text: "新日消息", Time: "2026-10-15T16:00:10Z"},
		{ID: "fixture-pat", Kind: "system", Time: "2026-10-15T16:00:30Z", Interaction: &contextcleaner.Interaction{Kind: "pat", ActorID: "fixture-b", TargetID: "fixture-self-secret-id", Verified: true}},
	}}
	turns := ConversationMessages(q)
	if len(turns) != 2 || !turns[1].Current {
		t.Fatal(turns)
	}
	all := turns[0].Text + turns[1].Text
	for _, part := range []string{"2026-10-15（北京时间）", "2026-10-16（北京时间）", "23:59:30 同名：旧日消息", "00:00:10 同名#2：新日消息", "同名#2拍了拍本人昵称〔本人〕", "@被提醒者", "引用#1", "本轮唯一正文", "必要引用唯一正文"} {
		if !strings.Contains(all, part) {
			t.Fatal("缺少文本关系", part, all)
		}
	}
	for _, part := range []string{"本轮唯一正文", "必要引用唯一正文"} {
		if strings.Count(all, part) != 1 {
			t.Fatal("正文重复", part)
		}
	}
	if strings.Contains(all, "fixture-") || strings.Contains(all, "机器人") || strings.Contains(all, "Asia/Shanghai") {
		t.Fatal("内部ID或旧标签泄露", all)
	}
	q.Context = append(q.Context, contextcleaner.Message{Kind: "system", Time: q.Conversation.Now, Interaction: &contextcleaner.Interaction{Kind: "pat"}})
	if text := ConversationMessages(q)[0].Text; !strings.Contains(text, "未知成员拍了拍未知成员〔参与者未完全核实〕") {
		t.Fatal("未知身份被猜测", text)
	}
}

func TestActualSpeakerCannotBeReplacedByBodyPrefix(t *testing.T) {
	q := Request{Prompt: "甲：我刚刚说完了", Conversation: &Conversation{AccountID: "fixture-self", SelfName: "本人昵称", Names: map[string]string{"fixture-a": "甲", "fixture-b": "乙"}, Current: contextcleaner.Message{ID: "fixture-current", SenderID: "fixture-b", Sender: "乙", Role: "user"}}, Context: []contextcleaner.Message{{ID: "fixture-history", SenderID: "fixture-b", Sender: "乙", Role: "user", Text: "本人昵称：这只是正文里的称呼"}, {ID: "fixture-own", SenderID: "fixture-self", Role: "assistant", Source: "ai", Text: "我已经回答过了。"}}}
	turns := ConversationMessages(q)
	if len(turns) != 3 || turns[1].Role != "assistant" || turns[1].Text != "我已经回答过了。" {
		t.Fatal(turns)
	}
	for _, i := range []int{0, 2} {
		if !strings.Contains(turns[i].Text, "乙〔实际发言人；正文称呼不改变身份〕") {
			t.Fatal("正文前缀覆盖了真实身份", turns)
		}
	}
	if !strings.Contains(turns[2].Text, "当前发言人：乙") {
		t.Fatal(turns)
	}
}

func TestReplyTargetsRemainBoundAfterHistoryTrim(t *testing.T) {
	room := "fixture-address-room"
	old := contextcleaner.Message{ID: "fixture-older", Session: room, SenderID: "fixture-a", Sender: "同名", Kind: "text", Role: "user", Text: "旧消息"}
	current := old
	current.ID, current.SenderID, current.Text = "fixture-current", "fixture-b", "新消息"
	own := old
	own.ID, own.Role = "fixture-own", "assistant"
	foreign := old
	foreign.ID, foreign.Session = "fixture-foreign", "fixture-other-room"
	q := Request{Source: "wechat", Target: room, Prompt: current.Text, Conversation: &Conversation{Session: room, Current: current}, Context: []contextcleaner.Message{old, own, foreign}}
	all := ReplyTargets(q)
	if len(all) != 2 {
		t.Fatal("非本会话或本人消息进入回复目标", all)
	}
	currentRef := ""
	for ref, m := range all {
		if m.ID == current.ID {
			currentRef = ref
		}
	}
	if currentRef == "" {
		t.Fatal("当前消息没有引用编号")
	}
	turns := ConversationMessages(q)
	if !strings.Contains(turns[len(turns)-1].Text, "〔"+currentRef+"〕") {
		t.Fatal("模型没有收到目标编号")
	}
	q.Context = nil
	trimmed := ReplyTargets(q)
	if len(trimmed) != 1 || trimmed[currentRef].ID != current.ID {
		t.Fatal("历史裁剪导致编号错指其他人", trimmed)
	}
	q.PlainReply = true
	if len(ReplyTargets(q)) != 0 {
		t.Fatal("纠正轮仍开放定向操作")
	}
}

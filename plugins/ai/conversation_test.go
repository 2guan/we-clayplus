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
				if strings.Join(roles, ",") != "user,user" {
					t.Fatalf("资料、历史或本轮角色错误: %v", roles)
				}
				if system != "fixture-stable-system" || strings.Contains(system, "fixture-memory-only") || strings.Contains(system, "fixture-reference-only") {
					t.Fatal("资料提升为系统规则", system)
				}
				all := strings.Join(texts, "\n")
				if strings.Count(all, current.Text) != 1 {
					t.Fatal("本轮消息重复或丢失", all)
				}
				if !strings.Contains(texts[0], "fixture-memory-only") || !strings.Contains(texts[0], "fixture-reference-only") {
					t.Fatal("参考资料缺失", texts[0])
				}
				if !strings.Contains(texts[0], "历史成员：fixture-history-user-body") || !strings.Contains(texts[0], "本人称呼〔本人〕：fixture-history-assistant-body") {
					t.Fatal("历史正文或身份丢失", texts)
				}
				for _, forbidden := range []string{"fixture-account", "fixture-room@chatroom", "fixture-history-self", "sender_id", "assistant_metadata", "Asia/Shanghai"} {
					if strings.Contains(all, forbidden) {
						t.Fatal("冗余元数据仍进入实际请求", forbidden)
					}
				}
				for _, value := range []string{"2026-10-15 18:21:00（北京时间）", "场景：群聊", "当前发言人：同名成员", "18:20:30", "@被提醒成员", "@全体", "引用#1", "拍者拍了拍被拍者"} {
					if !strings.Contains(texts[1], value) {
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
	for _, input := range []string{wrapper, "```json\n" + wrapper + "\n```"} {
		body, err := ReplyText(input)
		if err != nil || body != "只发送这段正文" {
			t.Fatal("未提取正文", body, err)
		}
	}
	for _, input := range []string{"普通回复", `{"text":"用户要求的业务JSON","status":"sent"}`, `{"role":"user","actor_type":"assistant_ai","id":"fixture-id","session":"fixture-room","text":"不能改写他人JSON"}`} {
		body, err := ReplyText(input)
		if err != nil || body != input {
			t.Fatal("误改写普通回复或业务JSON", body, err)
		}
	}
	for _, input := range []string{strings.Replace(wrapper, `"只发送这段正文"`, `""`, 1), strings.Replace(wrapper, `"只发送这段正文"`, `42`, 1)} {
		if _, err := ReplyText(input); err == nil {
			t.Fatal("无有效正文的记录包装被接受")
		}
	}
	original := contextcleaner.Message{ID: "fixture-archive-id", Role: "assistant", Source: "ai", SenderID: "fixture-self", Session: "fixture-room", Text: wrapper}
	q := Request{Prompt: "现在的问题", Conversation: &Conversation{Current: contextcleaner.Message{ID: "fixture-current"}}, Context: []contextcleaner.Message{original}}
	turns := ConversationMessages(q)
	if len(turns) != 2 || !strings.Contains(turns[0].Text, "〔本人〕：只发送这段正文") || strings.Contains(turns[0].Text, "fixture-generated-id") || q.Context[0].Text != wrapper {
		t.Fatal("历史包装未拆除，或原归档被修改", turns)
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

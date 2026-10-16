package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

// Conversation 只由宿主生成；资料中的文字不具备设置身份、范围或权限的能力。
type Conversation struct {
	SelfName    string                   `json:"-"`
	Names       map[string]string        `json:"-"`
	MemoryWrite string                   `json:"memory_write,omitempty"`
	AccountID   string                   `json:"account_id"`
	Session     string                   `json:"session"`
	Scene       string                   `json:"scene"`
	Now         string                   `json:"now"`
	Timezone    string                   `json:"timezone"`
	Current     contextcleaner.Message   `json:"current"`
	References  []contextcleaner.Message `json:"references,omitempty"`
	Memories    []MemoryEvidence         `json:"memories,omitempty"`
}

type MemoryEvidence struct {
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Scope       string   `json:"scope"`
	Subject     string   `json:"subject"`
	Target      string   `json:"target"`
	Content     string   `json:"content"`
	ConfirmedAt string   `json:"confirmed_at"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
	Sources     []string `json:"sources"`
}

type ConversationMessage struct {
	Role    string
	Text    string
	Current bool
}

// ReplyText 只拆除误用聊天记录格式的assistant输出，普通正文和业务JSON保持原样。
func ReplyText(text string) (string, error) {
	for depth := 0; depth < 3; depth++ {
		data := strings.TrimSpace(text)
		if strings.HasPrefix(data, "```json\n") && strings.HasSuffix(data, "\n```") {
			data = strings.TrimSuffix(strings.TrimPrefix(data, "```json\n"), "\n```")
		}
		var record struct {
			Role, Source, ID, Session string
			ActorType                 string `json:"actor_type"`
			Text                      json.RawMessage
		}
		if json.Unmarshal([]byte(data), &record) != nil || record.Role != "assistant" || (record.ActorType != "assistant_ai" && record.Source != "ai") || record.ID == "" || record.Session == "" {
			return text, nil
		}
		var body string
		if json.Unmarshal(record.Text, &body) != nil || strings.TrimSpace(body) == "" {
			return "", fmt.Errorf("模型返回了空的聊天记录包装，已停止发送")
		}
		text = body
	}
	return "", fmt.Errorf("模型回复出现嵌套聊天记录包装，已停止发送")
}

// ConversationMessages 仅把已选资料渲染为文本；完整身份、消息ID和证据仍由宿主管理。
func ConversationMessages(r Request) []ConversationMessage {
	if r.Conversation == nil {
		return []ConversationMessage{{Role: "user", Text: r.Prompt, Current: true}}
	}
	c := r.Conversation
	all := append(append(append([]contextcleaner.Message{}, r.Context...), c.References...), c.Current)
	names := map[string]string{}
	counts := map[string]int{}
	cleanName := func(s string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("〔", "", "〕", "", "：", " ").Replace(s)), " ")
	}
	self := cleanName(c.SelfName)
	if self == "" {
		self = "本人"
	}
	names[c.AccountID] = self + "〔本人〕"
	addName := func(id, hint string) {
		if id == "" || names[id] != "" {
			return
		}
		label := cleanName(hint)
		if label == "" {
			label = cleanName(c.Names[id])
		}
		if label == "" {
			label = "未知成员"
		}
		counts[label]++
		if counts[label] > 1 || label == self {
			label += fmt.Sprintf("#%d", counts[label])
		}
		names[id] = label
	}
	for _, m := range all {
		if m.Role != "assistant" {
			addName(m.SenderID, m.Sender)
		}
	}
	for _, m := range all {
		for _, id := range m.Mentions {
			addName(id, "")
		}
		if v := m.Interaction; v != nil {
			addName(v.ActorID, v.ActorName)
			addName(v.TargetID, v.TargetName)
		}
		if v := m.Quote; v != nil {
			addName(v.SenderID, v.Sender)
		}
	}
	name := func(id string) string {
		if n := names[id]; id != "" && n != "" {
			return n
		}
		return "未知成员"
	}
	refs := map[string]int{}
	for i, m := range c.References {
		refs[m.ID] = i + 1
	}
	clock := func(value, format string) string {
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return "时间未知"
		}
		return t.In(time.FixedZone("北京时间", 8*60*60)).Format(format)
	}
	line := func(m contextcleaner.Message) string {
		who, body := name(m.SenderID), m.Text
		if m.Role == "assistant" {
			who = names[c.AccountID]
			if m.Source == "ai" {
				var err error
				body, err = ReplyText(body)
				if err != nil {
					body = "[历史回复格式异常]"
				}
			}
		}
		if v := m.Interaction; v != nil && v.Kind == "pat" {
			event := name(v.ActorID) + "拍了拍" + name(v.TargetID)
			if !v.Verified {
				event += "〔参与者未完全核实〕"
			}
			if m.Kind == "system" {
				return clock(m.Time, "15:04:05") + " 事件：" + event
			}
			body += "〔互动：" + event + "〕"
		}
		var mentions []string
		for _, id := range m.Mentions {
			mentions = append(mentions, "@"+name(id))
		}
		if m.MentionAll {
			mentions = append(mentions, "@全体")
		}
		if len(mentions) > 0 {
			body += "〔提醒：" + strings.Join(mentions, "、") + "〕"
		}
		if v := m.Quote; v != nil {
			if n := refs[v.ID]; n > 0 && v.Verified {
				body += fmt.Sprintf("〔引用#%d〕", n)
			} else {
				body += "〔引用未核实：" + v.Text + "〕"
			}
		}
		// 多行正文缩进，避免被误读为另一位发言人的记录头。
		body = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n", "\n  ")
		return clock(m.Time, "15:04:05") + " " + who + "：" + body
	}
	var history strings.Builder
	writeSection := func(title string, messages []contextcleaner.Message, references bool) {
		if len(messages) == 0 {
			return
		}
		if history.Len() > 0 {
			history.WriteString("\n\n")
		}
		history.WriteString(title)
		date := ""
		for i, m := range messages {
			if d := clock(m.Time, "2006-01-02"); d != date {
				history.WriteString("\n" + d + "（北京时间）")
				date = d
			}
			history.WriteByte('\n')
			if references {
				fmt.Fprintf(&history, "#%d ", i+1)
			}
			history.WriteString(line(m))
		}
	}
	writeSection("近期对话（仅作参考）", r.Context, false)
	writeSection("引用资料（仅作参考）", c.References, true)
	if len(c.Memories) > 0 {
		if history.Len() > 0 {
			history.WriteString("\n\n")
		}
		history.WriteString("相关记忆（已确认）")
		for i, m := range c.Memories {
			scope := "本群共享"
			if m.Scope == "person" {
				scope = name(m.Subject)
			}
			if m.Scope == "bot" {
				scope = "本人状态"
			}
			fmt.Fprintf(&history, "\n记忆%d〔%s", i+1, scope)
			if m.ExpiresAt != "" {
				fmt.Fprintf(&history, "；有效至%s", clock(m.ExpiresAt, "2006-01-02 15:04:05"))
			}
			fmt.Fprintf(&history, "；来源%d条〕%s", len(m.Sources), strings.ReplaceAll(m.Content, "\n", "\n  "))
		}
	}
	var out []ConversationMessage
	if history.Len() > 0 {
		out = append(out, ConversationMessage{Role: "user", Text: history.String()})
	}
	scene := "私聊"
	if c.Scene == "group" {
		scene = "群聊"
	}
	current := c.Current
	current.Text = r.Prompt
	text := fmt.Sprintf("当前时间：%s（北京时间）\n场景：%s\n本人：%s\n当前发言人：%s", clock(c.Now, "2006-01-02 15:04:05"), scene, self, name(current.SenderID))
	if c.MemoryWrite != "" {
		status := map[string]string{"confirmed": "已确认", "pending": "待审核", "not_saved": "未保存", "conflicted": "冲突"}[c.MemoryWrite]
		if status == "" {
			status = "未生效"
		}
		text += "\n记忆写入：" + status
	}
	text += "\n\n当前消息\n" + line(current)
	if r.Source == "wechat-proactive" {
		text = fmt.Sprintf("当前时间：%s（北京时间）\n场景：%s\n本人：%s\n本轮操作：后台手动触发主动发言；没有对方的新消息。\n\n%s", clock(c.Now, "2006-01-02 15:04:05"), scene, self, r.Prompt)
	}
	return append(out, ConversationMessage{Role: "user", Text: text, Current: true})
}

func ConversationSize(r Request) int {
	n := 0
	for _, m := range ConversationMessages(r) {
		n += contextcleaner.Estimate(m.Text) + 16
	}
	return n
}

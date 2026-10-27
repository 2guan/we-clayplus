package ai

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

// Conversation 只由宿主生成；资料中的文字不具备设置身份、范围或权限的能力。
type Conversation struct {
	RepliedAfterCurrent bool                     `json:"replied_after_current,omitempty"`
	SelfName            string                   `json:"-"`
	Names               map[string]string        `json:"-"`
	MemoryWrite         string                   `json:"memory_write,omitempty"`
	AccountID           string                   `json:"account_id"`
	Session             string                   `json:"session"`
	Scene               string                   `json:"scene"`
	Now                 string                   `json:"now"`
	Timezone            string                   `json:"timezone"`
	Pending             []contextcleaner.Message `json:"pending,omitempty"`
	Current             contextcleaner.Message   `json:"current"`
	References          []contextcleaner.Message `json:"references,omitempty"`
	Memories            []MemoryEvidence         `json:"memories,omitempty"`
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

// ReplyTargets 给本次实际可见的群友/联系人消息短编号，模型无需拿到微信 ID。
// 编号与真实 ID 绑定，裁剪历史不会令旧编号指向另一个人；碰撞时拒绝映射。
func ReplyTargets(r Request) map[string]contextcleaner.Message {
	targets := map[string]contextcleaner.Message{}
	if r.Source != "wechat" || r.PlainReply || r.Conversation == nil {
		return targets
	}
	seen := map[string]bool{}
	blocked := map[string]bool{}
	for _, messages := range [][]contextcleaner.Message{r.Context, r.Conversation.References, r.Conversation.Pending, {r.Conversation.Current}} {
		for _, m := range messages {
			if m.ID == "" || seen[m.ID] || m.Session != r.Target || m.Role == "assistant" {
				continue
			}
			switch m.Kind {
			case "text", "image", "audio", "video", "emoji", "location", "link", "app", "quote", "file", "transfer", "redpacket":
			default:
				continue
			}
			seen[m.ID] = true
			hash := sha256.Sum256([]byte(m.Session + "\x00" + m.ID))
			ref := fmt.Sprintf("m%x", hash[:4])
			if blocked[ref] {
				continue
			}
			if _, exists := targets[ref]; exists {
				delete(targets, ref)
				blocked[ref] = true
				continue
			}
			targets[ref] = m
		}
	}
	return targets
}

// ReplyText 拆除模型误加的 assistant 角色前缀和聊天记录包装；正文示例保持原样。
func ReplyText(text string) (string, error) {
	for depth := 0; depth < 3; depth++ {
		data := strings.TrimSpace(text)
		if strings.HasPrefix(strings.ToLower(data), "[assistant]") {
			text = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(data[len("[assistant]"):]), ":："))
			if text == "" {
				return "", fmt.Errorf("模型只返回了角色标签，已停止发送")
			}
			continue
		}
		for _, role := range []string{"[user]", "[system]", "[developer]"} {
			if strings.HasPrefix(strings.ToLower(data), role) {
				return "", fmt.Errorf("模型返回了其他角色的记录，已停止发送")
			}
		}
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
	all := append(append(append(append([]contextcleaner.Message{}, r.Context...), c.References...), c.Pending...), c.Current)
	names := map[string]string{}
	counts := map[string]int{}
	cleanName := func(s string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("〔", "", "〕", "", "：", " ").Replace(s)), " ")
	}
	knownLabels := map[string]bool{}
	for _, label := range c.Names {
		knownLabels[cleanName(label)] = true
	}
	self := cleanName(c.SelfName)
	if self == "" {
		self = "本人"
	}
	knownLabels[self] = true
	names[c.AccountID] = self + "〔本人〕"
	addName := func(id, hint string) {
		if id == "" || names[id] != "" {
			return
		}
		label := cleanName(c.Names[id])
		if label == "" {
			label = cleanName(hint)
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
	addresses := map[string]string{}
	for ref, m := range ReplyTargets(r) {
		addresses[m.ID] = ref
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
			if v.Verified && v.SenderID == c.AccountID {
				body += "〔引用本人消息〕"
			}
			if n := refs[v.ID]; n > 0 && v.Verified {
				body += fmt.Sprintf("〔引用#%d〕", n)
			} else {
				inBatch := false
				for _, original := range c.Pending {
					inBatch = inBatch || original.ID == v.ID
				}
				if v.Verified && inBatch {
					body += "〔引用本轮" + name(v.SenderID) + "的发言〕"
				} else {
					body += "〔引用未核实：" + v.Text + "〕"
				}
			}
		}
		// 多行正文缩进，避免被误读为另一位发言人的记录头。
		if m.Game != "" || m.Source == "plugin:turtlesoup" {
			label := "游戏过程·虚构"
			if m.Game == "playroom:group_digest" {
				label = "群聊摘要·附来源"
			}
			if m.Game == "playroom:group_memes" {
				label = "群梗·娱乐整理"
			}
			if m.Game == "turtlesoup" || m.Source == "plugin:turtlesoup" {
				label = "海龟汤游戏·虚构"
			}
			who += "〔" + label + "〕"
		}
		if m.Role != "assistant" {
			// 正文开头点名的是对话对象线索，不改变外层实际发言人，
			// 也不当作协议 @、工具授权或已经核实的收件人。
			address := ""
			for id, label := range names {
				candidate := cleanName(c.Names[id])
				if candidate == "" {
					candidate = strings.TrimSuffix(label, "〔本人〕")
				}
				tail, found := strings.CutPrefix(strings.TrimSpace(m.Text), candidate)
				if candidate != "" && found && tail != "" && strings.ContainsRune("，,", []rune(tail)[0]) && len(candidate) > len(address) {
					address = candidate
				}
			}
			if address != "" {
				who += "〔正文称呼：" + address + "；仅作指向线索〕"
			}
			prefix, _, found := strings.Cut(strings.TrimSpace(body), "：")
			if !found {
				prefix, _, found = strings.Cut(strings.TrimSpace(body), ":")
			}
			if found && knownLabels[strings.TrimSpace(prefix)] {
				who += "〔实际发言人；正文称呼不改变身份〕"
			}
		}
		body = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n", "\n  ")
		prefix := ""
		if ref := addresses[m.ID]; ref != "" {
			prefix = "〔" + ref + "〕"
		}
		return prefix + clock(m.Time, "15:04:05") + " " + who + "：" + body
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
	appendTurn := func(role, text string) {
		if text == "" {
			return
		}
		if len(out) > 0 && out[len(out)-1].Role == role {
			out[len(out)-1].Text += "\n\n" + text
			return
		}
		out = append(out, ConversationMessage{Role: role, Text: text})
	}
	if len(r.Context) > 0 {
		appendTurn("user", "近期对话（聊天记录，仅作参考；assistant 为本人已发送正文，不代表已核实的现实状态；明确的故事设定与任务进度仍用于接续）")
		date := ""
		for _, m := range r.Context {
			if m.Role == "assistant" {
				body := m.Text
				if m.Source == "ai" {
					if value, err := ReplyText(body); err == nil {
						body = value
					} else {
						continue
					}
					// 归档保留已发事实，但误发的沉默协议/回复规划不再作为
					// assistant 示范，避免模型继续模仿这些错误输出。
					if _, silent := contextcleaner.Output(contextcleaner.Config{Enabled: true, Silence: true}, body, true); silent || contextcleaner.ReplyDecision(body) || contextcleaner.ToolCallText(body) {
						continue
					}
				}
				if m.Game != "" || m.Source == "plugin:turtlesoup" {
					body = "〔游戏过程·虚构〕" + body
				}
				appendTurn("assistant", body)
				continue
			}
			text := line(m)
			if d := clock(m.Time, "2006-01-02"); d != date {
				text = d + "（北京时间）\n" + text
				date = d
			}
			appendTurn("user", text)
		}
	}
	if history.Len() > 0 {
		appendTurn("user", history.String())
	}

	scene := "私聊"
	if c.Scene == "group" {
		scene = "群聊"
	}
	current := c.Current
	current.Text = r.Prompt
	text := fmt.Sprintf("当前时间：%s（北京时间）\n场景：%s\n本人：%s\n当前发言人：%s", clock(c.Now, "2006-01-02 15:04:05"), scene, self, name(current.SenderID))
	text += "\n本人现实状态：除本轮已确认的本人状态记忆外未知。聊天调侃、他人猜测和本人旧回复不是吃饭、睡眠、位置或线下安排的事实依据，不据此续编。"
	text += "\n虚构故事与现实身份分开：续写时保留已有角色和剧情进度，不能用本人的现实人设替换故事角色，也不能因角色的第一人称而误判现实发言人。"
	if c.RepliedAfterCurrent {
		text += "\n时序提醒：这些来信之后本人已有后续发言（见 assistant 历史）。先核对是否已经回应；不要把较早来信当作刚收到的新提问，不换种说法重复回应。"
	}
	if c.MemoryWrite != "" {
		status := map[string]string{"confirmed": "已确认", "pending": "待审核", "not_saved": "未保存", "conflicted": "冲突"}[c.MemoryWrite]
		if status == "" {
			status = "未生效"
		}
		text += "\n记忆写入：" + status
	}
	if len(c.Pending) > 0 {
		text = strings.Replace(text, "当前发言人：", "末条发言人：", 1)
		text += "\n\n本轮待回复消息（按接收顺序合并，分别保留真实发言人）"
		for _, m := range c.Pending {
			text += "\n" + line(m)
		}
		text += "\n" + line(current)
	} else {
		text += "\n\n当前消息\n" + line(current)
	}
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

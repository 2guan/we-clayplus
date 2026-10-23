// 将模型选择的本轮消息短编号转换为当前会话的真实 @ 和引用参数。
package dashboard

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

// 普通模型常直接输出 @昵称。只解析段首连续的称呼，以本群实时成员表
// 精确映射；不能把正文中提到的 @、同名成员或其他会话联系人猜成接收人。
func plainReplyMentions(st State, members []RoomMember, text string) (actionRequest, error) {
	aliases := map[string][]string{}
	add := func(name, id string) {
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(aliases[name], id) {
			aliases[name] = append(aliases[name], id)
		}
	}
	for _, m := range members {
		add(m.Name, m.ID)
		add(m.DisplayName, m.ID)
		for _, c := range st.Contacts {
			if c.ID == m.ID {
				add(c.Name, m.ID)
				add(c.Remark, m.ID)
			}
		}
	}
	cmd := actionRequest{Group: "text"}
	prefix := ""
	for strings.HasPrefix(text, "@") {
		rest, name := strings.TrimPrefix(text, "@"), ""
		for alias := range aliases {
			if !strings.HasPrefix(rest, alias) {
				continue
			}
			tail := strings.TrimPrefix(rest, alias)
			if tail != "" {
				boundary := []rune(tail)[0]
				if !unicode.IsSpace(boundary) && !strings.ContainsRune("@，,：:。！？!?、", boundary) {
					continue
				}
			}
			if len(alias) > len(name) {
				name = alias
			}
		}
		ids := aliases[name]
		if name == "" || len(ids) != 1 {
			return cmd, fmt.Errorf("未发送文字 @：本群找不到唯一匹配成员，请明确群内昵称或使用消息短编号")
		}
		id := ids[0]
		if id == "" || id == st.Account.ID || id == "notify@all" || len(id) > 256 || strings.ContainsAny(id, ",&<>\r\n\t ") {
			return cmd, fmt.Errorf("未发送文字 @：目标不是可提醒的群成员")
		}
		if !slices.Contains(cmd.Mentions, id) {
			cmd.Mentions = append(cmd.Mentions, id)
		}
		if len(cmd.Mentions) > 50 {
			return cmd, fmt.Errorf("@ 成员数量超过上限")
		}
		prefix += "@" + name + "\u2005"
		text = strings.TrimSpace(strings.TrimPrefix(rest, name))
	}
	cmd.Content = prefix + text
	return cmd, nil
}

func (s *Server) preparePlainReplyMentions(ctx context.Context, st State, q *ai.Request, parts []replyPart) (map[int]actionRequest, error) {
	commands := map[int]actionRequest{}
	if q == nil || q.Source != "wechat" || q.Conversation == nil || q.Conversation.Scene != "group" || !strings.HasSuffix(q.Target, "@chatroom") {
		return commands, nil
	}
	for i, p := range parts {
		if p.Kind == "text" && p.ReplyRef == "" && strings.HasPrefix(p.Text, "@") {
			commands[i] = actionRequest{}
		}
	}
	if len(commands) == 0 {
		return commands, nil
	}
	if q.AccountID != st.Account.ID || q.AccountEpoch != st.AccountEpoch || q.Conversation.Session != q.Target {
		return nil, fmt.Errorf("账号或会话已变化，未发送 @ 回复")
	}
	if s.host == nil {
		return nil, fmt.Errorf("微信尚未就绪，无法核实 @ 成员")
	}
	result, err := s.host.Execute(ctx, control.Command{Operation: "room-info", Target: q.Target})
	if err != nil {
		return nil, fmt.Errorf("未发送文字 @：无法获取本群成员：%w", err)
	}
	raw, err := json.Marshal(result)
	var room RoomInfo
	if err != nil || json.Unmarshal(raw, &room) != nil || len(room.Members) == 0 {
		return nil, fmt.Errorf("未发送文字 @：本群成员资料无效")
	}
	for i := range commands {
		cmd, err := plainReplyMentions(st, room.Members, parts[i].Text)
		if err != nil {
			return nil, err
		}
		cmd.Target = q.Target
		commands[i] = cmd
	}
	return commands, nil
}

func parseAddressedReply(start xml.StartElement, raw, body string) (replyPart, error) {
	p := replyPart{Kind: "text", Text: strings.TrimSpace(body)}
	seen := map[string]bool{}
	for _, attr := range start.Attr {
		if attr.Name.Space != "" || seen[attr.Name.Local] {
			return p, fmt.Errorf("reply 标签属性无效")
		}
		seen[attr.Name.Local] = true
		switch attr.Name.Local {
		case "ref":
			p.ReplyRef = attr.Value
		case "mode":
			p.ReplyMode = attr.Value
		default:
			return p, fmt.Errorf("reply 只支持 ref 和 mode 属性")
		}
	}
	inner := raw[strings.IndexByte(raw, '>')+1:]
	inner = strings.TrimSuffix(inner, "</reply>")
	if p.ReplyRef == "" || p.Text == "" || strings.Contains(inner, "<") || (p.ReplyMode != "at" && p.ReplyMode != "quote" && p.ReplyMode != "both") {
		return p, fmt.Errorf("reply 需要有效 ref、at/quote/both 模式和非空纯文字正文，不允许嵌套标签")
	}
	return p, nil
}

func replyAddressCommand(st State, q *ai.Request, p replyPart) (actionRequest, error) {
	if q == nil || q.Source != "wechat" || q.PlainReply || q.Conversation == nil || q.AccountID != st.Account.ID || q.AccountEpoch != st.AccountEpoch || q.Conversation.Session != q.Target {
		return actionRequest{}, fmt.Errorf("本轮不支持定向回复，请改用纯文字")
	}
	source, ok := ai.ReplyTargets(*q)[p.ReplyRef]
	if !ok {
		return actionRequest{}, fmt.Errorf("回复目标不在本轮可见消息中，请改用纯文字")
	}
	var original *Message
	for i := range st.Messages {
		m := &st.Messages[i]
		if m.ID == source.ID && m.Target == q.Target {
			original = m
			break
		}
	}
	if original == nil || original.Self || !contextAvailable(*original) || q.EvidenceHashes[original.ID] != replyEvidenceHash(*original) {
		return actionRequest{}, fmt.Errorf("回复目标已撤回、删除或变化，请改用纯文字")
	}
	cmd := actionRequest{Target: q.Target, Content: p.Text, Group: "text"}
	if p.ReplyMode == "at" || p.ReplyMode == "both" {
		id := original.SenderID
		if q.Conversation.Scene != "group" || !strings.HasSuffix(q.Target, "@chatroom") || id == "" || id == q.Target || id == st.Account.ID || id == "notify@all" || len(id) > 256 || strings.ContainsAny(id, ",&<>\r\n\t ") {
			return cmd, fmt.Errorf("只能 @ 本群已核实的实际发言人，请改用纯文字")
		}
		cmd.Mentions = []string{id}
		name := q.Conversation.Names[id]
		if name == "" {
			name = original.Sender
		}
		name = strings.Join(strings.Fields(strings.TrimPrefix(name, "@")), " ")
		if name == "" {
			name = "群友"
		}
		if !strings.HasPrefix(cmd.Content, "@"+name+"\u2005") {
			cmd.Content = "@" + name + "\u2005" + cmd.Content
		}
	}
	if p.ReplyMode == "quote" || p.ReplyMode == "both" {
		cmd.QuoteID = original.ID
	}
	return cmd, nil
}

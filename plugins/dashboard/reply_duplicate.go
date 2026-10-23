// 普通群聊发送前过滤明确重复；直接 @、引用、主动发言和工具结果继续保留原语义。
package dashboard

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/we-clay/we-clay/plugins/ai"
)

var closingReply = regexp.MustCompile(`^(?:晚安|回见|再见|拜拜|好梦|睡个好觉|早点睡|赶紧睡|明天见|改天聊|下次聊|好|嗯|行|收到|你也是|真撤了|这就睡)+$`)

func onlyClosingReply(text string) bool {
	runes := replyFingerprint(text)
	return len(runes) > 0 && len(runes) <= 32 && closingReply.MatchString(string(runes))
}

// 只拦同一成员在本人已告别后再互道告别；其他成员、新问题、@/引用仍保留。
func repeatedClosing(st State, q ai.Request, text string) bool {
	id := q.Conversation.Current.SenderID
	if id == "" || !onlyClosingReply(q.Prompt) || !onlyClosingReply(text) {
		return false
	}
	for _, m := range q.Conversation.Pending {
		if m.SenderID != id || !onlyClosingReply(m.Text) {
			return false
		}
	}
	anchor := -1
	for i := len(st.Messages) - 1; i >= 0; i-- {
		if st.Messages[i].Target == q.Target && st.Messages[i].ID == q.Conversation.Current.ID {
			anchor = i
			break
		}
	}
	for i := anchor - 1; i >= 0; i-- {
		m := st.Messages[i]
		if m.Target != q.Target || !contextAvailable(m) {
			continue
		}
		if messageGame(m) != "" || m.Kind != "text" || !onlyClosingReply(m.Content) {
			return false
		}
		if !m.Self {
			if m.SenderID != id {
				return false
			}
			continue
		}
		stamp, err := time.Parse(time.RFC3339, m.Time)
		if err != nil || time.Since(stamp) > 10*time.Minute || time.Until(stamp) > 5*time.Second {
			return false
		}
		for j := i - 1; j >= 0; j-- {
			previous := st.Messages[j]
			if previous.Target == q.Target && !previous.Self && contextAvailable(previous) {
				return previous.SenderID == id
			}
		}
		return false
	}
	return false
}

func replyFingerprint(text string) []rune {
	return []rune(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text))
}

func sameRecentReply(a, b []rune) bool {
	if len(a) < 12 || len(b) < 12 {
		return false
	}
	if slices.Equal(a, b) {
		return true
	}
	// 只拦高相似的近似复读，长度悬殊、短答复和新增内容较多的不按语义猜测。
	if len(a) > 1200 || len(b) > 1200 || min(len(a), len(b))*100 < max(len(a), len(b))*80 {
		return false
	}
	for _, r := range append(slices.Clone(a), b...) {
		if unicode.IsNumber(r) {
			return false
		}
	}
	for _, neg := range []rune("不没无未非否") {
		if strings.Count(string(a), string(neg)) != strings.Count(string(b), string(neg)) {
			return false
		}
	}
	counts := map[[2]rune]int{}
	for i := 1; i < len(a); i++ {
		counts[[2]rune{a[i-1], a[i]}]++
	}
	shared := 0
	for i := 1; i < len(b); i++ {
		key := [2]rune{b[i-1], b[i]}
		if counts[key] > 0 {
			shared++
			counts[key]--
		}
	}
	return shared*200 >= (len(a)+len(b)-2)*92
}

func (s *Server) duplicateReplyReason(q ai.Request, text string) string {
	if q.Source != "wechat" || q.Kind != "chat" || q.Conversation == nil || q.Conversation.Scene != "group" || q.Conversation.Current.Kind == "quote" {
		return ""
	}
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
		return ""
	}
	if slices.Contains(q.Conversation.Current.Mentions, st.Account.ID) {
		return ""
	}
	for _, m := range q.Conversation.Pending {
		if slices.Contains(m.Mentions, st.Account.ID) {
			return ""
		}
	}
	parts, err := parseReply(text, replyLimit(st))
	if err != nil || len(parts) == 0 || slices.ContainsFunc(parts, func(p replyPart) bool { return p.Kind != "text" || p.ReplyRef != "" }) {
		return ""
	}
	if repeatedClosing(st, q, text) {
		return "程序检查：与同一成员已互道告别，本轮仅重复告别且没有新增问题，未再次发送"
	}
	if len(parts) != 1 {
		return ""
	}
	candidate := replyFingerprint(parts[0].Text)
	if len(candidate) < 12 {
		return ""
	}
	now := time.Now()
	checked := 0
	for i := len(st.Messages) - 1; i >= 0 && checked < 8; i-- {
		m := st.Messages[i]
		if m.Target != q.Target || !m.Self || m.Kind != "text" || m.Status != "sent" || m.Source == "guard" || messageGame(m) != "" {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, m.Time)
		if err != nil || now.Sub(stamp) > 3*time.Minute || stamp.After(now.Add(5*time.Second)) {
			continue
		}
		checked++
		body, err := ai.ReplyText(m.Content)
		samePeople := true
		for _, name := range q.Conversation.Names {
			if name != "" && strings.Contains(parts[0].Text, name) != strings.Contains(body, name) {
				samePeople = false
				break
			}
		}
		if err == nil && samePeople && sameRecentReply(candidate, replyFingerprint(body)) {
			return fmt.Sprintf("程序检查：与本人近期已发送消息 %s 重复或高度相似，本轮未再次发送", m.ID)
		}
	}
	return ""
}

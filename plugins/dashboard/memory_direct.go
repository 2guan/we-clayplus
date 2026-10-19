// 主要功能：仅将本人私聊明确提出的低敏偏好直接记住，其他内容进入审核。
package dashboard

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var memorySensitivePattern = regexp.MustCompile(`(?i)(密码|口令|验证码|身份证|护照|银行卡|信用卡|账户|账号|私钥|令牌|token|secret|api.?key|住址|地址|电话|手机|病|诊断|药|过敏|怀孕|生育|抑郁|宗教|政治|性取向|薪资|工资|收入|负债|[0-9]{5,}|https?://|@)`)

func memoryExplicitBody(text string) string {
	text = strings.TrimSpace(text)
	for _, p := range []string{"记住：", "记住，", "请记住：", "请记住，"} {
		if strings.HasPrefix(text, p) {
			return strings.TrimSpace(strings.TrimPrefix(text, p))
		}
	}
	return ""
}

func memoryLowPreference(body string) (category, key, content string, ok bool) {
	if body == "" || len([]rune(body)) > 80 || memorySensitivePattern.MatchString(body) {
		return
	}
	if strings.HasPrefix(body, "叫我") {
		for _, phrase := range []string{"记住", "机器人", "今天", "明天", "现在", "不要", "忽略", "规则", "状态", "然后", "并且"} {
			if strings.Contains(body, phrase) {
				return
			}
		}
		name := strings.TrimSpace(strings.TrimPrefix(body, "叫我"))
		if len([]rune(name)) < 1 || len([]rune(name)) > 12 {
			return
		}
		for _, r := range name {
			if !unicode.IsLetter(r) && r != '·' && r != ' ' {
				return
			}
		}
		return "name", "address", "称呼" + name, true
	}
	for _, v := range []string{"回复简洁一些", "回复详细一些", "请用简体中文回复", "请用繁体中文回复", "请用英文回复", "不要使用表情", "可以使用表情", "喜欢简洁回复", "喜欢详细回复"} {
		if body == v {
			key := "reply-detail"
			if strings.Contains(body, "文回复") {
				key = "reply-language"
			}
			if strings.Contains(body, "表情") {
				key = "reply-emoji"
			}
			return "communication", key, body, true
		}
	}
	for _, v := range []string{"不吃辣", "喜欢吃辣", "喜欢清淡", "不喝酒", "不喝咖啡", "不吃香菜", "喜欢甜食", "不喜欢甜食"} {
		if body == v {
			key = "diet:" + v
			if v == "不吃辣" || v == "喜欢吃辣" {
				key = "diet:spicy"
			}
			if v == "喜欢甜食" || v == "不喜欢甜食" {
				key = "diet:sweet"
			}
			return "preference", key, body, true
		}
	}
	return
}

func memoryScanEligible(st State, m Message) bool {
	if !memoryMessageAvailable(m) || m.Self || m.Source == "ai" || m.Source == "guard" || m.Source == "system" || (m.Kind != "text" && m.Kind != "quote") {
		return false
	}
	if memoryGroup(st, m.Target) && m.SenderID == m.Target {
		return false
	}
	return true
}

func (s *Server) rememberExplicit(m Message) error {
	st := s.store.snapshot()
	if !settingFlag(st, "memory.direct_enabled") || st.Overrides[m.Target]["memory_enabled"] != true || m.Self || m.Kind != "text" || m.Rich != nil || m.Media != "" || m.Source == "guard" || m.Source == "ai" || memoryGroup(st, m.Target) {
		return nil
	}
	friend := false
	for _, c := range st.Contacts {
		if c.ID == m.Target && c.Kind == "friend" {
			friend = true
			break
		}
	}
	if !friend {
		return nil
	}
	if m.SenderID == "" {
		m.SenderID = m.Target
	}
	if m.SenderID != m.Target || !memoryScanEligible(st, m) {
		return nil
	}
	body := memoryExplicitBody(m.Content)
	if body == "" {
		return nil
	}
	// 来源必须已由 Record 归档；不从外部参数创造已确认事实。
	if !memorySourceValid(st, memorySource(m)) {
		return nil
	}
	if memorySensitivePattern.MatchString(body) {
		return fmt.Errorf("敏感内容未保存，请在 AI 记忆页手动录入并明确同意保存")
	}
	category, key, content, low := memoryLowPreference(body)
	item := MemoryItem{Scope: "person", Subject: m.SenderID, Target: m.Target, Category: category, Key: key, Content: content, Status: "confirmed", Sensitivity: "low", Origin: "explicit", Sources: []MemorySource{memorySource(m)}}
	if !low {
		// 未确定的请求只生成待审候选；不把“记住”当作敏感保存或机器人状态授权。
		item.Origin = "scan"
		item.Status = "pending"
		item.Category = "requested-fact"
		item.Key = "request:" + memorySource(m).Hash
		item.Content = body
		item.ExpiresAt = memoryTemporaryExpiry(body, m.Time)
		if memorySensitivePattern.MatchString(body) {
			item.Sensitivity = "sensitive"
		}
	}
	s.store.mu.Lock()
	if s.store.accountEpoch != st.AccountEpoch || s.store.state.Account.ID != st.Account.ID {
		s.store.mu.Unlock()
		return fmt.Errorf("微信账号已切换，明确记忆未保存")
	}
	_, err := s.putMemoryLocked(st.Account.ID, item, "explicit-request:"+m.SenderID, 0)
	s.store.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// 相对时间事实必须受限；旧聊天不会因为今天重新抽取而获得新的有效期。
func memoryTemporaryExpiry(content, sourceTime string) string {
	temporary := false
	for _, word := range []string{"今天", "明天", "这周", "本周", "目前", "暂时", "现在", "这次", "最近"} {
		if strings.Contains(content, word) {
			temporary = true
			break
		}
	}
	if !temporary {
		return ""
	}
	base, err := time.Parse(time.RFC3339, sourceTime)
	if err != nil || base.After(time.Now()) {
		base = time.Now()
	}
	expires := base.Add(24 * time.Hour)
	if strings.Contains(content, "明天") {
		expires = base.Add(48 * time.Hour)
	}
	if strings.Contains(content, "本周") || strings.Contains(content, "这周") {
		expires = base.Add(7 * 24 * time.Hour)
	}
	return expires.UTC().Format(time.RFC3339)
}

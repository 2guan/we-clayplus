// 主要功能：从现有加密原文修复系统通知及旧版误归为文字的通话消息。
package dashboard

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Server) restoreNoticeArchive() error {
	st := s.store.snapshot()
	type update struct{ before, after Message }
	updates := map[string]update{}
	for _, m := range st.Messages {
		if strings.HasPrefix(m.Status, "revoked") || m.Status == "deleted" {
			continue
		}
		isSystem := m.Kind == "system" && strings.HasPrefix(m.Content, "[微信系统通知")
		isOldCall := m.Kind == "text" && strings.HasPrefix(strings.TrimSpace(m.Content), "<voipmsg")
		if !isSystem && !isOldCall {
			continue
		}
		next := m
		if isOldCall {
			// 只以 SDK 归档中的真实类型升级，用户粘贴的通话 XML 仍是普通文字。
			descriptor, err := s.store.getSecret(accountMessageMediaKey(st.Account.ID, m))
			var source struct {
				Type struct {
					Code int `json:"code"`
				} `json:"type"`
			}
			if err != nil || json.Unmarshal([]byte(descriptor), &source) != nil || source.Type.Code != 50 && source.Type.Code != 52 && source.Type.Code != 53 {
				continue
			}
			next.Kind = "call"
		}
		raw, err := s.store.getSecret("quote:" + accountMessageMediaKey(st.Account.ID, m))
		if err != nil {
			continue
		}
		next, keep := ParseWechatMessage(next, raw)
		if !keep || next.Content == m.Content {
			continue
		}
		next = resolveNotice(st, next)
		updates[m.Target+"\x00"+m.ID] = update{m, next}
	}
	if len(updates) == 0 {
		return nil
	}
	return s.store.change(func(v *State) error {
		if v.Account.ID != st.Account.ID || s.store.accountEpoch != st.AccountEpoch {
			return fmt.Errorf("恢复通知时账号已切换")
		}
		for i, m := range v.Messages {
			u, ok := updates[m.Target+"\x00"+m.ID]
			if !ok || m.Content != u.before.Content || m.Kind != u.before.Kind || m.Status != u.before.Status {
				continue
			}
			v.Messages[i].Kind, v.Messages[i].Content, v.Messages[i].Rich = u.after.Kind, u.after.Content, u.after.Rich
			for j, c := range v.Contacts {
				if c.ID == m.Target && c.Time == m.Time && c.Preview == m.Content {
					v.Contacts[j].Preview = u.after.Content
				}
			}
		}
		return nil
	})
}

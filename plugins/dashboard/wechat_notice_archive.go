// 主要功能：从现有加密原文修复通知、通话和旧版被撤回提示覆盖的原文。
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
		if m.Status == "deleted" {
			continue
		}
		if strings.HasPrefix(m.Status, "revoked") {
			if text, ok := s.revokedArchiveContent(st.Account.ID, m); ok && text != m.Content {
				next := m
				next.Content = text // 只恢复展示原文，不恢复状态、已清除的引用或模型可用性。
				updates[m.Target+"\x00"+m.ID] = update{m, next}
			}
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

func (s *Server) revokedArchiveContent(account string, m Message) (string, bool) {
	var deleted bool
	if err := s.store.db.QueryRow("SELECT EXISTS(SELECT 1 FROM memory_deleted_sources WHERE account=? AND target=? AND message_id=?)", account, m.Target, m.ID).Scan(&deleted); err != nil || deleted {
		return "", false
	}
	var hash string
	var body []byte
	var original Message
	err := s.store.db.QueryRow(`SELECT event_hash,body FROM memory_archive_events e WHERE account=? AND target=? AND message_id=? AND NOT EXISTS (SELECT 1 FROM memory_deleted_sources d WHERE d.account=e.account AND d.target=e.target AND d.message_id=e.message_id) ORDER BY seq DESC LIMIT 1`, account, m.Target, m.ID).Scan(&hash, &body)
	if err == nil && s.store.openMemory(account, "archive:"+m.Target+":"+m.ID+":"+hash, body, &original) == nil && original.ID == m.ID && original.Target == m.Target && contextAvailable(original) && original.Content != "" {
		return original.Content, true
	}
	// 更早版本可能只有加密引用原文；仅经过既有类型解析后取展示文字，
	// 不把媒体XML及其中的票据直接返回页面。
	raw, err := s.store.getSecret("quote:" + accountMessageMediaKey(account, m))
	if err != nil || raw == "" {
		return "", false
	}
	if m.Kind == "text" {
		return raw, true
	}
	original = m
	original.Content = ""
	original, keep := ParseWechatMessage(original, raw)
	return original.Content, keep && original.Content != ""
}

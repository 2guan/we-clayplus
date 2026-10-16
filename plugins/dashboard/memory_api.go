// 主要功能：提供仅管理员可用的记忆管理、来源和范围预览接口。
package dashboard

import (
	"fmt"
	"net/http"
	"strings"
)

type memoryChange struct {
	ID     string      `json:"id"`
	Time   string      `json:"time"`
	Actor  string      `json:"actor"`
	Action string      `json:"action"`
	Before *MemoryItem `json:"before,omitempty"`
	After  MemoryItem  `json:"after"`
}

func (s *Server) memoryHistory(account, id string) ([]memoryChange, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if account == "" || s.store.state.Account.ID != account {
		return nil, fmt.Errorf("微信账号已切换或未就绪")
	}
	rows, err := s.store.db.Query("SELECT id,time,body FROM memory_changes WHERE account=? AND item_id=? ORDER BY time DESC,id DESC LIMIT 200", account, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memoryChange{}
	for rows.Next() {
		var c memoryChange
		var raw []byte
		if err = rows.Scan(&c.ID, &c.Time, &raw); err != nil {
			return nil, err
		}
		if err = s.store.openMemory(account, c.ID, raw, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Server) memorySourceMessage(account, target, id string) (Message, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if account == "" || s.store.state.Account.ID != account {
		return Message{}, fmt.Errorf("微信账号已切换或未就绪")
	}
	for _, m := range s.store.state.Messages {
		if m.Target == target && m.ID == id && memoryMessageAvailable(m) {
			m.RevokeReceipt = nil
			return m, nil
		}
	}
	return Message{}, fmt.Errorf("来源消息已撤回、删除或不可用")
}
func (s *Server) memoryAPI(w http.ResponseWriter, r *http.Request, u User) {
	if err := requireAdmin(u); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	account := s.store.snapshot().Account.ID
	if r.Method == http.MethodGet {
		var result any
		var err error
		switch r.URL.Path {
		case "/api/memory":
			var items []MemoryItem
			items, err = s.memoryList(account, MemoryFilter{Target: r.URL.Query().Get("target"), Subject: r.URL.Query().Get("subject"), Scope: r.URL.Query().Get("scope"), Status: r.URL.Query().Get("status")})
			result = map[string]any{"items": items}
		case "/api/memory/source":
			var msg Message
			msg, err = s.memorySourceMessage(account, r.URL.Query().Get("target"), r.URL.Query().Get("id"))
			result = map[string]any{"message": msg}
		case "/api/memory/history":
			var changes []memoryChange
			changes, err = s.memoryHistory(account, r.URL.Query().Get("id"))
			result = map[string]any{"changes": changes}
		case "/api/memory/preview":
			var items []MemoryItem
			items, err = s.replyMemories(account, Message{Target: r.URL.Query().Get("target"), SenderID: r.URL.Query().Get("sender"), Content: r.URL.Query().Get("query")}, 8)
			result = map[string]any{"items": items, "preview": true, "description": "当前范围和主题可用记忆预览，不代表某次回复实际采用的依据"}
		default:
			writeError(w, 404, "接口不存在")
			return
		}
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if s.store.snapshot().Account.ID != account {
			writeError(w, 409, "微信账号已切换，请刷新")
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/memory" {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Action  string     `json:"action"`
		Item    MemoryItem `json:"item"`
		ID      string     `json:"id"`
		Version int        `json:"version"`
	}
	if !decode(w, r, &q) {
		return
	}
	var item MemoryItem
	var err error
	switch q.Action {
	case "save":
		// HTTP 无法指定机器来源或冒充联系人明确授权。
		if q.Item.Origin != "" && q.Item.Origin != "manual" {
			writeError(w, 400, "后台保存只接受人工来源")
			return
		}
		q.Item.Origin = "manual"
		q.Item.ID = strings.TrimSpace(q.Item.ID)
		version := q.Version
		if version == 0 {
			version = q.Item.Version
		}
		item, err = s.putMemory(account, q.Item, u.Name, version)
	case "confirm", "reject", "expire", "delete":
		item, err = s.memoryAction(account, q.ID, q.Action, q.Version, u.Name)
	default:
		err = fmt.Errorf("未知记忆操作")
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.notify()
	writeJSON(w, 200, map[string]any{"item": item})
}

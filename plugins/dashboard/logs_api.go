// 主要功能：仅在日志页面查询摘要，按需读取原文，保持管理员与账号隔离。
package dashboard

import (
	"net/http"
	"strings"
)

func (s *Server) logsAPI(w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.store.mu.RLock()
	account, epoch := s.store.state.Account.ID, s.store.accountEpoch
	s.store.mu.RUnlock()
	field := "substr(message,1,512)"
	id := r.URL.Query().Get("id")
	if id != "" || r.URL.Query().Get("full") == "1" {
		field = "message"
	}
	query := "SELECT id,time,level,component," + field + ",target FROM state_logs WHERE account=?"
	args := []any{account}
	if user.Role != "admin" {
		query += " AND component<>'debug'"
	}
	if id != "" {
		query += " AND id=?"
		args = append(args, id)
	}
	for _, key := range []string{"q", "target"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			if len(value) > 1000 {
				writeError(w, 400, "搜索内容过长")
				return
			}
			query += " AND (instr(lower(message),lower(?))>0 OR instr(lower(target),lower(?))>0 OR instr(lower(component),lower(?))>0)"
			args = append(args, value, value, value)
		}
	}
	query += " ORDER BY seq LIMIT 2000"
	rows, err := s.store.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "日志读取失败")
		return
	}
	items := []Record{}
	for rows.Next() {
		var item Record
		if err = rows.Scan(&item.ID, &item.Time, &item.Level, &item.Component, &item.Message, &item.Target); err != nil {
			break
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		writeError(w, 500, "日志读取失败")
		return
	}
	s.store.mu.RLock()
	valid := account == s.store.state.Account.ID && epoch == s.store.accountEpoch
	s.store.mu.RUnlock()
	if !valid {
		writeError(w, 409, "账号已切换，请重新加载日志")
		return
	}
	if id != "" {
		if len(items) != 1 {
			writeError(w, 404, "日志不存在或已清理")
			return
		}
		writeJSON(w, 200, items[0])
		return
	}
	writeJSON(w, 200, items)
}

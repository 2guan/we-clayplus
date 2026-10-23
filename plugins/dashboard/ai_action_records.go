// 文件名：ai_action_records.go
// 主要功能：持久化操作凭据、防止重复写入，并提供后台确认；进程中断不会自动重放操作。
package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

type aiActionRecord struct {
	Account      string              `json:"-"`
	ID           string              `json:"id"`
	Task         string              `json:"task"`
	Origin       string              `json:"origin"`
	Actor        string              `json:"actor"`
	Operation    string              `json:"operation"`
	Target       string              `json:"target"`
	Status       string              `json:"status"`
	Created      string              `json:"created"`
	Updated      string              `json:"updated"`
	Preview      map[string]any      `json:"preview"`
	ReferenceIDs map[string][]string `json:"reference_ids,omitempty"`
	Result       any                 `json:"result,omitempty"`
	Error        string              `json:"error,omitempty"`
}
type pendingAIAction struct {
	Command              control.Command
	SourceID, SourceHash string
	Sources              []Message
}

// 写操作必须绑定到本轮真实收到的微信消息。模型看到的历史、引用和工具结果
// 可以帮助查找对象，但不能代替当前发言人的身份与原始消息。
func (a *actionSession) currentWriteSource() (Message, error) {
	if a.q.Source != "wechat" || a.q.Conversation == nil || a.q.Conversation.Current.ID == "" || a.actor == "" || a.q.Conversation.Current.SenderID != a.actor {
		return Message{}, fmt.Errorf("缺少当前微信原始消息，不能执行写操作")
	}
	st := a.s.store.snapshot()
	if st.Account.ID != a.q.AccountID || st.AccountEpoch != a.q.AccountEpoch {
		return Message{}, fmt.Errorf("微信账号已切换，不能执行写操作")
	}
	for _, m := range st.Messages {
		if m.ID != a.q.Conversation.Current.ID || m.Target != a.q.Target {
			continue
		}
		if m.Self || m.Source != "wechat" || m.Status != "sent" || m.SenderID != a.actor || !contextAvailable(m) || a.q.EvidenceHashes[m.ID] != replyEvidenceHash(m) {
			return Message{}, fmt.Errorf("当前微信原始消息已变化或发起人不符，不能执行写操作")
		}
		return m, nil
	}
	return Message{}, fmt.Errorf("当前微信原始消息不存在，不能执行写操作")
}

func (s *Server) initAIActions() error {
	_, e := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS ai_action_records(account TEXT NOT NULL,id TEXT NOT NULL,task TEXT NOT NULL,origin TEXT NOT NULL,fingerprint TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,id),UNIQUE(account,task,fingerprint))`)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec(`UPDATE ai_action_records SET body=json_set(CAST(body AS TEXT),'$.status','unknown','$.error','服务中断，操作结果未知，未自动重放') WHERE json_extract(CAST(body AS TEXT),'$.status')='running'`)
	return e
}
func (s *Server) actionRecords(origin, task string) ([]aiActionRecord, error) {
	s.store.mu.RLock()
	account := s.store.state.Account.ID
	s.store.mu.RUnlock()
	query := "SELECT body FROM ai_action_records WHERE account=?"
	args := []any{account}
	if origin != "" {
		query += " AND origin=?"
		args = append(args, origin)
	}
	if task != "" {
		query += " AND task=?"
		args = append(args, task)
	}
	query += " ORDER BY rowid DESC LIMIT 100"
	rows, e := s.store.db.Query(query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []aiActionRecord{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var r aiActionRecord
		if e = json.Unmarshal(raw, &r); e != nil {
			return nil, e
		}
		r.Account = account
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Server) saveActionRecord(r aiActionRecord) error {
	r.Updated = time.Now().Format(time.RFC3339)
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("UPDATE ai_action_records SET body=? WHERE account=? AND id=?", raw, r.Account, r.ID)
	s.notify()
	return e
}
func (a *actionSession) preview(cmd control.Command, args map[string]any) map[string]any {
	out := map[string]any{"content": cmd.Content, "target": cmd.Target}
	st := a.s.store.snapshot()
	for _, c := range st.Contacts {
		if c.ID == cmd.Target {
			out["target"] = c.Name
		}
	}
	for key, v := range args {
		if key == "target_ref" || key == "content" {
			continue
		}
		if strings.HasSuffix(key, "_refs") {
			names := []string{}
			for _, id := range argRefs(args, key) {
				r := a.refs[id]
				name := r.Name
				if name == "" {
					name = r.ID
				}
				names = append(names, name)
			}
			out[key] = names
		} else if strings.HasSuffix(key, "_ref") {
			r := a.refs[argText(args, key)]
			name := r.Name
			if name == "" {
				name = r.ID
			}
			out[key] = name
		} else {
			out[key] = v
		}
	}
	return out
}

// 审批时将同名对象的真实微信 ID 与预览名称按原顺序对应展示。
// 只导出人员和目标引用的 ID；申请票据、媒体凭据和内部请求引用均不进入操作记录或 API。
func (a *actionSession) previewReferenceIDs(args map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, key := range []string{"target_ref", "person_ref", "member_refs", "with_refs", "blacklist_refs"} {
		refs := []string{}
		if strings.HasSuffix(key, "_refs") {
			refs = argRefs(args, key)
		} else if id := argText(args, key); id != "" {
			refs = append(refs, id)
		}
		ids := make([]string, 0, len(refs))
		valid := true
		for _, id := range refs {
			r, ok := a.refs[id]
			if !ok || r.ID == "" || r.Kind != "contact" && r.Kind != "group" && r.Kind != "member" && r.Kind != "search" {
				valid = false
				break
			}
			ids = append(ids, r.ID)
		}
		if valid && len(ids) > 0 {
			out[key] = ids
		}
	}
	return out
}

// 写入声明和审批由 SQLite 唯一约束/CAS 仲裁，不在微信网络请求期间持有全局锁。
func (a *actionSession) write(ctx context.Context, op string, cmd control.Command, args map[string]any) (any, error) {
	if e := a.authorize(op, cmd.Target); e != nil {
		return nil, e
	}
	if e := a.live(); e != nil {
		return nil, e
	}
	source, e := a.currentWriteSource()
	if e != nil {
		return nil, e
	}
	// 这三项资料写入沿用当前原始消息授权边界，不能由历史、引用或模型推断的新值触发。
	if slices.Contains([]string{"contact.SetRemark", "chatroom.SetName", "chatroom.SetAnnouncement"}, op) && !a.s.currentWriteIntent(a.q, op, cmd.Content) {
		return nil, writeIntentMissing{}
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	sources, err := a.actionSources(args)
	if err != nil {
		return nil, err
	}
	st := a.s.store.snapshot()
	now := time.Now().Format(time.RFC3339)
	r := aiActionRecord{Account: st.Account.ID, ID: randomID(), Task: a.task, Origin: a.q.Target, Actor: a.actor, Operation: op, Target: cmd.Target, Status: "running", Created: now, Updated: now, Preview: a.preview(cmd, args), ReferenceIDs: a.previewReferenceIDs(args)}
	p := actionPolicy(st, op)
	// 引用、图片和语音中的内容不能在关闭人工确认后直接驱动外部写入。
	if !p.Confirm && !control.AIActions[op].Legacy && source.Kind != "text" {
		return nil, fmt.Errorf("当前消息不是纯文字；请在后台启用确认，或用新的纯文字消息明确提出操作")
	}
	if p.Confirm {
		r.Status = "pending"
	}
	encoded, _ := json.Marshal(cmd)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	body, _ := json.Marshal(r)
	a.writeStarted = true // 包括待确认和结果未知的操作；失败后不能整轮重放。
	_, e = a.s.store.db.Exec("INSERT INTO ai_action_records(account,id,task,origin,fingerprint,body) VALUES(?,?,?,?,?,?)", st.Account.ID, r.ID, r.Task, r.Origin, fingerprint, body)
	if e != nil {
		var previous []byte
		if queryErr := a.s.store.db.QueryRow("SELECT body FROM ai_action_records WHERE account=? AND task=? AND fingerprint=?", st.Account.ID, a.task, fingerprint).Scan(&previous); queryErr == nil {
			var old aiActionRecord
			if json.Unmarshal(previous, &old) == nil && old.Status == "succeeded" {
				return map[string]any{"status": "already_succeeded", "operation_id": old.ID, "result": old.Result}, nil
			}
			return nil, fmt.Errorf("相同操作已提交，当前状态不允许再次执行")
		}
		return nil, e
	}
	if p.Confirm {
		pending := pendingAIAction{Command: cmd, SourceID: source.ID, SourceHash: replyEvidenceHash(source), Sources: sources}
		raw, _ := json.Marshal(pending)
		e = a.s.store.putSecret("ai-pending:"+st.Account.ID+":"+r.ID, string(raw))
		if e != nil {
			r.Status = "failed"
			r.Error = "待确认命令保存失败"
			_ = a.s.saveActionRecord(r)
			return nil, e
		}
		a.pending = true
		a.s.notify()
		return map[string]any{"status": "pending_confirmation", "operation_id": r.ID, "notice": "尚未执行，请在模型中心的行为与任务中确认"}, nil
	}
	return a.runWrite(ctx, &r, cmd)
}
func (a *actionSession) runWrite(ctx context.Context, r *aiActionRecord, cmd control.Command) (any, error) {
	st := a.s.store.snapshot()
	var v any
	var e error
	if r.Operation == "contact.VerifyFriend" {
		var p control.ActionParams
		_ = json.Unmarshal(cmd.Params, &p)
		claimed, err := a.s.claimBusiness(st.Account.ID, "friend:"+p.V1+":"+p.V2)
		if err != nil || !claimed {
			r.Status = "failed"
			r.Error = "此申请正在或已经处理"
			_ = a.s.saveActionRecord(*r)
			return nil, fmt.Errorf("此申请正在或已经处理")
		}
	}
	if a.s.cfg.Mock {
		switch r.Operation {
		case "message.Send", "message.Forward":
			v = map[string]string{"id": "mock-" + r.ID}
		case "chatroom.Create":
			v = map[string]any{"id": "mock-created-" + r.ID + "@chatroom", "name": "示例新群"}
		case "label.Add":
			v = map[string]any{"id": 1, "name": cmd.Content}
		case "moments.Post":
			v = Moment{ID: "mock-" + r.ID, Content: cmd.Content, AuthorID: st.Account.ID}
		default:
			v = map[string]any{"mock": true, "operation": r.Operation}
		}
	} else if a.s.host == nil {
		e = fmt.Errorf("微信尚未就绪")
	} else {
		v, e = a.s.host.Execute(ctx, cmd)
	}
	if e == nil && v == nil && r.Operation != "contact.SetRemark" {
		e = fmt.Errorf("操作回执缺失，结果未知")
	}
	if e == nil {
		e = checkActionReceipt(v)
	}
	if current := a.s.store.snapshot(); current.Account.ID != st.Account.ID || current.AccountEpoch != st.AccountEpoch {
		e = fmt.Errorf("操作过程中账号已变化，结果未写入新账号")
	}

	var result any
	if e == nil {
		// 原始回执仅加密保存在本地；模型只收到最少的适用字段。
		raw, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			e = marshalErr
		} else {
			e = a.s.store.putSecret("ai-receipt:"+st.Account.ID+":"+r.ID, string(raw))
		}
		if e == nil {
			result, e = a.present(r.Operation, cmd, v)
		}
	}
	if e == nil {
		e = a.project(r.Operation, cmd, v)
	}
	if e != nil {
		r.Status = "unknown"
		r.Error = e.Error()
	} else {
		r.Status = "succeeded"
		if control.AIActions[r.Operation].Legacy {
			a.verifiedOperation = true
		}
		r.Result = result
	}
	saveErr := a.s.saveActionRecord(*r)
	if saveErr != nil {
		return nil, fmt.Errorf("操作回执保存失败，请核对微信，不要重复执行：%w", saveErr)
	}
	_ = a.s.store.change(func(s *State) error {
		level := "INFO"
		if e != nil {
			level = "ERROR"
		}
		addLog(s, level, "ai-action", r.Operation+"："+r.Status+"；操作 "+r.ID)
		s.Logs[len(s.Logs)-1].Target = r.Origin
		return nil
	})
	if e != nil {
		return nil, fmt.Errorf("操作未确认完成；此前成功步骤仍保留，不要重复执行：%w", e)
	}
	return map[string]any{"status": "succeeded", "operation_id": r.ID, "result": result}, nil
}
func (a *actionSession) project(op string, c control.Command, result any) error {
	st := a.s.store.snapshot()
	if op == "message.Send" || op == "message.Forward" {
		raw, _ := json.Marshal(result)
		var receipt map[string]string
		_ = json.Unmarshal(raw, &receipt)
		id := receipt["id"]
		if id == "" && !a.s.cfg.Mock {
			return fmt.Errorf("缺少真实发送消息ID")
		}
		if id == "" {
			id = "mock-" + randomID()
		}
		m := Message{ID: id, Target: c.Target, Sender: st.Account.Name, SenderID: st.Account.ID, Self: true, Kind: "text", Content: c.Content, Source: "ai", Status: "sent", Time: time.Now().Format(time.RFC3339)}
		var p control.ActionParams
		_ = json.Unmarshal(c.Params, &p)
		if p.Kind != "" {
			m.Kind = p.Kind
		}
		m.Mentions = p.Members
		if p.Kind == "link" {
			m.Articles = []MessageArticle{{Title: c.Content, URL: p.URL}}
		}
		if p.Kind == "location" {
			lat, lon := float64(p.Latitude), float64(p.Longitude)
			m.Rich = &RichMessage{Title: c.Content, Latitude: &lat, Longitude: &lon}
		}
		if receipt["account"] == st.Account.ID && receipt["create_time"] != "" && (receipt["client_id"] != "" || receipt["original_client_id"] != "") {
			m.RevokeReceipt = &control.RevokeReceipt{Account: receipt["account"], Created: receipt["create_time"], ClientID: receipt["client_id"], OriginalClientID: receipt["original_client_id"]}
		}
		if len(p.Media) > 0 {
			var e error
			m.Media, e = a.s.saveMedia(p.Media, http.DetectContentType(p.Media))
			if e != nil {
				return e
			}
		}
		if e := a.s.Record(m); e != nil {
			return e
		}
		if e := a.s.store.change(func(st *State) error {
			if st.Account.ID != m.SenderID {
				return fmt.Errorf("账号已切换")
			}
			for i := range st.Messages {
				saved := &st.Messages[i]
				if saved.ID == m.ID && saved.Target == m.Target {
					saved.Self = true
					saved.Source = "ai"
					saved.SenderID = m.SenderID
					if m.RevokeReceipt != nil {
						saved.RevokeReceipt = m.RevokeReceipt
					}
					if m.Media != "" {
						saved.Media = m.Media
					}
					saved.Mentions = m.Mentions
				}
			}
			return nil
		}); e != nil {
			return e
		}
		a.sentCurrent = a.sentCurrent || c.Target == a.q.Target
	}
	if op == "contact.VerifyFriend" { // 已受理申请不再显示为待确认。
		var p control.ActionParams
		_ = json.Unmarshal(c.Params, &p)
		_, _ = a.s.store.db.Exec("DELETE FROM secrets WHERE id=?", "ai-friend:"+st.Account.ID+":"+fmt.Sprintf("%x", sha256.Sum256([]byte(p.V1+"\x00"+p.V2))))
	}
	return a.s.store.change(func(s *State) error {
		if s.Account.ID != st.Account.ID {
			return fmt.Errorf("账号已切换，停止更新")
		}
		for i := range s.Contacts {
			if s.Contacts[i].ID == c.Target {
				if op == "contact.SetRemark" {
					s.Contacts[i].Remark = c.Content
				}
				if op == "chatroom.SetName" {
					s.Contacts[i].Name = c.Content
				}
			}
		}
		if op == "message.Revoke" {
			for i := range s.Messages {
				if s.Messages[i].ID == c.ID && s.Messages[i].Target == c.Target {
					s.Messages[i].Status = "revoked"
				}
			}
		}
		if op == "chatroom.Create" {
			raw, _ := json.Marshal(result)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			id, _ := m["id"].(string)
			name, _ := m["name"].(string)
			if name == "" {
				name = id
			}
			found := false
			for _, c := range s.Contacts {
				found = found || c.ID == id
			}
			if !found {
				s.Contacts = append(s.Contacts, Contact{ID: id, Name: name, Kind: "group", Mode: "manual"})
			}
		}
		return nil
	})
}
func (s *Server) aiActionsAPI(w http.ResponseWriter, r *http.Request, u User) {
	if u.Role != "admin" {
		writeError(w, 403, "仅管理员可查看和确认微信操作")
		return
	}
	if r.Method == http.MethodGet {
		rows, e := s.actionRecords("", "")
		if e != nil {
			writeError(w, 500, "读取操作记录失败")
			return
		}
		writeJSON(w, 200, rows)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if !decode(w, r, &q) {
		return
	}
	st := s.store.snapshot()
	var raw []byte
	if s.store.db.QueryRow("SELECT body FROM ai_action_records WHERE account=? AND id=?", st.Account.ID, q.ID).Scan(&raw) != nil {
		writeError(w, 404, "操作不存在")
		return
	}
	var item aiActionRecord
	_ = json.Unmarshal(raw, &item)
	item.Account = st.Account.ID
	if item.Status != "pending" {
		writeError(w, 409, "此操作已处理，不能再次执行")
		return
	}
	if !q.Approve {
		item.Status = "rejected"
		if e := s.claimPendingAction(item); e != nil {
			writeError(w, 409, e.Error())
			return
		}
		writeJSON(w, 200, item)
		return
	}
	secret, e := s.store.getSecret("ai-pending:" + st.Account.ID + ":" + item.ID)
	var pending pendingAIAction
	if e != nil || json.Unmarshal([]byte(secret), &pending) != nil {
		writeError(w, 409, "待确认命令不可用")
		return
	}
	created, _ := time.Parse(time.RFC3339, item.Created)
	if time.Since(created) > 24*time.Hour {
		writeError(w, 409, "操作超过24小时，请重新发起")
		return
	}
	a := newActionSession(s, item.Task, ai.Request{Source: "wechat", Target: item.Origin, AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch})
	a.actor = item.Actor
	validSource := false
	for _, m := range st.Messages {
		if m.Target == item.Origin && m.ID == pending.SourceID && m.SenderID == item.Actor && !m.Self && m.Source == "wechat" && m.Status == "sent" && contextAvailable(m) && pending.SourceHash != "" && replyEvidenceHash(m) == pending.SourceHash {
			validSource = true
			break
		}
	}
	if !validSource {
		writeError(w, 409, "原始请求已变化、撤回或发起人不符，请重新发起")
		return
	}
	for _, source := range pending.Sources {
		valid := false
		for _, m := range st.Messages {
			if m.ID == source.ID && m.Target == source.Target && m.Status == "sent" {
				valid = true
			}
		}
		if !valid {
			writeError(w, 409, "操作所引用的消息已删除或撤回")
			return
		}
	}
	if e = a.authorize(item.Operation, item.Target); e != nil {
		writeError(w, 403, e.Error())
		return
	}
	item.Status = "running"
	if e = s.claimPendingAction(item); e != nil {
		writeError(w, 409, e.Error())
		return
	}
	_, e = a.runWrite(r.Context(), &item, pending.Command)
	if e != nil {
		writeError(w, 409, e.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (a *actionSession) actionSources(args map[string]any) ([]Message, error) {
	ids := argRefs(args, "media_refs")
	for _, key := range []string{"message_ref", "media_ref"} {
		if id := argText(args, key); id != "" {
			ids = append(ids, id)
		}
	}
	out := []Message{}
	for _, id := range ids {
		if ref := a.refs[id]; ref.Kind == "asset" {
			continue
		}
		m, e := a.messageRef(id)
		if e != nil {
			return nil, e
		}
		out = append(out, Message{ID: m.ID, Target: m.Target})
	}
	return out, nil
}
func (s *Server) claimPendingAction(item aiActionRecord) error {
	item.Updated = time.Now().Format(time.RFC3339)
	raw, e := json.Marshal(item)
	if e != nil {
		return e
	}
	result, e := s.store.db.Exec("UPDATE ai_action_records SET body=? WHERE account=? AND id=? AND json_extract(CAST(body AS TEXT),'$.status')='pending'", raw, item.Account, item.ID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("此操作已处理，不可再次执行")
	}
	s.notify()
	return nil
}

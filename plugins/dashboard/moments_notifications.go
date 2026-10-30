package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

type MomentNotification struct {
	CommentID   int32  `json:"comment_id,omitempty"`
	ReplyToSelf bool   `json:"reply_to_self,omitempty"`
	ID          string `json:"id"`
	MomentID    string `json:"moment_id"`
	ActorID     string `json:"actor_id"`
	Actor       string `json:"actor"`
	Kind        string `json:"kind"`
	Created     int64  `json:"created"`
	Content     string `json:"content"`
	Original    string `json:"original,omitempty"`
	ReplyTo     string `json:"reply_to,omitempty"`
	Read        bool   `json:"read"`
}
type MomentNotificationSync struct {
	Key    []byte               `json:"key,omitempty"`
	Items  []MomentNotification `json:"items"`
	Notice string               `json:"notice,omitempty"`
}
type momentNotificationState struct {
	Key        []byte               `json:"key,omitempty"`
	ReadBefore int64                `json:"read_before"`
	Items      []MomentNotification `json:"items"`
}

func momentNotificationKey(account string) string { return "moments-notifications:" + account }
func (s *Server) loadMomentNotifications(account string) (momentNotificationState, error) {
	v := momentNotificationState{}
	raw, err := s.store.getSecret(momentNotificationKey(account))
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (s *Server) saveMomentNotifications(account string, epoch uint64, v momentNotificationState) error {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.Account.ID != account || s.store.accountEpoch != epoch {
		return fmt.Errorf("账号已切换，通知结果已丢弃")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.putSecret(momentNotificationKey(account), string(raw))
}
func notificationUnread(v momentNotificationState) uint32 {
	var n uint32
	for _, item := range v.Items {
		if !item.Read && item.Created > v.ReadBefore {
			n++
		}
	}
	return n
}
func (s *Server) momentNotificationAccount() (string, uint64) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.state.Account.ID, s.store.accountEpoch
}

func (s *Server) syncMomentNotifications(ctx context.Context) (momentNotificationState, string, error) {
	s.momentNotificationMu.Lock()
	defer s.momentNotificationMu.Unlock()
	account, epoch := s.momentNotificationAccount()
	if account == "" {
		return momentNotificationState{}, "", fmt.Errorf("微信账号尚未就绪")
	}
	v, err := s.loadMomentNotifications(account)
	if err != nil {
		return v, "", err
	}
	if s.cfg.Mock {
		return v, "", nil
	}
	if s.host == nil {
		return v, "", fmt.Errorf("微信尚未就绪")
	}
	if s.momentNotificationAccountID == account && s.momentNotificationEpoch == epoch && time.Since(s.momentNotificationChecked) < 15*time.Second {
		return v, "", nil
	}
	params, _ := json.Marshal(map[string]any{"key": v.Key})
	raw, err := s.host.Execute(ctx, control.Command{Operation: "moments-notification-sync", Params: params})
	if err != nil {
		return v, "", err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return v, "", err
	}
	var page MomentNotificationSync
	if err = json.Unmarshal(data, &page); err != nil {
		return v, "", fmt.Errorf("通知同步回执无效")
	}
	if page.Items == nil {
		return v, "", fmt.Errorf("通知同步回执缺少事件列表")
	}
	seen := map[string]bool{}
	for _, item := range v.Items {
		seen[item.ID] = true
	}
	for _, item := range page.Items {
		if item.ID == "" || item.MomentID == "" || item.Created <= 0 {
			return v, "", fmt.Errorf("通知缺少真实身份或时间")
		}
		if !seen[item.ID] {
			item.Read = false
			v.Items = append(v.Items, item)
			seen[item.ID] = true
		}
	}
	if len(page.Key) > 0 {
		v.Key = page.Key
	}
	sort.SliceStable(v.Items, func(i, j int) bool { return v.Items[i].Created > v.Items[j].Created })
	if err = s.saveMomentNotifications(account, epoch, v); err != nil {
		return v, "", err
	}
	s.momentNotificationAccountID, s.momentNotificationEpoch, s.momentNotificationChecked = account, epoch, time.Now()
	return v, page.Notice, nil
}

// RecordMomentStatus 只接收可信系统事件或本人设备的51号状态事件；聊天粘贴XML无效。
func (s *Server) RecordMomentStatus(m Message, raw string) (bool, error) {
	account, epoch := s.momentNotificationAccount()
	trustedSystem := m.Kind == "system" && !m.Self && m.Target == "weixin"
	trustedDevice := m.Kind == "status" && m.Self && m.SenderID == account && m.Target == account && account != ""
	if m.Source != "wechat" || (!trustedSystem && !trustedDevice) {
		return false, nil
	}
	var status struct {
		XMLName xml.Name `xml:"msg"`
		Op      struct {
			Name string `xml:"name"`
			Arg  string `xml:"arg"`
		} `xml:"op"`
	}
	if xml.Unmarshal([]byte(raw), &status) != nil || status.Op.Name != "MomentsUnreadMsgStatus" {
		return false, nil
	}
	read, err := strconv.ParseInt(strings.TrimSpace(status.Op.Arg), 10, 64)
	if err != nil || read < 0 || read > time.Now().Unix()+86400 {
		return true, nil // 未识别的可选状态不阻塞整条微信消息流，也不清空提醒。
	}
	s.momentNotificationMu.Lock()
	defer s.momentNotificationMu.Unlock()
	v, err := s.loadMomentNotifications(account)
	if err != nil {
		return true, err
	}
	if read > v.ReadBefore {
		v.ReadBefore = read
		err = s.saveMomentNotifications(account, epoch, v)
	}
	return true, err
}

func (s *Server) momentNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost && r.URL.Path == "/api/moments/notifications/read" {
		var q struct {
			IDs []string `json:"ids"`
		}
		if !decode(w, r, &q) {
			return
		}
		if len(q.IDs) > 1000 {
			writeError(w, 400, "一次最多标记1000条提醒")
			return
		}
		s.momentNotificationMu.Lock()
		defer s.momentNotificationMu.Unlock()
		account, epoch := s.momentNotificationAccount()
		v, err := s.loadMomentNotifications(account)
		if err != nil {
			writeError(w, 500, "通知状态读取失败")
			return
		}
		ids := map[string]bool{}
		for _, id := range q.IDs {
			ids[id] = true
		}
		for i := range v.Items {
			if ids[v.Items[i].ID] {
				v.Items[i].Read = true
			}
		}
		if err = s.saveMomentNotifications(account, epoch, v); err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]uint32{"unread": notificationUnread(v)})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	account, epoch := s.momentNotificationAccount()
	v, notice, err := s.syncMomentNotifications(r.Context())
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if r.URL.Path == "/api/moments/unread" {
		writeJSON(w, 200, map[string]uint32{"unread": notificationUnread(v)})
		return
	}
	if current, generation := s.momentNotificationAccount(); current != account || generation != epoch {
		writeError(w, 409, "账号已切换，请重新打开提醒")
		return
	}
	if id := r.URL.Query().Get("id"); id != "" {
		for _, item := range v.Items {
			if item.ID != id {
				continue
			}
			var m Moment
			if s.cfg.Mock {
				for _, candidate := range s.store.snapshot().Moments {
					if candidate.ID == item.MomentID {
						m = candidate
						break
					}
				}
			} else {
				raw, e := s.host.Execute(r.Context(), control.Command{Operation: "ai-action", ID: "moments.Detail", Content: item.MomentID, Params: json.RawMessage(`{}`)})
				if e != nil {
					writeError(w, 502, e.Error())
					return
				}
				body, e := json.Marshal(raw)
				var result struct {
					Moment Moment `json:"moment"`
				}
				if e == nil && json.Unmarshal(body, &result) == nil {
					m = result.Moment
				}
			}
			if current, generation := s.momentNotificationAccount(); current != account || generation != epoch {
				writeError(w, 409, "账号已切换，请重新打开提醒")
				return
			}
			if m.ID != item.MomentID {
				writeError(w, 404, "关联动态暂不可读，提醒仍保持未读")
				return
			}
			writeJSON(w, 200, m)
			return
		}
		writeError(w, 404, "提醒不存在或不属于当前账号")
		return
	}
	page := MomentPage{Unread: notificationUnread(v), Items: []Moment{}, Notice: notice}
	for _, item := range v.Items {
		item.Read = item.Read || item.Created <= v.ReadBefore
		page.Notifications = append(page.Notifications, item)
	}
	writeJSON(w, 200, page)
}

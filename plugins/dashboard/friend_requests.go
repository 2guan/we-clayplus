// 好友申请与验证凭据分开展示；只有收到的真实申请才能通过，网页不能提交验证票据。
package dashboard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

type FriendRequestField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type FriendRequest struct {
	Dismissed                      bool
	CapturedAccount                string `json:"-"`
	ID, Applicant, Comment, V1, V2 string
	Created                        string
	Scene                          int
	Nickname                       string
	Avatar                         string
	Details                        []FriendRequestField
	Status                         string
	Due                            string
	Updated                        string
	Reason                         string
}
type friendRequestView struct {
	Dismissed bool                 `json:"dismissed"`
	ID        string               `json:"id"`
	MessageID string               `json:"message_id"`
	Applicant string               `json:"applicant"`
	Nickname  string               `json:"nickname"`
	Avatar    string               `json:"avatar"`
	Comment   string               `json:"comment"`
	Created   string               `json:"created"`
	Scene     int                  `json:"scene"`
	Details   []FriendRequestField `json:"details"`
	Status    string               `json:"status"`
	Due       string               `json:"due,omitempty"`
	Reason    string               `json:"reason,omitempty"`
}

func friendRequestID(q FriendRequest) string {
	key := q.V1 + "\x00" + q.V2
	if q.V1 == "" || q.V2 == "" {
		key = "incomplete:" + q.ID + ":" + q.Applicant
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
}
func friendRequestKey(account, id string) string { return "ai-friend:" + account + ":" + id }
func (s *Server) loadFriend(account, id string) (FriendRequest, error) {
	var q FriendRequest
	raw, err := s.store.getSecret(friendRequestKey(account, id))
	if err == nil {
		err = json.Unmarshal([]byte(raw), &q)
	}
	return q, err
}
func (s *Server) saveFriend(account string, epoch uint64, id string, q FriendRequest) error {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.Account.ID != account || s.store.accountEpoch != epoch {
		return fmt.Errorf("账号已切换，好友申请结果未写入新账号")
	}
	raw, err := json.Marshal(q)
	if err != nil {
		return err
	}
	return s.store.putSecret(friendRequestKey(account, id), string(raw))
}
func friendView(id string, q FriendRequest) friendRequestView {
	redact := func(v string) string {
		for _, secret := range []string{q.V1, q.V2} {
			if secret != "" {
				v = strings.ReplaceAll(v, secret, "[验证凭据已隐藏]")
			}
		}
		return v
	}
	fields := make([]FriendRequestField, 0, len(q.Details))
	for _, f := range q.Details {
		fields = append(fields, FriendRequestField{Name: redact(f.Name), Value: redact(f.Value)})
	}
	return friendRequestView{Dismissed: q.Dismissed, ID: id, MessageID: q.ID, Applicant: redact(q.Applicant), Nickname: redact(q.Nickname), Avatar: redact(q.Avatar), Comment: redact(q.Comment), Created: q.Created, Scene: q.Scene, Details: fields, Status: q.Status, Due: q.Due, Reason: redact(q.Reason)}
}

func (s *Server) HandleFriend(q FriendRequest) error {
	if q.ID == "" {
		return fmt.Errorf("好友申请缺少原消息编号")
	}
	if q.Created == "" {
		q.Created = time.Now().Format(time.RFC3339)
	}
	st := s.store.snapshot()
	account, epoch := st.Account.ID, st.AccountEpoch
	if q.CapturedAccount != "" && q.CapturedAccount != account {
		return nil
	}
	if account == "" {
		return fmt.Errorf("微信账号尚未就绪")
	}
	id := friendRequestID(q)
	s.friendMu.Lock()
	defer s.friendMu.Unlock()
	if old, err := s.loadFriend(account, id); err == nil {
		// 重放同一凭据只补齐展示资料，不复活已经处理或等待的任务。
		q.Status, q.Due, q.Reason, q.Created = old.Status, old.Due, old.Reason, old.Created
		q.Dismissed = old.Dismissed
		if q.Nickname == "" {
			q.Nickname = old.Nickname
		}
		if q.Avatar == "" {
			q.Avatar = old.Avatar
		}
		if len(q.Details) == 0 {
			q.Details = old.Details
		}
		return s.saveFriend(account, epoch, id, q)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	q.Status = "pending"
	if q.V1 == "" || q.V2 == "" || q.Applicant == "" || q.Scene < 0 || q.Scene > 1000 {
		q.Status = "unavailable"
		q.Reason = "微信未提供完整有效的验证参数，仅可查看申请内容"
	}
	if q.Status == "pending" {
		q.Reason = friendMatchReason(st, q)
	}
	if q.Status == "pending" && q.Reason == "" && !s.businessCooldown(account, "auto_accept", q.Applicant) {
		q.Reason = "自动通过冷却中，未安排本次申请，可手动处理"
	}
	if q.Status == "pending" && q.Reason == "" {
		q.Status = "scheduled"
		q.Due = time.Now().Add(time.Duration(businessNumber(st, "auto_accept", "delay_seconds")) * time.Second).Format(time.RFC3339Nano)
	}
	if err := s.saveFriend(account, epoch, id, q); err != nil {
		return err
	}
	if q.Status == "scheduled" {
		if s.friendWorkers == nil {
			s.friendWorkers = map[string]bool{}
		}
		key := friendRequestKey(account, id)
		s.friendWorkers[key] = true
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			defer func() { s.friendMu.Lock(); delete(s.friendWorkers, key); s.friendMu.Unlock() }()
			due, _ := time.Parse(time.RFC3339Nano, q.Due)
			err := momentRandomWait(s.ctx, time.Until(due))
			if err != nil {
				s.friendMu.Lock()
				old, e := s.loadFriend(account, id)
				if e == nil && old.Status == "scheduled" {
					old.Status, old.Due, old.Reason = "pending", "", "服务已停止，自动通过未继续；可手动处理"
					_ = s.saveFriend(account, epoch, id, old)
				}
				s.friendMu.Unlock()
				return
			}
			_, _ = s.acceptFriend(s.ctx, account, epoch, id, true)
		}()
	}
	s.notify()
	return nil
}

// 手动通过、延迟自动通过与 AI 通过共用持久化 claim；未知结果不重复提交。
func (s *Server) acceptFriend(ctx context.Context, account string, epoch uint64, id string, automatic bool) (friendRequestView, error) {
	s.friendMu.Lock()
	q, err := s.loadFriend(account, id)
	if err != nil {
		s.friendMu.Unlock()
		return friendRequestView{}, fmt.Errorf("好友申请不存在或不属于当前账号")
	}
	st := s.store.snapshot()
	if st.Account.ID != account || st.AccountEpoch != epoch {
		s.friendMu.Unlock()
		return friendRequestView{}, fmt.Errorf("账号已切换")
	}
	if q.Dismissed {
		s.friendMu.Unlock()
		if automatic {
			return friendView(id, q), nil
		}
		return friendView(id, q), fmt.Errorf("该申请已关闭，未提交通过请求")
	}
	if q.Status == "" {
		q.Status = "pending"
	}
	if q.Status != "pending" && q.Status != "scheduled" {
		s.friendMu.Unlock()
		return friendView(id, q), fmt.Errorf("申请已处理或不可操作，请刷新后核对")
	}
	if automatic {
		if q.Status != "scheduled" {
			s.friendMu.Unlock()
			return friendView(id, q), nil
		}
		if reason := friendMatchReason(st, q); reason != "" {
			q.Status, q.Due, q.Reason = "pending", "", reason
			err = s.saveFriend(account, epoch, id, q)
			s.friendMu.Unlock()
			return friendView(id, q), err
		}
	}
	if !s.cfg.Mock && st.Account.Status != "online" {
		s.friendMu.Unlock()
		return friendView(id, q), fmt.Errorf("微信尚未登录，未提交通过请求")
	}
	if q.V1 == "" || q.V2 == "" || q.Scene < 0 || q.Scene > 1000 {
		s.friendMu.Unlock()
		return friendView(id, q), fmt.Errorf("验证参数不完整，无法通过")
	}
	if ctx.Err() != nil {
		s.friendMu.Unlock()
		return friendView(id, q), ctx.Err()
	}
	for _, c := range st.Contacts {
		if c.ID == q.Applicant && c.Kind == "friend" {
			q.Status, q.Due, q.Reason = "accepted", "", "申请人已经是当前账号的好友"
			err = s.saveFriend(account, epoch, id, q)
			if err == nil {
				if run, e := s.beginBusiness(account, businessRequest{Plugin: "auto_accept", Target: q.Applicant}, "completed"); e == nil {
					s.finishBusiness(run, "completed", "申请人已经是当前账号好友，未重复提交验证")
				}
			}
			s.friendMu.Unlock()
			return friendView(id, q), err
		}
	}
	claimed, e := s.claimBusiness(account, "friend:"+q.V1+":"+q.V2)
	if e != nil {
		s.friendMu.Unlock()
		return friendView(id, q), e
	}
	if !claimed {
		q.Status, q.Reason, q.Due = "unknown", "申请此前已经提交，请在微信核对；不会自动重试", ""
		_ = s.saveFriend(account, epoch, id, q)
		s.friendMu.Unlock()
		return friendView(id, q), fmt.Errorf("申请此前已经提交，未重复通过")
	}
	q.Status, q.Reason, q.Due = "processing", "正在提交通过请求", ""
	if err = s.saveFriend(account, epoch, id, q); err != nil {
		s.friendMu.Unlock()
		return friendView(id, q), err
	}
	run, err := s.beginBusiness(account, businessRequest{Plugin: "auto_accept", Target: q.Applicant}, "running")
	if err != nil {
		q.Status, q.Reason = "unknown", "本地执行记录保存失败，未继续提交；请在微信核对"
		_ = s.saveFriend(account, epoch, id, q)
		s.friendMu.Unlock()
		return friendView(id, q), err
	}
	if s.friendProcessing == nil {
		s.friendProcessing = map[string]bool{}
	}
	s.friendProcessing[friendRequestKey(account, id)] = true
	s.friendMu.Unlock()
	s.notify()
	if !s.cfg.Mock {
		if s.host == nil {
			err = fmt.Errorf("微信尚未就绪")
		} else {
			params, _ := json.Marshal(map[string]any{"v1": q.V1, "v2": q.V2, "scene": q.Scene, "account": account})
			_, err = s.host.Execute(ctx, control.Command{Operation: "friend-verify", Params: params})
		}
	}
	status, reason := "accepted", "已通过好友申请"
	if err != nil {
		status = "unknown"
		reason = "通过结果未确认，请在手机微信核对；不会自动重试"
	}
	s.friendMu.Lock()
	delete(s.friendProcessing, friendRequestKey(account, id))
	q.Status, q.Reason, q.Updated = status, reason, time.Now().Format(time.RFC3339)
	saveErr := s.saveFriend(account, epoch, id, q)
	s.friendMu.Unlock()
	s.finishBusiness(run, map[string]string{"accepted": "completed", "unknown": "unknown"}[status], reason)
	if saveErr != nil {
		return friendView(id, q), saveErr
	}
	if err != nil {
		return friendView(id, q), fmt.Errorf("%s", reason)
	}
	return friendView(id, q), nil
}
func (s *Server) markFriendResult(account string, epoch uint64, v1, v2, status string) error {
	s.friendMu.Lock()
	defer s.friendMu.Unlock()
	id := friendRequestID(FriendRequest{V1: v1, V2: v2})
	q, err := s.loadFriend(account, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	q.Status, q.Due, q.Updated = status, "", time.Now().Format(time.RFC3339)
	q.Reason = map[string]string{"accepted": "已通过好友申请", "unknown": "通过结果未确认，请在微信核对；不会自动重试"}[status]
	return s.saveFriend(account, epoch, id, q)
}
func (s *Server) friendRequestsAPI(w http.ResponseWriter, r *http.Request, u User) {
	w.Header().Set("Cache-Control", "no-store")
	if requireAdmin(u) != nil {
		writeError(w, 403, "仅管理员可以查看和处理好友申请")
		return
	}
	account, epoch := s.momentNotificationAccount()
	if r.Method == http.MethodPost {
		var q struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if !decode(w, r, &q) {
			return
		}
		if len(q.ID) != 64 || strings.Trim(q.ID, "0123456789abcdef") != "" {
			writeError(w, 400, "申请编号无效")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		var item friendRequestView
		var err error
		switch q.Action {
		case "dismiss", "restore":
			item, err = s.dismissFriend(account, epoch, q.ID, q.Action == "dismiss")
		case "", "accept":
			item, err = s.acceptFriend(ctx, account, epoch, q.ID, false)
		default:
			writeError(w, 400, "不支持的好友申请操作")
			return
		}
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, item)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	s.friendMu.Lock()
	defer s.friendMu.Unlock()
	prefix := friendRequestKey(account, "")
	rows, err := s.store.db.Query("SELECT id FROM secrets WHERE substr(id,1,?)=?", len(prefix), prefix)
	if err != nil {
		writeError(w, 500, "申请列表读取失败")
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, strings.TrimPrefix(id, prefix))
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, 500, "申请列表读取失败")
		return
	}
	items := []friendRequestView{}
	pending := 0
	for _, id := range ids {
		q, e := s.loadFriend(account, id)
		if e != nil {
			writeError(w, 500, "申请记录解密失败")
			return
		}
		if q.Status == "" {
			q.Status = "pending"
			var claimed int
			_ = s.store.db.QueryRow("SELECT COUNT(*) FROM business_claims WHERE account=? AND id=?", account, fmt.Sprintf("%x", sha256.Sum256([]byte("friend:"+q.V1+":"+q.V2)))).Scan(&claimed)
			if claimed > 0 {
				q.Status, q.Reason = "unknown", "此前已处理，结果请在微信核对"
			}
		}
		if q.Status == "scheduled" && !s.friendWorkers[friendRequestKey(account, id)] {
			q.Status, q.Due, q.Reason = "pending", "", "服务曾中断，自动通过未继续，可手动处理"
			if e = s.saveFriend(account, epoch, id, q); e != nil {
				writeError(w, 409, e.Error())
				return
			}
		}
		if q.Status == "processing" && !s.friendProcessing[friendRequestKey(account, id)] {
			q.Status, q.Due, q.Reason = "unknown", "", "服务曾中断或操作尚未确认，请在微信核对；不会自动重试"
			if e = s.saveFriend(account, epoch, id, q); e != nil {
				writeError(w, 409, e.Error())
				return
			}
		}
		if !q.Dismissed && (q.Status == "pending" || q.Status == "scheduled") {
			pending++
		}
		items = append(items, friendView(id, q))
	}
	if current, generation := s.momentNotificationAccount(); current != account || generation != epoch {
		writeError(w, 409, "账号已切换，请刷新申请列表")
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339, items[i].Created)
		b, _ := time.Parse(time.RFC3339, items[j].Created)
		return a.After(b)
	})
	writeJSON(w, 200, map[string]any{"items": items, "pending": pending})
}

// 关闭仅更新本地待办标记；保留凭据和处理状态，撤销也不会重新安排自动通过。
func (s *Server) dismissFriend(account string, epoch uint64, id string, dismissed bool) (friendRequestView, error) {
	s.friendMu.Lock()
	defer s.friendMu.Unlock()
	q, err := s.loadFriend(account, id)
	if err != nil {
		return friendRequestView{}, fmt.Errorf("申请不存在或不属于当前账号")
	}
	if q.Status == "processing" {
		return friendView(id, q), fmt.Errorf("通过请求正在提交，请等待结果后关闭")
	}
	q.Dismissed = dismissed
	if q.Status == "scheduled" {
		q.Status, q.Due, q.Reason = "pending", "", "本地已关闭，自动通过已取消"
	}
	if !dismissed && q.Reason == "本地已关闭，自动通过已取消" {
		q.Reason = "已恢复显示，请手动处理；不会自动通过"
	}
	q.Updated = time.Now().Format(time.RFC3339)
	if err = s.saveFriend(account, epoch, id, q); err != nil {
		return friendRequestView{}, err
	}
	s.notify()
	return friendView(id, q), nil
}

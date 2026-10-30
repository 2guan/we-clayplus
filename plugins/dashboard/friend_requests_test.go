package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func friendFixture(t *testing.T, automatic bool, delay int) (*Server, FriendRequest) {
	t.Helper()
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "fixture-self"
		st.Account.Status = "online"
		for i := range st.Plugins {
			if st.Plugins[i].ID == "auto_accept" {
				st.Plugins[i].Enabled = automatic
			}
		}
		st.Settings["auto_accept.delay_seconds"] = float64(delay)
		return nil
	}))
	return s, FriendRequest{ID: "fixture-message", Applicant: "fixture-peer", Nickname: "申请昵称", Comment: strings.Repeat("完整附言\n", 30), V1: "fixture-v1", V2: "fixture-ticket", Scene: 3, Details: []FriendRequestField{{Name: "msg/@alias", Value: "申请微信号"}, {Name: "msg/@city", Value: "测试城市"}}}
}
func friendCall(s *Server, body string, role string) *httptest.ResponseRecorder {
	method := "GET"
	if body != "" {
		method = "POST"
	}
	r := httptest.NewRequest(method, "/api/friend-requests", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.friendRequestsAPI(w, r, User{Role: role})
	return w
}
func TestFriendRequestFullDetailsManualAndReplay(t *testing.T) {
	s, q := friendFixture(t, false, 10)
	var calls atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		var p struct {
			V1, V2  string
			Scene   int
			Account string
		}
		_ = json.Unmarshal(c.Params, &p)
		if c.Operation != "friend-verify" || p.V1 != q.V1 || p.V2 != q.V2 || p.Scene != 3 || p.Account != "fixture-self" {
			t.Error("原始申请参数丢失")
		}
		calls.Add(1)
		return nil, nil
	})
	replyRegressionCheck(t, s.HandleFriend(q))
	replyRegressionCheck(t, s.HandleFriend(q))
	w := friendCall(s, "", "admin")
	var page struct {
		Items   []friendRequestView
		Pending int
	}
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &page))
	if len(page.Items) != 1 || page.Pending != 1 || page.Items[0].Comment != q.Comment || page.Items[0].Nickname != q.Nickname || len(page.Items[0].Details) != 2 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), q.V1) || strings.Contains(w.Body.String(), q.V2) {
		t.Fatal("验证票据返回网页")
	}
	if w = friendCall(s, "", "viewer"); w.Code != 403 {
		t.Fatal("非管理员读取好友申请")
	}
	id := friendRequestID(q)
	body := `{"id":"` + id + `"}`
	if w = friendCall(s, body, "admin"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = friendCall(s, body, "admin"); w.Code != 409 || calls.Load() != 1 {
		t.Fatal("重复通过申请", w.Code, calls.Load())
	}
	replyRegressionCheck(t, s.HandleFriend(q))
	saved, err := s.loadFriend("fixture-self", id)
	replyRegressionCheck(t, err)
	if saved.Status != "accepted" {
		t.Fatal("重放复活已通过申请", saved.Status)
	}
	var stored string
	replyRegressionCheck(t, s.store.db.QueryRow("SELECT value FROM secrets WHERE id=?", friendRequestKey("fixture-self", id)).Scan(&stored))
	if strings.Contains(stored, q.V2) || strings.Contains(stored, q.Comment) {
		t.Fatal("申请未加密保存")
	}
}
func TestFriendAutoDelayManualWinsAndDisable(t *testing.T) {
	s, q := friendFixture(t, true, 1)
	var calls atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) { calls.Add(1); return nil, nil })
	replyRegressionCheck(t, s.HandleFriend(q))
	if calls.Load() != 0 {
		t.Fatal("自动通过未等待延迟")
	}
	id := friendRequestID(q)
	if w := friendCall(s, `{"id":"`+id+`"}`, "admin"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	time.Sleep(1100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("手动通过后自动任务再次提交", calls.Load())
	}
	q.ID = "second"
	q.Applicant = "fixture-second-peer"
	q.V2 = "fixture-ticket-second"
	replyRegressionCheck(t, s.HandleFriend(q))
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		for i := range st.Plugins {
			if st.Plugins[i].ID == "auto_accept" {
				st.Plugins[i].Enabled = false
			}
		}
		return nil
	}))
	time.Sleep(1100 * time.Millisecond)
	saved, err := s.loadFriend("fixture-self", friendRequestID(q))
	replyRegressionCheck(t, err)
	if calls.Load() != 1 || saved.Status != "pending" {
		t.Fatal("关闭后仍自动通过", calls.Load(), saved.Status)
	}
}
func TestFriendUnknownAndAccountIsolation(t *testing.T) {
	s, q := friendFixture(t, false, 0)
	var calls atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		calls.Add(1)
		return nil, fmt.Errorf("verify failure %s", q.V2)
	})
	replyRegressionCheck(t, s.HandleFriend(q))
	body := `{"id":"` + friendRequestID(q) + `"}`
	w := friendCall(s, body, "admin")
	if w.Code != 409 || strings.Contains(w.Body.String(), q.V2) {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = friendCall(s, body, "admin")
	if calls.Load() != 1 {
		t.Fatal("未知结果自动重复")
	}
	replyRegressionCheck(t, s.SetAccount(Account{ID: "fixture-other", Status: "online"}))
	if w = friendCall(s, body, "admin"); w.Code != 409 || calls.Load() != 1 {
		t.Fatal("跨账号通过申请")
	}
	var page struct{ Items []friendRequestView }
	w = friendCall(s, "", "admin")
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &page))
	if len(page.Items) != 0 {
		t.Fatal("跨账号读取申请")
	}
}
func TestFriendIncompleteAndLegacyStayVisible(t *testing.T) {
	s, q := friendFixture(t, false, 0)
	q.V2 = ""
	replyRegressionCheck(t, s.HandleFriend(q))
	w := friendCall(s, "", "admin")
	if !strings.Contains(w.Body.String(), "unavailable") {
		t.Fatal(w.Body.String())
	}
	q.V2 = "fixture-ticket"
	raw, _ := json.Marshal(q)
	replyRegressionCheck(t, s.store.putSecret(friendRequestKey("fixture-self", friendRequestID(q)), string(raw)))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) { return nil, nil })
	if w = friendCall(s, `{"id":"`+friendRequestID(q)+`"}`, "admin"); w.Code != 200 {
		t.Fatal("旧版待处理申请不能手动通过", w.Code, w.Body.String())
	}
	for _, n := range []float64{-1, 0.5, 86401} {
		if validateSettings("auto_accept", map[string]any{"delay_seconds": n}) == nil {
			t.Fatal("接受非法延迟", n)
		}
	}
}

func TestFriendDelayedRequestCannotCrossAccountSwitch(t *testing.T) {
	s, q := friendFixture(t, true, 1)
	var calls atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) { calls.Add(1); return nil, nil })
	replyRegressionCheck(t, s.HandleFriend(q))
	replyRegressionCheck(t, s.SetAccount(Account{ID: "fixture-next", Status: "online"}))
	time.Sleep(1100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("旧账号延迟申请在新账号上被通过")
	}
	q.ID = "captured-old"
	q.V2 = "fixture-another-ticket"
	q.CapturedAccount = "fixture-self"
	replyRegressionCheck(t, s.HandleFriend(q))
	w := friendCall(s, "", "admin")
	var result struct{ Items []friendRequestView }
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &result))
	if len(result.Items) != 0 {
		t.Fatal("旧账号申请进入了新账号列表")
	}
}

func TestFriendDismissCancelsAutoPreservesReplayAndSupportsUndo(t *testing.T) {
	s, q := friendFixture(t, true, 1)
	var calls atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) { calls.Add(1); return nil, nil })
	replyRegressionCheck(t, s.HandleFriend(q))
	id := friendRequestID(q)
	w := friendCall(s, `{"id":"`+id+`","action":"dismiss"}`, "admin")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var closed friendRequestView
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &closed))
	if !closed.Dismissed || closed.Status != "pending" || closed.Due != "" {
		t.Fatal(closed)
	}
	replyRegressionCheck(t, s.HandleFriend(q))
	w = friendCall(s, "", "admin")
	var list struct {
		Items   []friendRequestView
		Pending int
	}
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &list))
	if list.Pending != 0 || len(list.Items) != 1 || !list.Items[0].Dismissed {
		t.Fatal("关闭申请重放复活", w.Body.String())
	}
	w = friendCall(s, `{"id":"`+id+`"}`, "admin")
	if w.Code != 409 {
		t.Fatal("关闭后仍通过", w.Code)
	}
	time.Sleep(1100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("关闭后延迟任务仍执行")
	}
	w = friendCall(s, `{"id":"`+id+`","action":"restore"}`, "admin")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, err := s.loadFriend("fixture-self", id)
	replyRegressionCheck(t, err)
	if saved.Dismissed || saved.Status != "pending" || saved.Due != "" {
		t.Fatal("撤销后重新安排自动通过", saved.Status, saved.Due)
	}
	// 未知结果只能恢复显示，不能因撤销关闭而变成可重试的待处理状态。
	saved.Status = "unknown"
	account, epoch := s.momentNotificationAccount()
	replyRegressionCheck(t, s.saveFriend(account, epoch, id, saved))
	_ = friendCall(s, `{"id":"`+id+`","action":"dismiss"}`, "admin")
	_ = friendCall(s, `{"id":"`+id+`","action":"restore"}`, "admin")
	saved, err = s.loadFriend(account, id)
	replyRegressionCheck(t, err)
	if saved.Status != "unknown" {
		t.Fatal("撤销丢失操作结果")
	}
}

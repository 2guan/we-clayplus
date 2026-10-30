package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestMomentNotificationReadWatermarkDedupAndPersistence(t *testing.T) {
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-self"; return nil }))
	account, epoch := s.momentNotificationAccount()
	status := Message{Kind: "system", Source: "wechat", Target: "weixin"}
	handled, err := s.RecordMomentStatus(status, `<msg><op id="7"><name>MomentsUnreadMsgStatus</name><arg>100</arg></op></msg>`)
	if !handled || err != nil {
		t.Fatal(handled, err)
	}
	items := []MomentNotification{{ID: "old", MomentID: "1", Created: 90}, {ID: "new", MomentID: "2", Actor: "测试好友", Created: 110, Content: "新回复"}}
	calls := 0
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		calls++
		if c.Operation != "moments-notification-sync" {
			t.Fatal(c.Operation)
		}
		return MomentNotificationSync{Key: []byte("fixture-sync-key"), Items: append(items, items...)}, nil
	})
	v, _, err := s.syncMomentNotifications(context.Background())
	if err != nil || notificationUnread(v) != 1 || len(v.Items) != 2 {
		t.Fatal(v, err)
	}
	s.momentNotificationChecked = time.Time{}
	v, _, err = s.syncMomentNotifications(context.Background())
	if err != nil || notificationUnread(v) != 1 || len(v.Items) != 2 || calls != 2 {
		t.Fatal(v, err, calls)
	}
	// 普通聊天和群内伪造的XML不能推进已读时间。
	for _, m := range []Message{{Kind: "text", Source: "wechat", Target: "weixin"}, {Kind: "system", Source: "wechat", Target: "fixture-room"}, {Kind: "system", Source: "ai", Target: "weixin"}} {
		if h, _ := s.RecordMomentStatus(m, `<msg><op><name>MomentsUnreadMsgStatus</name><arg>999</arg></op></msg>`); h {
			t.Fatal("伪造状态被接受")
		}
	}
	// 点开提醒后只标记对应的ID，新到来的提醒不能一起清除。
	v.Items = append(v.Items, MomentNotification{ID: "later", MomentID: "3", Created: 120})
	replyRegressionCheck(t, s.saveMomentNotifications(account, epoch, v))
	r := httptest.NewRequest("POST", "/api/moments/notifications/read", strings.NewReader(`{"ids":["new"]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.momentNotificationsAPI(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"unread":1`) {
		t.Fatal(w.Code, w.Body.String())
	}
	v, err = s.loadMomentNotifications(account)
	if err != nil || notificationUnread(v) != 1 {
		t.Fatal(v, err)
	}
	var encrypted string
	replyRegressionCheck(t, s.store.db.QueryRow("SELECT value FROM secrets WHERE id=?", momentNotificationKey(account)).Scan(&encrypted))
	if strings.Contains(encrypted, "fixture-sync-key") || strings.Contains(encrypted, "新回复") {
		t.Fatal("通知或同步游标未加密")
	}
	// 真实微信已读时间应持久化且单调递增。
	_, err = s.RecordMomentStatus(status, `<msg><op><name>MomentsUnreadMsgStatus</name><arg>120</arg></op></msg>`)
	replyRegressionCheck(t, err)
	_, err = s.RecordMomentStatus(Message{Kind: "status", Source: "wechat", Self: true, Target: account, SenderID: account}, `<msg><op><name>MomentsUnreadMsgStatus</name><arg>121</arg></op></msg>`)
	replyRegressionCheck(t, err)
	_, err = s.RecordMomentStatus(status, `<msg><op><name>MomentsUnreadMsgStatus</name><arg>80</arg></op></msg>`)
	replyRegressionCheck(t, err)
	v, err = s.loadMomentNotifications(account)
	if err != nil || v.ReadBefore != 121 || notificationUnread(v) != 0 {
		t.Fatal(v, err)
	}
	s.Close()
	restored, err := openStore(s.cfg.DataDir, false)
	replyRegressionCheck(t, err)
	defer restored.db.Close()
	raw, err := restored.getSecret(momentNotificationKey(account))
	replyRegressionCheck(t, err)
	var saved momentNotificationState
	replyRegressionCheck(t, json.Unmarshal([]byte(raw), &saved))
	if saved.ReadBefore != 121 || len(saved.Items) != 3 || notificationUnread(saved) != 0 {
		t.Fatal("重启丢失已读状态")
	}
}

func TestMomentNotificationAccountSwitchDiscardsOldSync(t *testing.T) {
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-old"; return nil }))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		replyRegressionCheck(t, s.SetAccount(Account{ID: "fixture-next", Status: "online"}))
		return MomentNotificationSync{Items: []MomentNotification{{ID: "old", MomentID: "1", Created: 100}}}, nil
	})
	if _, _, err := s.syncMomentNotifications(context.Background()); err == nil {
		t.Fatal("账号切换后采用旧通知")
	}
	v, err := s.loadMomentNotifications("fixture-next")
	if err != nil || len(v.Items) != 0 {
		t.Fatal("旧通知进入新账号")
	}
}

func TestMomentNotificationListAndSelectedDetailDoNotMarkRead(t *testing.T) {
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-self"; return nil }))
	account, epoch := s.momentNotificationAccount()
	replyRegressionCheck(t, s.saveMomentNotifications(account, epoch, momentNotificationState{Items: []MomentNotification{
		{ID: "first", MomentID: "101", Created: 110}, {ID: "second", MomentID: "202", Created: 120},
	}}))
	details := 0
	fail := false
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation == "moments-notification-sync" {
			return MomentNotificationSync{Items: []MomentNotification{}}, nil
		}
		if c.Operation != "ai-action" || c.ID != "moments.Detail" || c.Content != "101" {
			t.Fatalf("unexpected operation: %+v", c)
		}
		details++
		id := "101"
		if fail {
			id = "wrong"
		}
		return map[string]any{"moment": Moment{ID: id, Content: "selected"}}, nil
	})
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.momentNotificationsAPI(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	w := get("/api/moments/notifications")
	var page MomentPage
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &page))
	if w.Code != 200 || details != 0 || len(page.Items) != 0 || len(page.Notifications) != 2 || page.Unread != 2 {
		t.Fatal(w.Code, w.Body.String(), details)
	}
	w = get("/api/moments/notifications?id=first")
	var m Moment
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &m))
	if w.Code != 200 || m.ID != "101" || details != 1 {
		t.Fatal(w.Code, w.Body.String(), details)
	}
	if w = get("/api/moments/notifications?id=foreign-account-notice"); w.Code != 404 || details != 1 {
		t.Fatal(w.Code, details)
	}
	fail = true
	if w = get("/api/moments/notifications?id=first"); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	v, err := s.loadMomentNotifications(account)
	replyRegressionCheck(t, err)
	if notificationUnread(v) != 2 {
		t.Fatal("list, detail, or failed detail cleared unread")
	}
}

// 文件名：moments_api_test.go
// 主要功能：验证朋友圈发布的角色、好友范围及媒体限制；全部使用脱机环境。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestMomentUnreadAPIIsReadOnly(t *testing.T) {
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-self"; return nil }))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation != "moments-notification-sync" || c.Target != "" || c.ID != "" {
			t.Error("未读查询变成了其他操作或范围", c.Operation)
		}
		return MomentNotificationSync{Items: []MomentNotification{{ID: "a", MomentID: "1", Created: 1}, {ID: "b", MomentID: "2", Created: 2}, {ID: "c", MomentID: "3", Created: 3}}}, nil
	})
	w := httptest.NewRecorder()
	s.momentsAPI(w, httptest.NewRequest("GET", "/api/moments/unread?target=fixture-other", nil), User{Role: "viewer"})
	var result map[string]uint32
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result["unread"] != 3 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("未读计数丢失或允许缓存", w.Code, err)
	}
}

func TestMomentReplyAPIReferencesAndMockState(t *testing.T) {
	s := newServer(t, true)
	post := func(server *Server, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/moments/interact", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.momentInteractionAPI(w, r)
		return w
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Moments = []Moment{{ID: "123", AuthorID: "fixture-owner", Comments: []Comment{{ID: "7", AuthorID: "fixture-peer", Author: "被回复者", Content: "原评论"}}}}
		return nil
	}))
	for _, body := range []string{
		`{"id":"123","kind":"moment-comment","content":"测试","reply_comment_id":-1,"reply_author_id":"fixture-peer"}`,
		`{"id":"123","kind":"moment-comment","content":"测试","reply_comment_id":7}`,
		`{"id":"123","kind":"moment-comment","content":"测试","reply_comment_id":2147483648,"reply_author_id":"fixture-peer"}`,
		`{"id":"123","kind":"moment-like","reply_comment_id":7,"reply_author_id":"fixture-peer"}`,
		`{"id":"123","kind":"moment-comment","content":"测试","reply_comment_id":7,"reply_author_id":"fixture-other"}`,
	} {
		w := post(s, body)
		if w.Code < 400 || len(s.store.snapshot().Moments[0].Comments) != 1 {
			t.Fatal("非法回复被发送或改成普通留言", w.Code)
		}
	}
	w := post(s, `{"id":"123","kind":"moment-comment","content":"本次回复","reply_comment_id":7,"reply_author_id":"fixture-peer"}`)
	var item Moment
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &item))
	if w.Code != 200 || len(item.Comments) != 2 || item.Comments[1].ReplyTo != "被回复者" || item.Comments[1].Author != s.store.snapshot().Account.Name || item.Comments[1].ID != "8" {
		t.Fatal("脱机回复未绑定原评论", w.Code, item)
	}
	// 非脱机请求必须把相同编号和作者完整传至宿主，而不是只改变显示文字。
	real := newServer(t, false)
	calls := 0
	real.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		calls++
		var reply MomentReply
		replyRegressionCheck(t, json.Unmarshal(c.Params, &reply))
		if c.Operation != "moments-comment" || c.ID != "123" || c.Content != "本次回复" || reply.CommentID != 7 || reply.AuthorID != "fixture-peer" {
			t.Error("回复参数未透传")
		}
		return item, nil
	})
	w = post(real, `{"id":"123","kind":"moment-comment","content":"本次回复","reply_comment_id":7,"reply_author_id":"fixture-peer"}`)
	if w.Code != 200 || calls != 1 {
		t.Fatal("回复未单次传递至宿主", w.Code, calls)
	}
}

func TestMomentPublishValidation(t *testing.T) {
	s := newServer(t, true)
	for _, tc := range []struct {
		body, role string
		code       int
	}{
		{`{"content":"测试"}`, "viewer", 403},
		{`{"content":""}`, "admin", 400},
		{`{"content":"测试","blacklist":["not-a-friend"]}`, "admin", 400},
		{`{"content":"测试","images":["YWJj"]}`, "admin", 400},
	} {
		r := httptest.NewRequest("POST", "/api/moments", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.momentsAPI(w, r, User{Role: tc.role})
		if w.Code != tc.code {
			t.Fatalf("预期 %d，实际 %d", tc.code, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/person?id=unknown", nil)
	w := httptest.NewRecorder()
	s.personAPI(w, r)
	if w.Code != 404 {
		t.Fatal("接受未归档人员")
	}
}

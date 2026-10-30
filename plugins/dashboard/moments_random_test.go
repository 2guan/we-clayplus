package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func randomFixture(t *testing.T) (*Server, User) {
	t.Helper()
	s := newServer(t, false)
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "fixture-self"
		st.Account.Status = "online"
		st.Settings["moments.like_probability"] = float64(100)
		st.Settings["moments.comment_probability"] = float64(100)
		return nil
	}))
	return s, User{Role: "admin"}
}
func randomCall(t *testing.T, s *Server, u User, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/moments/random", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	s.momentRandomAPI(w, r, u)
	return w
}
func randomFinished(t *testing.T, s *Server) momentRandomJob {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s.momentRandomMu.Lock()
		job := s.momentRandomJob
		running := s.momentRandomCancel != nil
		var copy momentRandomJob
		if job != nil {
			copy = *job
		}
		s.momentRandomMu.Unlock()
		if !running {
			return copy
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("随机互动未结束")
	return momentRandomJob{}
}
func TestMomentRandomPaginationProbabilityAndUnknownWrite(t *testing.T) {
	s, u := randomFixture(t)
	now := time.Now()
	m := func(id string, hours int) Moment {
		return Moment{ID: id, AuthorID: "fixture-peer", Author: "测试好友", Content: "动态", Time: now.Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)}
	}
	rows := []Moment{m("old", 30), m("first", 1), m("second", 2)}
	var mu sync.Mutex
	writes := []string{}
	pages := []string{}
	fail := true
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		switch c.Operation {
		case "moments-page":
			mu.Lock()
			pages = append(pages, c.ID)
			mu.Unlock()
			if c.ID == "" {
				return MomentPage{Items: rows[:2], NextCursor: "page2"}, nil
			}
			return MomentPage{Items: rows[1:]}, nil
		case "ai-action":
			return map[string]any{"moment": m(c.Content, 1)}, nil
		case "moments-like":
			var guard struct {
				Account string
				Only    bool `json:"only_if_missing"`
			}
			_ = json.Unmarshal(c.Params, &guard)
			if guard.Account != "fixture-self" || !guard.Only {
				t.Error("未下传防重复及账号约束")
			}
			mu.Lock()
			writes = append(writes, c.ID)
			mu.Unlock()
			if fail {
				return nil, fmt.Errorf("写入结果未知")
			}
			return m(c.ID, 1), nil
		}
		return nil, fmt.Errorf("unexpected: %s", c.Operation)
	})
	if w := randomCall(t, s, User{Role: "viewer"}, `{"kind":"like"}`); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := randomCall(t, s, u, `{"kind":"like"}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	job := randomFinished(t, s)
	if job.Status != "failed" || job.Total != 2 || job.Sent != 0 {
		t.Fatal(job)
	}
	fail = false
	if w := randomCall(t, s, u, `{"kind":"like"}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	job = randomFinished(t, s)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(writes, []string{"first", "second"}) || !slices.Equal(pages, []string{"", "page2", "", "page2"}) || job.Sent != 1 || job.Skipped != 1 {
		t.Fatal(writes, pages, job)
	}
}
func TestMomentRandomStopAndZeroProbability(t *testing.T) {
	s, u := randomFixture(t)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["moments.like_probability"] = float64(0); return nil }))
	reads := 0
	s.host = replyRegressionHost(func(ctx context.Context, c control.Command) (any, error) {
		if c.Operation != "moments-page" {
			t.Error("0%仍发起读取详情或写操作", c.Operation)
		}
		reads++
		return MomentPage{Items: []Moment{{ID: "123", AuthorID: "fixture-peer", Time: time.Now().Add(-time.Minute).Format(time.RFC3339)}}}, nil
	})
	if w := randomCall(t, s, u, `{"kind":"like"}`); w.Code != 202 {
		t.Fatal(w.Code)
	}
	if job := randomFinished(t, s); job.Sent != 0 || job.Skipped != 1 || reads != 1 {
		t.Fatal(job, reads)
	}
	gate := make(chan struct{})
	s.host = replyRegressionHost(func(ctx context.Context, c control.Command) (any, error) {
		close(gate)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	w := randomCall(t, s, u, `{"kind":"comment"}`)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	<-gate
	if w = randomCall(t, s, u, `{"kind":"like"}`); w.Code != 409 {
		t.Fatal("未拦截并发", w.Code)
	}
	s.momentRandomMu.Lock()
	id := s.momentRandomJob.ID
	s.momentRandomMu.Unlock()
	w = randomCall(t, s, u, `{"stop":"`+id+`"}`)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	if job := randomFinished(t, s); job.Status != "cancelled" {
		t.Fatal(job)
	}
}
func TestMomentAutoReplyTargetsOriginalAndDecidesOnce(t *testing.T) {
	s, _ := randomFixture(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		if !strings.Contains(string(raw), "本次指定回复的评论") || !strings.Contains(string(raw), "问候") {
			t.Error("模型未收到定向评论")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"谢谢关心，你也一样！"}}]}`)
	}))
	defer remote.Close()
	replyRegressionCheck(t, s.store.putSecret("fixture-vendor", "fixture-key"))
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Models = []Model{{ID: "fixture", VendorID: "fixture-vendor", Name: "fixture", Model: "fixture", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture"}
		return nil
	}))
	s.momentReplyStarted = time.Now().Add(-time.Hour)
	var writes int
	m := Moment{ID: "123", AuthorID: "fixture-owner", Author: "动态作者", Content: "动态", Comments: []Comment{{ID: "7", AuthorID: "fixture-peer", Author: "回复者", Content: "问候", ReplyToID: "fixture-self"}, {ID: "6", AuthorID: "fixture-self", Author: "本人", Content: "原评论"}}}
	s.host = replyRegressionHost(func(ctx context.Context, c control.Command) (any, error) {
		switch c.Operation {
		case "moments-notification-sync":
			return MomentNotificationSync{Items: []MomentNotification{
				{ID: "old", MomentID: "123", ActorID: "fixture-peer", Kind: "comment", CommentID: 7, ReplyToSelf: true, Created: time.Now().Add(-2 * time.Hour).Unix()},
				{ID: "new", MomentID: "123", ActorID: "fixture-peer", Kind: "comment", CommentID: 7, ReplyToSelf: true, Created: time.Now().Unix()},
			}}, nil
		case "ai-action":
			return map[string]any{"moment": m}, nil
		case "moments-comment":
			var guard struct {
				MomentReply
				Only bool `json:"only_if_missing"`
			}
			_ = json.Unmarshal(c.Params, &guard)
			if guard.CommentID != 7 || guard.AuthorID != "fixture-peer" || !guard.Only || c.Content != "谢谢关心，你也一样！" {
				t.Error("未定向回复", c)
			}
			writes++
			return m, nil
		}
		return nil, fmt.Errorf("unexpected operation %s", c.Operation)
	})
	// 首次只建立起点，不追溯旧通知；随后每条新回复只作一次概率判断。
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	job := randomFinished(t, s)
	if job.Kind != "reply" || job.Sent != 1 || writes != 1 {
		t.Fatal(job, writes)
	}
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	if writes != 1 {
		t.Fatal("重复回应同一提醒")
	}
}
func TestMomentRandomDefaultsAndRange(t *testing.T) {
	d := defaults()
	if d["moments.like_probability"] != float64(80) || d["moments.comment_probability"] != float64(50) || d["moments.comment_interval_min"] != float64(15) || d["moments.comment_interval_max"] != float64(30) {
		t.Fatal(d)
	}
	for _, v := range []float64{-1, 101, 0.5} {
		if validateSettings("moments", map[string]any{"like_probability": v}) == nil {
			t.Fatal("接受了非法概率", v)
		}
	}
	if !momentRandomAlready(Moment{Comments: []Comment{{AuthorID: "fixture-self"}}}, "comment", "fixture-self") {
		t.Fatal("本人已评论仍追加")
	}
}

func TestMomentReplyZeroProbabilityIsNotRetried(t *testing.T) {
	s, _ := randomFixture(t)
	s.momentReplyStarted = time.Now().Add(-time.Hour)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["moments.comment_probability"] = float64(0); return nil }))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation != "moments-notification-sync" {
			t.Error("不应调用模型或发送操作", c.Operation)
		}
		return MomentNotificationSync{Items: []MomentNotification{{ID: "new", MomentID: "123", ActorID: "fixture-peer", Kind: "comment", CommentID: 7, ReplyToSelf: true, Created: time.Now().Unix()}}}, nil
	})
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["moments.comment_probability"] = float64(100); return nil }))
	replyRegressionCheck(t, s.checkMomentReplies(context.Background()))
	if s.momentRandomJob != nil {
		t.Fatal("概率改变后重抽同一条回复")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if momentRandomWait(ctx, time.Hour) == nil {
		t.Fatal("间隔等待未响应取消")
	}
}
func TestMomentRandomIntervalValidationUsesSavedOtherBound(t *testing.T) {
	s, u := randomFixture(t)
	revision := s.store.snapshot().ConfigRevision
	body := fmt.Sprintf(`{"kind":"settings","group":"moments","revision":%d,"values":{"comment_interval_min":31}}`, revision)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/action", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	s.action(w, r, u)
	if w.Code == 200 || !strings.Contains(w.Body.String(), "最短间隔不能大于最长间隔") {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.store.snapshot().Settings["moments.comment_interval_min"] != float64(15) {
		t.Fatal("失败配置仍被保存")
	}
}

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func retryFixture(t *testing.T, handler http.HandlerFunc) (*Server, Message, *atomic.Int32) {
	t.Helper()
	s := newServer(t, false)
	remote := httptest.NewServer(handler)
	t.Cleanup(remote.Close)
	sends := &atomic.Int32{}
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation != "send" {
			t.Error("意外操作", c.Operation)
		}
		n := sends.Add(1)
		return map[string]string{"id": fmt.Sprint("fixture-retry-receipt-", n)}, nil
	})
	m := Message{ID: "fixture-retry-current", Target: "fixture-retry-target", SenderID: "fixture-retry-target", Sender: "测试成员", Kind: "text", Content: "fixture-retry-input", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339)}
	replyRegressionCheck(t, s.store.putSecret("fixture-retry-vendor", "fixture-retry-key"))
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID, st.Account.Status = "fixture-retry-self", "online"
		st.Contacts = []Contact{{ID: m.Target, Kind: "friend", Mode: "auto"}}
		st.Messages = []Message{m}
		st.Settings["reply.enabled"], st.Settings["routing.fallback"] = true, false
		st.Settings["reply.interval_ms"], st.Settings["reply.max_parts"] = float64(0), float64(1)
		st.Models = []Model{{ID: "fixture-retry-model", Name: "fixture-retry-model", VendorID: "fixture-retry-vendor", Model: "fixture", Endpoint: remote.URL, Provider: "openai", Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-retry-model"}
		return nil
	}))
	return s, m, sends
}

func retrySubmit(t *testing.T, s *Server, m Message) Task {
	t.Helper()
	id, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	return waitModelTask(t, s, id)
}

func TestReplyRetryOnceThenSendExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-retry-answer"}}]}`)
	})
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || task.AutoRetries != 1 || task.ReplyRetryable || calls.Load() != 2 || sends.Load() != 1 || task.ReplyTo != m.ID {
		t.Fatal(task, calls.Load(), sends.Load())
	}
}

func TestReplyRetryRunsWholePipelineBeforeSecondRound(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		successAt int
		want      []string
	}{
		{"first-round-second-model", 2, []string{"a", "b"}},
		{"second-round-first-model", 3, []string{"a", "b", "a"}},
		{"second-round-second-model", 4, []string{"a", "b", "a", "b"}},
		{"both-rounds-fail", 0, []string{"a", "b", "a", "b"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				mu.Lock()
				calls = append(calls, body.Model)
				number := len(calls)
				mu.Unlock()
				if number != fixture.successAt {
					w.WriteHeader(503)
					return
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-pipeline-answer"}}]}`)
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				a := st.Models[0]
				a.ID, a.Name, a.Model = "a", "a", "a"
				b := a
				b.ID, b.Name, b.Model = "b", "b", "b"
				st.Models = []Model{a, b}
				st.Pipeline = []string{"a", "b"}
				st.Settings["routing.fallback"] = true
				return nil
			}))
			task := retrySubmit(t, s, m)
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			if !slices.Equal(got, fixture.want) {
				t.Fatal("流水线顺序错误", got, fixture.want)
			}
			wantRetries := 0
			if len(fixture.want) > 2 {
				wantRetries = 1
			}
			if task.AutoRetries != wantRetries {
				t.Fatal(task)
			}
			if fixture.successAt == 0 {
				if task.Status != "failed" || !task.ReplyRetryable || sends.Load() != 0 {
					t.Fatal(task, sends.Load())
				}
			} else if task.Status != "completed" || task.ReplyRetryable || sends.Load() != 1 {
				t.Fatal(task, sends.Load())
			}
		})
	}
}

func TestReplyRetryTimeoutAfterEmptyHTTP200(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-timeout-recovered"}}]}`)
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Models[0].Timeout = 1; return nil }))
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || calls.Load() != 2 || sends.Load() != 1 || task.AutoRetries != 1 {
		t.Fatal(task, calls.Load(), sends.Load())
	}
	found := false
	for _, l := range s.store.snapshot().Logs {
		if l.Level == "WARN" && strings.Contains(l.Message, "模型响应读取超时") {
			found = true
		}
	}
	if !found {
		t.Fatal("超时原因被错误标成响应超大")
	}
}

func TestReplyRetryRechecksSourceBeforeAutomaticRetry(t *testing.T) {
	var calls atomic.Int32
	var server *Server
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		replyRegressionCheck(t, server.store.change(func(st *State) error { st.Messages[0].Status = "revoked"; return nil }))
		w.WriteHeader(503)
	})
	server = s
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || task.ReplyRetryable || calls.Load() != 1 || sends.Load() != 0 || !strings.Contains(task.Error, "撤回") {
		t.Fatal(task, calls.Load(), sends.Load())
	}
}

func TestReplyRetryExhaustionAndConcurrentManualClaim(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-recovered"}}]}`)
	})
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || !task.ReplyRetryable || task.AutoRetries != 1 || calls.Load() != 2 || sends.Load() != 0 {
		t.Fatal(task, calls.Load(), sends.Load())
	}
	fail.Store(false)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.retryReply(task.ID, m.Target) == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("重复点击创建了多个任务", accepted.Load())
	}
	latest := s.store.snapshot().Tasks[len(s.store.snapshot().Tasks)-1]
	child := waitModelTask(t, s, latest.ID)
	if child.Status != "completed" || child.RetryOf != task.ID || child.ReplyTo != m.ID || calls.Load() != 3 || sends.Load() != 1 {
		t.Fatal(child, calls.Load(), sends.Load())
	}
	if err := s.retryReply(task.ID, m.Target); err == nil {
		t.Fatal("旧失败任务再次入队")
	}
}

func TestReplyRetryRevalidatesArchiveAndMode(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	task := retrySubmit(t, s, m)
	if err := s.retryReply(task.ID, "fixture-other-target"); err == nil {
		t.Fatal("目标未绑定")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "manual"; return nil }))
	if err := s.retryReply(task.ID, m.Target); err == nil {
		t.Fatal("人工接管后仍重试")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "auto"; st.Messages[0].Status = "revoked"; return nil }))
	if err := s.retryReply(task.ID, m.Target); err == nil {
		t.Fatal("撤回后仍重试")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = nil; return nil }))
	if err := s.retryReply(task.ID, m.Target); err == nil {
		t.Fatal("删除后仍重试")
	}
	if calls.Load() != 2 {
		t.Fatal("拒绝的重试调用了模型")
	}
}

func TestReplyRetryDoesNotRepeatUnknownDeliveryOrBypassGuard(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			var calls atomic.Int32
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-output"}}]}`)
			})
			var sends atomic.Int32
			s.host = replyRegressionHost(func(context.Context, control.Command) (any, error) {
				sends.Add(1)
				return nil, fmt.Errorf("fixture-unknown-delivery")
			})
			if blocked {
				replyRegressionCheck(t, s.store.change(func(st *State) error {
					st.Settings["prompt_guard.rules"] = `[{"id":"fixture-retry-output","name":"禁止测试输出","pattern":"fixture-output","source":"output","level":"high","action":"block","enabled":true}]`
					return nil
				}))
			}
			task := retrySubmit(t, s, m)
			if task.Status != "failed" || task.ReplyRetryable || task.AutoRetries != 0 || calls.Load() != 1 {
				t.Fatal(task, calls.Load())
			}
			if blocked && sends.Load() != 0 || !blocked && sends.Load() != 1 {
				t.Fatal("错误的微信操作次数", sends.Load())
			}
			if err := s.retryReply(task.ID, m.Target); err == nil {
				t.Fatal("不允许的失败被重新生成")
			}
		})
	}
}

func TestReplyRetryLegacyTraceRecovery(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("恢复来源不应调用模型") })
	trace, _ := json.Marshal(replyTrace{CurrentID: m.ID})
	account := s.store.snapshot().Account.ID
	replyRegressionCheck(t, s.store.putSecret("reply-trace:"+account+":fixture-legacy", string(trace)))
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Tasks = append(st.Tasks, Task{ID: "fixture-legacy", Source: "wechat", Kind: "chat", Target: m.Target, Status: "failed", Error: "模型响应读取失败或超过 32 MB"})
		return nil
	}))
	replyRegressionCheck(t, s.restoreReplyRetries())
	task := s.store.snapshot().Tasks[len(s.store.snapshot().Tasks)-1]
	if !task.ReplyRetryable || task.ReplyTo != m.ID {
		t.Fatal(task)
	}
	if strings.Contains(task.Input, m.Content) {
		t.Fatal("恢复关联不应复制聊天正文")
	}
}

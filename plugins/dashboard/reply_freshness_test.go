package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func expireReplyBatchForTest(s *Server, target string) {
	s.replyBatchMu.Lock()
	if b := s.replyBatches[target]; b != nil && !b.submitting {
		b.first = time.Now().Add(-groupReplyMaximum)
		select {
		case b.wake <- struct{}{}:
		default:
		}
	}
	s.replyBatchMu.Unlock()
}

func TestDraftAbsorbsPrivateAndGroupSupplement(t *testing.T) {
	for _, group := range []bool{false, true} {
		t.Run(fmt.Sprint(group), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					close(entered)
					<-release
					fmt.Fprint(w, `{"usage":{"prompt_tokens":11,"completion_tokens":7},"choices":[{"message":{"content":"不应发送的旧草稿"}}]}`)
					return
				}
				var body struct {
					Messages []struct{ Role, Content string }
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				input := body.Messages[len(body.Messages)-1].Content
				for _, text := range []string{"fixture-retry-input", "fixture-supplement"} {
					if strings.Count(input, text) != 1 {
						t.Error("合并后当前输入遗漏或重复", text, input)
					}
				}
				fmt.Fprint(w, `{"usage":{"prompt_tokens":11,"completion_tokens":7},"choices":[{"message":{"content":"已按补充统一回答"}}]}`)
			})
			if group {
				replyRegressionCheck(t, s.store.change(func(st *State) error {
					st.Contacts[0].Kind = "group"
					st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
					return nil
				}))
			}
			s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
				if c.Content != "已按补充统一回答" {
					t.Error("旧草稿被发送", c.Content)
				}
				sends.Add(1)
				return map[string]string{"id": "fixture-final-receipt"}, nil
			})
			id, err := s.queue(replyRequest(s.store.snapshot(), m))
			replyRegressionCheck(t, err)
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("未开始生成")
			}
			m.ID, m.Content = "fixture-supplement-id", "fixture-supplement"
			replyRegressionCheck(t, s.Record(m))
			expireReplyBatchForTest(s, m.Target)
			unblock()
			task := waitModelTask(t, s, id)
			if task.Status != "completed" || task.ReplyTo != m.ID || len(task.ReplyBatch) != 2 || sends.Load() != 1 || calls.Load() != 2 {
				t.Fatal(task, sends.Load(), calls.Load())
			}
			input, output := 0, 0
			for _, point := range s.store.snapshot().Trend {
				input += point.Input
				output += point.Output
			}
			if input != 22 || output != 14 {
				t.Fatal("作废草稿的模型用量没有统计", input, output)
			}
		})
	}
}

func TestNewMessageStopsRemainingPartsWithoutReplayingFirst(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		text := "第一段已经发送\n\n不应发送的第二段"
		if calls.Add(1) > 1 {
			text = "收到纠正后继续回答"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["reply.max_parts"] = float64(3)
		return nil
	}))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		n := sends.Add(1)
		if n == 1 {
			if c.Content != "第一段已经发送" {
				t.Error(c.Content)
			}
			next := m
			next.ID, next.Content = "fixture-correction", "刚才说错了，按这个补充"
			if err := s.Record(next); err != nil {
				t.Error(err)
			}
		} else if c.Content != "收到纠正后继续回答" {
			t.Error("旧段落重放或继续发送", c.Content)
		}
		return map[string]string{"id": fmt.Sprint("fixture-part-", n)}, nil
	})
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || task.ReplyRetryable || sends.Load() != 1 || !strings.Contains(task.Error, "已经发送的部分不会重发") {
		t.Fatal(task, sends.Load())
	}
	flushReplyBatchForTest(t, s, m.Target)
	st := s.store.snapshot()
	last := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if last.Status != "completed" || sends.Load() != 2 || calls.Load() != 2 {
		t.Fatal(last, sends.Load(), calls.Load())
	}
}

func TestFreshnessNeverReplaysSuccessfulWrite(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-write","function":{"name":"chatroom__SetName","arguments":"{\"content\":\"离线新群名\"}"}}]}}]}`)
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"已更新群名。"}}]}`)
		}
	})
	m.Target, m.Content = "fixture-write@chatroom", "把群名改为离线新群名"
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
	replyRegressionCheck(t, s.initBehaviors())
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts[0].ID = m.Target
		st.Contacts[0].Kind = "group"
		st.Messages = []Message{m}
		st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
		return nil
	}))
	var writes atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation == "group-name" {
			writes.Add(1)
			next := m
			next.ID, next.Content = "fixture-after-write", "然后聊点别的"
			if err := s.Record(next); err != nil {
				t.Error(err)
			}
		}
		return map[string]string{"id": "fixture-write-receipt"}, nil
	})
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || writes.Load() != 1 || calls.Load() != 2 {
		t.Fatal(task, writes.Load(), calls.Load())
	}
}

func TestPendingCorrectionDiscardsUnexecutedToolCall(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-obsolete-write","function":{"name":"chatroom__SetName","arguments":"{\"content\":\"离线新群名\"}"}}]}}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"好，按最新的意思聊"}}]}`)
	})
	m.Target, m.Content = "fixture-cancel-write@chatroom", "把群名改为离线新群名"
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
	replyRegressionCheck(t, s.initBehaviors())
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
		return nil
	}))
	id, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("没有开始生成")
	}
	m.ID, m.Content = "fixture-cancel-write", "不用改了，聊点别的"
	replyRegressionCheck(t, s.Record(m))
	expireReplyBatchForTest(s, m.Target)
	unblock()
	if task := waitModelTask(t, s, id); task.Status != "completed" || calls.Load() != 2 || sends.Load() != 1 {
		t.Fatal(task, calls.Load(), sends.Load())
	}
}

func TestBusyConversationRefreshIsBoundedAndKeepsAllInputs(t *testing.T) {
	var s *Server
	var m Message
	var calls atomic.Int32
	var sends *atomic.Int32
	s, m, sends = retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= maxReplyRefreshes+1 {
			next := m
			next.ID, next.Content = fmt.Sprint("fixture-busy-", n), fmt.Sprint("补充 ", n)
			if err := s.Record(next); err != nil {
				t.Error(err)
			}
			expireReplyBatchForTest(s, m.Target)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"已综合最新补充"}}]}`)
	})
	id, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	task := waitModelTask(t, s, id)
	if task.Status != "cancelled" || calls.Load() != maxReplyRefreshes+1 || sends.Load() != 0 {
		t.Fatal("刷新没有限次或发送了过时草稿", task, calls.Load(), sends.Load())
	}
	flushReplyBatchForTest(t, s, m.Target)
	st := s.store.snapshot()
	last := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if last.Status != "completed" || len(last.ReplyBatch) != maxReplyRefreshes+2 || sends.Load() != 1 {
		t.Fatal("移交下一批后遗漏了来信", last, sends.Load())
	}
}

// 构造已处于提交中的批次，验证草稿不会穿过入队交接的空隙。
func TestReplyWaitsForBatchSubmission(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	q := replyRequest(s.store.snapshot(), m)
	b := &groupReplyBatch{account: q.AccountID, epoch: q.AccountEpoch, target: q.Target, ids: []string{"fixture-transferring"}, submitting: true, submitted: make(chan struct{})}
	s.replyBatchMu.Lock()
	s.replyBatches = map[string]*groupReplyBatch{q.Target: b}
	s.replyBatchMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, changed, err := s.refreshPendingReply(ctx, "fixture-parent", q)
	if err != context.DeadlineExceeded || changed {
		t.Fatal("旧草稿从入队交接的间隙穿过", err, changed)
	}
}

func TestOperatorMessageReplacesUnsentDraft(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, `{"choices":[{"message":{"content":"不应追答的旧回复"}}]}`)
	})
	id, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("没有开始生成")
	}
	m.ID, m.Content, m.Self, m.Source = "fixture-operator-message", "这件事我来回答了", true, "wechat"
	replyRegressionCheck(t, s.Record(m))
	unblock()
	if task := waitModelTask(t, s, id); task.Status != "cancelled" || sends.Load() != 0 || !strings.Contains(task.Error, "人工发言") {
		t.Fatal(task, sends.Load())
	}
}

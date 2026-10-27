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

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestReplyDecisionRecordsReasonAtInfoAndDebugLevels(t *testing.T) {
	s, target := replyServer(t)
	before := s.store.snapshot()
	s.replyDecision(Message{ID: "fixture-info", Target: target}, "跳过", "测试")
	after := s.store.snapshot()
	if after.Revision != before.Revision+1 || len(after.Logs) != len(before.Logs)+1 || !strings.Contains(after.Logs[len(after.Logs)-1].Message, "原因：测试") || after.Logs[len(after.Logs)-1].Level != "INFO" {
		t.Fatal("INFO 下缺少不回复原因日志")
	}
	if err := s.store.change(func(st *State) error { st.Settings["logging.level"] = "DEBUG"; return nil }); err != nil {
		t.Fatal(err)
	}
	before = s.store.snapshot()
	s.replyDecision(Message{ID: "fixture-debug", Target: target}, "跳过", "测试")
	after = s.store.snapshot()
	if after.Revision != before.Revision+1 || len(after.Logs) != len(before.Logs)+1 || after.Logs[len(after.Logs)-1].Target != target {
		t.Fatal("DEBUG 下应保留原有判定日志")
	}
}

func TestOrdinaryReplyDoesNotQueueDuringCooldown(t *testing.T) {
	for _, status := range []string{"queued", "running", "completed"} {
		t.Run(status, func(t *testing.T) {
			s, target := replyServer(t)
			if err := s.store.change(func(st *State) error {
				st.Contacts[0].Kind = "friend"
				st.Tasks = nil
				st.Tasks = append(st.Tasks, Task{ID: "fixture-earlier", Source: "wechat", Kind: "chat", Target: target, Status: status, Created: time.Now().Format(time.RFC3339)})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s.autoReply(Message{ID: "fixture-later", Target: target, Kind: "text", Content: "再说一句"})
			if len(s.store.snapshot().Tasks) != 1 {
				t.Fatal("普通消息绕过了正在执行的回复或冷却时间")
			}
		})
	}
}

func TestReplyQueueIgnoresUnrelatedTaskAtSameTarget(t *testing.T) {
	s, target := replyServer(t)
	if err := s.store.change(func(st *State) error {
		st.Tasks = append(st.Tasks,
			Task{ID: "fixture-report", Source: "analysis", Kind: "insight", Target: target, Status: "running"},
			Task{ID: "fixture-waiting", Source: "wechat", Kind: "chat", Target: target, Status: "queued"},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Target: target, Conversation: &ai.Conversation{}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.waitReplyTurn(ctx, "fixture-waiting", q); err != nil {
		t.Fatal("无关的后台任务挡住了自动回复", err)
	}
}

func TestQueuedOrdinaryReplyRechecksCooldown(t *testing.T) {
	s, target := replyServer(t)
	if err := s.store.change(func(st *State) error {
		st.Tasks = append(st.Tasks,
			Task{ID: "fixture-completed", Source: "wechat", Kind: "chat", Target: target, Status: "completed", Created: time.Now().Format(time.RFC3339)},
			Task{ID: "fixture-waiting", Source: "wechat", Kind: "chat", Target: target, Status: "queued"},
		)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Target: target, Conversation: &ai.Conversation{}}
	if err := s.waitReplyTurn(context.Background(), "fixture-waiting", q); err == nil || !strings.Contains(err.Error(), "冷却") {
		t.Fatal("出队时绕过冷却", err)
	}
}

func TestGroupHundredPercentQueuesBatchesAndPreservesOrder(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			entered <- struct{}{}
			<-release
		}
		answer := "第一批已经收到，这里先说明事情的起因。"
		if n > 1 {
			answer = "补充的两个问题已收齐，这次一起讲后续方案。"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
	})
	m.Target = "fixture-queue@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = nil
		st.Settings["routing.concurrency"] = float64(1)
		st.Settings["reply.cooldown"] = float64(3600)
		st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
		return nil
	}))
	for i := 0; i < 3; i++ {
		m.ID, m.SenderID = fmt.Sprint("fixture-msg-", i), fmt.Sprint("fixture-member-", i)
		replyRegressionCheck(t, s.Record(m))
		flushReplyBatchForTest(t, s, m.Target)
		if i == 0 {
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("首条未开始")
			}
		}
	}
	st := s.store.snapshot()
	if len(st.Tasks) != 3 || st.Tasks[1].Status != "queued" || st.Tasks[2].Status != "queued" || calls.Load() != 1 {
		t.Fatal("忙时消息未排队或突破单槽限制", len(st.Tasks), calls.Load())
	}
	rows, err := loadRows[Task](s.store.db, st.Account.ID, "tasks")
	replyRegressionCheck(t, err)
	if len(rows) != 3 || rows[2].Status != "queued" {
		t.Fatal("排队任务未落盘")
	}
	unblock()
	first := waitModelTask(t, s, st.Tasks[0].ID)
	combined := waitModelTask(t, s, st.Tasks[1].ID)
	absorbed := waitModelTask(t, s, st.Tasks[2].ID)
	if first.Status != "completed" || first.ReplyTo != "fixture-msg-2" || len(first.ReplyBatch) != 3 || combined.Status != "cancelled" || combined.ReplyMergedInto != first.ID || absorbed.Status != "cancelled" || absorbed.ReplyMergedInto != first.ID {
		t.Fatal(first, combined, absorbed)
	}
	if sends.Load() != 1 || calls.Load() != 2 {
		t.Fatal("旧草稿外发或补充消息重复生成", sends.Load(), calls.Load())
	}
}

func TestVerifiedMentionBypassesProbabilityAndCooldownButNotPermissions(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-mention-answer"}}]}`)
	})
	m.Target = "fixture-mention@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = nil
		st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
		st.Settings["reply.cooldown"] = float64(3600)
		return nil
	}))
	m.ID, m.Content = "fixture-fake-at", "@本人 fixture text"
	replyRegressionCheck(t, s.Record(m))
	if len(s.store.snapshot().Tasks) != 0 {
		t.Fatal("昵称文本伪造了 @ 关系")
	}
	for i := 0; i < 2; i++ {
		m.ID, m.Mentions = fmt.Sprint("fixture-real-at-", i), []string{s.store.snapshot().Account.ID}
		replyRegressionCheck(t, s.Record(m))
		flushReplyBatchForTest(t, s, m.Target)
		st := s.store.snapshot()
		if len(st.Tasks) != i+1 {
			t.Fatal("真实 @ 未触发")
		}
		if task := waitModelTask(t, s, st.Tasks[i].ID); task.Status != "completed" {
			t.Fatal(task)
		}
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["reply.blacklist"] = m.Target; return nil }))
	m.ID = "fixture-blocked-at"
	replyRegressionCheck(t, s.Record(m))
	if len(s.store.snapshot().Tasks) != 2 || sends.Load() != 2 {
		t.Fatal("@ 绕过黑名单或重复发送")
	}
}

func TestQueuedReplyRechecksManualTakeover(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		fmt.Fprint(w, `{"choices":[{"message":{"content":"fixture-unused-answer"}}]}`)
	})
	first, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("首条未开始")
	}
	m.ID = "fixture-queued-takeover"
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = append(st.Messages, m); return nil }))
	second, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "manual"; return nil }))
	unblock()
	_ = waitModelTask(t, s, first)
	if task := waitModelTask(t, s, second); task.Status != "cancelled" || !strings.Contains(task.Error, "暂停") {
		t.Fatal(task)
	}
	if sends.Load() != 0 {
		t.Fatal("接管后仍发送")
	}
}

func TestSuccessfulToolSendsOneConfirmedAcknowledgement(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []struct{ Role string } }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Role == "tool" {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"已将群名改为“测试新群名”。"}}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-call","function":{"name":"chatroom__SetName","arguments":"{\"content\":\"测试新群名\"}"}}]}}]}`)
	})
	m.Target, m.Content = "fixture-tool@chatroom", "请把群名改为测试新群名"
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
	replyRegressionCheck(t, s.initBehaviors())
	var operations, sent atomic.Int32
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation == "group-name" {
			operations.Add(1)
		} else if c.Operation == "send" {
			sent.Add(1)
			if c.Content != "已更新群名。" {
				t.Error(c.Content)
			}
		} else {
			t.Error(c.Operation)
		}
		return map[string]string{"id": "fixture-tool-receipt"}, nil
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || operations.Load() != 1 || sent.Load() != 1 {
		t.Fatal(task, operations.Load(), sent.Load())
	}
}

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLaterBatchIncludesActuallyDeliveredSelfReply(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seen := false
		for _, turn := range body.Messages {
			if turn.Role == "assistant" && turn.Content == "知道了，路上小心，到了就好好休息。" {
				seen = true
			}
			if strings.Contains(turn.Content, "fixture-next-batch-not-yet") {
				t.Error("提前混入下一批来信")
			}
		}
		if !seen || !strings.Contains(body.Messages[len(body.Messages)-1].Content, "时序提醒") {
			t.Error("来信后已发出的本人回复被遗漏", body.Messages)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"NO_REPLY: 已有人回答"}}]}`)
	})
	m.Target = "fixture-order@chatroom"
	m.Time = time.Now().Add(-10 * time.Second).Format(time.RFC3339)
	sent := m
	sent.ID = "fixture-already-sent"
	sent.Self = true
	sent.Source = "ai"
	sent.Content = "知道了，路上小心，到了就好好休息。"
	sent.Time = time.Now().Add(-5 * time.Second).Format(time.RFC3339)
	later := m
	later.ID = "fixture-next-batch"
	later.Content = "fixture-next-batch-not-yet"
	later.Time = time.Now().Format(time.RFC3339)
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m, sent, later}
		st.Models[0].Context = 1
		return nil
	}))
	id, err := s.queue(batchReplyRequest(s.store.snapshot(), m, []string{m.ID}))
	replyRegressionCheck(t, err)
	task := waitModelTask(t, s, id)
	if task.Status != "completed" || task.Result != "本次无需回复" || sends.Load() != 0 {
		t.Fatal(task, sends.Load())
	}
}

func TestLeakedSilenceDoesNotCountAsAnswered(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	m.Target = "fixture-silence-history@chatroom"
	m.Time = time.Now().Add(-10 * time.Second).Format(time.RFC3339)
	for _, text := range []string{"[assistant]NO_REPLY: 非本人对话", "[NO_REPLY]", "没@我，我就不凑热闹了"} {
		bad := m
		bad.ID, bad.Self, bad.Source, bad.Content = "fixture-leaked-silence", true, "ai", text
		bad.Time = time.Now().Add(-5 * time.Second).Format(time.RFC3339)
		replyRegressionCheck(t, s.store.change(func(st *State) error {
			st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
			st.Messages = []Message{m, bad}
			return nil
		}))
		q, err := s.refreshReplyContext(batchReplyRequest(s.store.snapshot(), m, []string{m.ID}))
		replyRegressionCheck(t, err)
		if q.Conversation.RepliedAfterCurrent {
			t.Fatal("误发的沉默协议被当成已回答", text)
		}
		for _, item := range q.Context {
			if item.ID == bad.ID {
				t.Fatal("误发的控制内容仍进入后续回复示例")
			}
		}
	}
}

func TestRepeatedClosingIsSilentWithoutSuppressingNewConversation(t *testing.T) {
	for _, kind := range []string{"repeat", "split-repeat", "new-question", "new-member", "mention", "quote", "old", "game", "new-topic", "mixed-batch"} {
		t.Run(kind, func(t *testing.T) {
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
			m.Target, m.Content = "fixture-closing@chatroom", "回见，这就睡"
			before, own := m, m
			before.ID, before.Content = "fixture-closing-before", "晚安，早点睡"
			before.Time = time.Now().Add(-40 * time.Second).Format(time.RFC3339)
			own.ID, own.Self, own.Source, own.Content = "fixture-closing-self", true, "ai", "晚安，睡个好觉"
			own.Time = time.Now().Add(-30 * time.Second).Format(time.RFC3339)
			candidate := "回见，早点睡"
			switch kind {
			case "split-repeat":
				candidate = "回见\n\n睡个好觉"
			case "new-question":
				m.Content = "晚安，明天几点集合？"
			case "new-member":
				m.SenderID = "fixture-new-arrival"
			case "mention":
				m.Mentions = []string{s.store.snapshot().Account.ID}
			case "quote":
				m.Kind = "quote"
			case "old":
				own.Time = time.Now().Add(-11 * time.Minute).Format(time.RFC3339)
			case "game":
				own.Game = "turtlesoup"
			case "new-topic":
				own.Content = "新问题已经回答过了，换个话题吧"
			}
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{before, own, m}
				st.Settings["reply.max_parts"] = float64(3)
				return nil
			}))
			q, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
			replyRegressionCheck(t, err)
			if kind == "mixed-batch" {
				pending := q.Conversation.Current
				pending.Text = "再解释一下新方案"
				q.Conversation.Pending = append(q.Conversation.Pending, pending)
			}
			reason := s.duplicateReplyReason(q, candidate)
			wantSilent := kind == "repeat" || kind == "split-repeat"
			if (reason != "") != wantSilent || wantSilent && !strings.Contains(reason, "互道告别") {
				t.Fatal(kind, reason)
			}
		})
	}
}

func TestOrdinaryGroupDuplicateIsSilentButExplicitMentionStillWorks(t *testing.T) {
	for _, mentioned := range []bool{false, true} {
		t.Run(fmt.Sprint(mentioned), func(t *testing.T) {
			answer := "下次一起看看新方案吧，这个问题先讨论到这里。"
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
			})
			m.Target = "fixture-dedup@chatroom"
			own := m
			own.ID = "fixture-own-duplicate"
			own.Self = true
			own.Source = "ai"
			own.Content = answer
			own.Time = time.Now().Add(-30 * time.Second).Format(time.RFC3339)
			if mentioned {
				m.Mentions = []string{s.store.snapshot().Account.ID}
			}
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{own, m}
				return nil
			}))
			task := retrySubmit(t, s, m)
			expected := int32(0)
			if mentioned {
				expected = 1
			}
			if task.Status != "completed" || sends.Load() != expected || calls.Load() != 1 {
				t.Fatal(task, sends.Load(), calls.Load())
			}
			if !mentioned {
				found := false
				for _, log := range s.store.snapshot().Logs {
					found = found || strings.Contains(log.Message, "程序检查：与本人近期已发送消息 fixture-own-duplicate")
				}
				if !found || task.Result != "本次无需回复" {
					t.Fatal("防重未记录来源和原因")
				}
			}
		})
	}
}

func TestDuplicateCheckPreservesChangedFactsAndShortReplies(t *testing.T) {
	for _, pair := range [][2]string{
		{"可以", "可以"},
		{"今天气温预计是21度，明天情况再看", "今天气温预计是22度，明天情况再看"},
		{"这个提议我赞成，后续可以继续推进讨论", "这个提议我不赞成，后续可以继续推进讨论"},
	} {
		if sameRecentReply(replyFingerprint(pair[0]), replyFingerprint(pair[1])) {
			t.Fatal("不同事实或短答复被误判", pair)
		}
	}
	if !sameRecentReply(replyFingerprint("这个方案已经说过了，下次再一起讨论。"), replyFingerprint("这个方案已经说过了！下次再一起讨论")) {
		t.Fatal("标点变化绕过了明确复读检查")
	}
}

func TestMergedQueuedReplyCanRetryAllSources(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"统一回答补充内容。"}}]}`)
	})
	m.Target = "fixture-coalesced-retry@chatroom"
	first := m
	first.ID = "fixture-coalesced-first"
	st := s.store.snapshot()
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{first, m}
		st.Tasks = []Task{{ID: "fixture-parent", Source: "wechat", Kind: "chat", Target: m.Target, Status: "queued", ReplyTo: first.ID, ReplyBatch: []string{first.ID}}, {ID: "fixture-child", Source: "wechat", Kind: "chat", Target: m.Target, Status: "queued", ReplyTo: m.ID, ReplyBatch: []string{m.ID}}}
		return nil
	}))
	s.taskMu.Lock()
	s.taskWaiting["fixture-child"] = true
	s.taskMu.Unlock()
	q, err := s.coalesceQueuedReplies("fixture-parent", batchReplyRequest(s.store.snapshot(), first, []string{first.ID}))
	replyRegressionCheck(t, err)
	s.taskMu.Lock()
	delete(s.taskWaiting, "fixture-child")
	s.taskMu.Unlock()
	s.runTask(s.ctx, "fixture-parent", q)
	failed := s.store.snapshot().Tasks[0]
	if failed.Status != "failed" || !failed.ReplyRetryable || len(failed.ReplyBatch) != 2 || st.Account.ID != q.AccountID {
		t.Fatal(failed)
	}
	replyRegressionCheck(t, s.retryReply("fixture-parent", m.Target))
	tasks := s.store.snapshot().Tasks
	got := waitModelTask(t, s, tasks[len(tasks)-1].ID)
	if got.Status != "completed" || len(got.ReplyBatch) != 2 || calls.Load() != 3 {
		t.Fatal(got, calls.Load())
	}
	if tasks[1].ReplyMergedInto != "fixture-parent" {
		t.Fatal("合并任务来源关系丢失")
	}
}

func TestCancelledQueuedTaskIsNotCoalesced(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	m.Target = "fixture-coalesce-cancelled@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		st.Tasks = []Task{{ID: "fixture-active", Source: "wechat", Kind: "chat", Status: "queued", Target: m.Target}, {ID: "fixture-cancelled", Source: "wechat", Kind: "chat", Status: "cancelled", Target: m.Target, ReplyTo: "fixture-not-valid"}}
		return nil
	}))
	s.taskMu.Lock()
	s.taskWaiting["fixture-cancelled"] = true
	s.taskMu.Unlock()
	q, err := s.coalesceQueuedReplies("fixture-active", batchReplyRequest(s.store.snapshot(), m, []string{m.ID}))
	replyRegressionCheck(t, err)
	s.taskMu.Lock()
	delete(s.taskWaiting, "fixture-cancelled")
	s.taskMu.Unlock()
	if len(q.BatchIDs) != 1 || s.store.snapshot().Tasks[1].ReplyMergedInto != "" {
		t.Fatal("手动取消的任务被重新并入")
	}
}

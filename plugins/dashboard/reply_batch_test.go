package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func flushReplyBatchForTest(t *testing.T, s *Server, target string) {
	t.Helper()
	s.replyBatchMu.Lock()
	b := s.replyBatches[target]
	var deadline time.Time
	if b != nil {
		deadline = b.deadline()
	}
	s.replyBatchMu.Unlock()
	if b == nil {
		t.Fatal("缺少待合并消息", target)
	}
	if !s.flushGroupReply(b, deadline) {
		t.Fatal("到期未刷新")
	}
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func TestGroupReplyDeadlineExtendsButNeverBeyondFifteenSeconds(t *testing.T) {
	start := time.Now()
	b := &groupReplyBatch{first: start, last: start}
	if !b.deadline().Equal(start.Add(7 * time.Second)) {
		t.Fatal(b.deadline())
	}
	b.last = start.Add(6 * time.Second)
	if !b.deadline().Equal(start.Add(13 * time.Second)) {
		t.Fatal(b.deadline())
	}
	b.last = start.Add(12 * time.Second)
	if !b.deadline().Equal(start.Add(15 * time.Second)) {
		t.Fatal(b.deadline())
	}
}

func TestGroupReplyCombinesDifferentMembersAndKeepsCurrentExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		var input string
		for _, v := range body.Messages {
			if v.Role == "user" {
				input += v.Content + "\n"
			}
		}
		for _, text := range []string{"fixture-first-body", "fixture-second-body", "fixture-third-body"} {
			if strings.Count(input, text) != 1 {
				t.Error("消息遗漏或重复", text, input)
			}
		}
		if !strings.Contains(input, "本轮待回复消息") || !strings.Contains(input, "成员甲：fixture-first-body") || !strings.Contains(input, "成员乙：fixture-second-body") {
			t.Error(input)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"收到补充后统一回答。"}}]}`)
	})
	m.Target = "fixture-batch@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Messages = nil
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
		st.Models[0].Context = 1 // 近期历史限制不应截掉同批待回复的消息。
		return nil
	}))
	for i, body := range []string{"fixture-first-body", "fixture-second-body", "fixture-third-body"} {
		m.ID = fmt.Sprint("fixture-batch-", i)
		m.Content = body
		m.SenderID, m.Sender = "fixture-member-a", "成员甲"
		if i == 1 {
			m.SenderID, m.Sender = "fixture-member-b", "成员乙"
		}
		replyRegressionCheck(t, s.Record(m))
	}
	if len(s.store.snapshot().Tasks) != 0 || calls.Load() != 0 {
		t.Fatal("未等待补充即生成")
	}
	flushReplyBatchForTest(t, s, m.Target)
	st := s.store.snapshot()
	if len(st.Tasks) != 1 || len(st.Tasks[0].ReplyBatch) != 3 {
		t.Fatal(st.Tasks)
	}
	got := waitModelTask(t, s, st.Tasks[0].ID)
	if got.Status != "completed" || calls.Load() != 1 || sends.Load() != 1 {
		t.Fatal(got, calls.Load(), sends.Load())
	}
}

func TestBatchEvidenceAndManualTakeoverAreRechecked(t *testing.T) {
	for _, takeover := range []bool{false, true} {
		t.Run(fmt.Sprint(takeover), func(t *testing.T) {
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
			m.Target = "fixture-batch-safety@chatroom"
			first := m
			first.ID = "fixture-batch-earlier"
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{first, m}
				st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
				return nil
			}))
			q, err := s.refreshReplyContext(batchReplyRequest(s.store.snapshot(), m, []string{first.ID, m.ID}))
			replyRegressionCheck(t, err)
			replyRegressionCheck(t, s.validateReplyEvidence(q))
			s.accumulateGroupReply(s.store.snapshot(), m, time.Now())
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				if takeover {
					st.Contacts[0].Mode = "manual"
				} else {
					st.Messages[0].Status = "revoked"
					st.Messages[1].Status = "revoked"
				}
				return nil
			}))
			if !takeover && s.validateReplyEvidence(q) == nil {
				t.Fatal("撤回批次证据仍有效")
			}
			flushReplyBatchForTest(t, s, m.Target)
			if len(s.store.snapshot().Tasks) != 0 {
				t.Fatal("接管或撤回后仍排队")
			}
		})
	}
}

func TestHundredPercentStillAllowsModelSilence(t *testing.T) {
	for _, marker := range []string{"NO_REPLY", "No_REPLY", "no_reply", "[silent]"} {
		t.Run(marker, func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": marker}}}})
			})
			m.Target = "fixture-silent-batch@chatroom"
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = nil
				st.Overrides[m.Target] = map[string]any{"probability": float64(100)}
				return nil
			}))
			replyRegressionCheck(t, s.Record(m))
			flushReplyBatchForTest(t, s, m.Target)
			got := waitModelTask(t, s, s.store.snapshot().Tasks[0].ID)
			if got.Status != "completed" || got.Result != "本次无需回复" || sends.Load() != 0 || calls.Load() != 1 || got.AutoRetries != 0 {
				t.Fatal(got, sends.Load(), calls.Load())
			}
		})
	}
}

func TestReplyCorrectsIdentityAndUnavailableMediaBeforeSending(t *testing.T) {
	for _, mode := range []string{"identity", "image"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				text := "是我理解错了，你说的是另一种口味。"
				if calls.Add(1) == 1 {
					if mode == "identity" {
						text = "我理解错了。离线本人你赶紧出门去吧。"
					} else {
						text = `<image prompt="健身照片"/>`
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.Name = "离线本人"; return nil }))
			got := retrySubmit(t, s, m)
			if got.Status != "completed" || calls.Load() != 2 || sends.Load() != 1 || got.AutoRetries != 1 {
				t.Fatal(got, calls.Load(), sends.Load())
			}
		})
	}
}

func TestInvalidToolReferenceCanRecoverWithoutAnyWrite(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-call","function":{"name":"chatroom__SetName","arguments":"{\"target_ref\":\"ref_missing\",\"content\":\"fixture-name\"}"}}]}}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"那就先聊到这儿。"}}]}`)
	})
	m.Target = "fixture-invalid-ref@chatroom"
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
	replyRegressionCheck(t, s.initBehaviors())
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	got := retrySubmit(t, s, m)
	if got.Status != "completed" || calls.Load() != 2 || sends.Load() != 1 {
		t.Fatal(got, calls.Load(), sends.Load())
	}
	var writes int
	replyRegressionCheck(t, s.store.db.QueryRow("select count(*) from ai_action_records where task=?", got.ID).Scan(&writes))
	if writes != 0 {
		t.Fatal("错误引用产生了写入")
	}
}

func TestBatchHistoryDoesNotIncludeMessagesFromNextBatch(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	earlier := m
	earlier.ID = "fixture-batch-old"
	earlier.Content = "earlier"
	later := m
	later.ID = "fixture-batch-later"
	later.Content = "later"
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{earlier, m, later}; return nil }))
	q, err := s.refreshReplyContext(batchReplyRequest(s.store.snapshot(), m, []string{earlier.ID, m.ID}))
	replyRegressionCheck(t, err)
	if len(q.Context) != 0 || len(q.Conversation.Pending) != 1 {
		t.Fatal(q.Context, q.Conversation.Pending)
	}
}

func TestSelfAddressCheckPreservesQuotationAndCode(t *testing.T) {
	c := &ai.Conversation{SelfName: "离线本人"}
	for _, text := range []string{"我就是离线本人。", "他说“离线本人你去吧”。", "> 离线本人你去吧", "示例：\n```text\n离线本人你去吧\n```"} {
		if err := replyQualityError(text, c); err != nil {
			t.Fatal(text, err)
		}
	}
	if err := replyQualityError("离线本人你去吧", c); err == nil {
		t.Fatal("身份串用未识别")
	}
}

func TestSilenceCannotExecuteTools(t *testing.T) {
	s, _, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	_, report, err := filterSafetyOutput(s.store.snapshot(), ai.Request{Kind: "chat", Source: "wechat"}, ai.Result{Text: "No_REPLY", ToolCalls: []ai.ToolCall{{Name: "fixture", Arguments: "{}"}}}, "")
	if err == nil || !report.Silent {
		t.Fatal(report, err)
	}
}

func TestBatchManualRetryRebuildsAllOriginalMessages(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		text := ""
		for _, v := range body.Messages {
			if v.Role == "user" {
				text += v.Content
			}
		}
		if strings.Count(text, "fixture-batch-retry-first") != 1 || strings.Count(text, "fixture-batch-retry-last") != 1 {
			t.Error("重试丢失合并来源", text)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"统一回复。"}}]}`)
	})
	m.Target = "fixture-batch-retry@chatroom"
	first := m
	first.ID = "fixture-batch-retry-0"
	first.Content = "fixture-batch-retry-first"
	m.Content = "fixture-batch-retry-last"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{first, m}
		return nil
	}))
	id, err := s.queue(batchReplyRequest(s.store.snapshot(), m, []string{first.ID, m.ID}))
	replyRegressionCheck(t, err)
	failed := waitModelTask(t, s, id)
	if failed.Status != "failed" || !failed.ReplyRetryable {
		t.Fatal(failed)
	}
	replyRegressionCheck(t, s.retryReply(id, m.Target))
	st := s.store.snapshot()
	got := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if got.Status != "completed" || len(got.ReplyBatch) != 2 || calls.Load() != 3 || sends.Load() != 1 {
		t.Fatal(got, calls.Load(), sends.Load())
	}
}

func TestToolCorrectionFitsBudgetByRemovingOlderChatOnly(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		receipt := false
		for _, v := range body.Messages {
			if v.Role == "tool" && strings.Contains(v.Content, "invalid_reference") {
				receipt = true
			}
			if strings.Contains(v.Content, "fixture-old-budget") {
				t.Error("未给纠正预留上下文")
			}
		}
		if !receipt {
			t.Error("裁掉了本轮工具回执")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"继续聊吧。"}}]}`)
	})
	m.Target = "fixture-budget-correction@chatroom"
	old := m
	old.ID = "fixture-older-budget"
	old.Content = strings.Repeat("fixture-old-budget ", 200)
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
	replyRegressionCheck(t, s.initBehaviors())
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{old, m}
		return nil
	}))
	st := s.store.snapshot()
	q, err := s.refreshReplyContext(replyRequest(st, m))
	replyRegressionCheck(t, err)
	q.Tools = s.behaviorToolsFor(q)
	c := ai.Config{Endpoint: st.Models[0].Endpoint, Provider: "openai", Key: "fixture-key", Model: "fixture", Timeout: time.Second, MaxOutput: 128, ContextBudget: ai.ConversationSize(q)}
	result, sent, err := s.runActionLoop(context.Background(), "fixture-correction-budget", q, c, ai.Result{ToolCalls: []ai.ToolCall{{ID: "fixture-budget-call", Name: "chatroom__SetName", Arguments: `{"target_ref":"ref_missing","content":"fixture-name"}`}}}, "")
	replyRegressionCheck(t, err)
	if sent || result.Text != "继续聊吧。" || calls.Load() != 1 {
		t.Fatal(result, sent, calls.Load())
	}
}

func TestMentionBatchIncludesFollowUpEvenAtZeroProbability(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"收到完整问题。"}}]}`)
	})
	m.Target = "fixture-mention-batch@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = nil
		st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
		st.Settings["reply.cooldown"] = float64(3600)
		return nil
	}))
	m.Mentions = []string{s.store.snapshot().Account.ID}
	m.ID = "fixture-batch-mention"
	replyRegressionCheck(t, s.Record(m))
	m.Mentions = nil
	m.ID = "fixture-batch-followup"
	m.SenderID = "fixture-another-member"
	m.Content = "补充前一个问题"
	replyRegressionCheck(t, s.Record(m))
	flushReplyBatchForTest(t, s, m.Target)
	st := s.store.snapshot()
	if len(st.Tasks) != 1 || len(st.Tasks[0].ReplyBatch) != 2 {
		t.Fatal("补充被概率规则丢弃", st.Tasks)
	}
	got := waitModelTask(t, s, st.Tasks[0].ID)
	if got.Status != "completed" || sends.Load() != 1 {
		t.Fatal(got, sends.Load())
	}
}

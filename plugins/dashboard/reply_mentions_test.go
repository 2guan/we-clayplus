package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOtherGroupMentionsTakePriorityOverProbability(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		mentions                          []string
		probability                       float64
		disabled, all, private, wantReply bool
	}{
		{name: "default-100-other", mentions: []string{"fixture-other"}, probability: 100},
		{name: "disabled-100-other", mentions: []string{"fixture-other"}, probability: 100, disabled: true, wantReply: true},
		{name: "also-mentions-self-zero", mentions: []string{"fixture-other", "fixture-retry-self"}, probability: 0, wantReply: true},
		{name: "all-members", mentions: []string{"fixture-other"}, probability: 100, all: true, wantReply: true},
		{name: "plain-nickname", probability: 100, wantReply: true},
		{name: "private-unaffected", mentions: []string{"fixture-other"}, private: true, wantReply: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"正常回复"}}]}`))
			})
			kind := "group"
			m.Target = "fixture-mentions@chatroom"
			if tc.private {
				kind, m.Target = "friend", "fixture-person"
			}
			m.Mentions, m.MentionAll = tc.mentions, tc.all
			m.Content = "@示例昵称 测试消息"
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: kind, Mode: "auto"}}
				st.Messages = nil
				st.Overrides[m.Target] = map[string]any{"probability": tc.probability}
				st.Settings["logging.level"] = "DEBUG"
				if tc.disabled {
					st.Settings["reply.skip_other_mentions"] = false
				}
				return nil
			}))
			replyRegressionCheck(t, s.Record(m))
			if tc.wantReply && !tc.private {
				flushReplyBatchForTest(t, s, m.Target)
			}
			st := s.store.snapshot()
			if !tc.wantReply {
				if len(st.Tasks) != 0 || calls.Load() != 0 || sends.Load() != 0 {
					t.Fatal("仅 @ 他人仍生成或发送")
				}
				found := false
				for _, l := range st.Logs {
					found = found || strings.Contains(l.Message, "消息仅 @ 其他成员")
				}
				if !found {
					t.Fatal("未记录跳过原因")
				}
				return
			}
			if len(st.Tasks) != 1 {
				t.Fatal("符合条件的消息未排队")
			}
			got := waitModelTask(t, s, st.Tasks[0].ID)
			if got.Status != "completed" || calls.Load() != 1 || sends.Load() != 1 {
				t.Fatal(got, calls.Load(), sends.Load())
			}
		})
	}
}

func TestReplyPlanningRetriesWithoutLeakingAndCanStaySilent(t *testing.T) {
	for _, silence := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject-both-attempts", true: "retry-then-silent"}[silence], func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []struct{ Role, Content string }
				}
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				found := false
				for _, v := range body.Messages {
					found = found || v.Role == "system" && strings.Contains(v.Content, "整段只返回 NO_REPLY")
				}
				if !found {
					t.Error("请求缺少明确沉默协议")
				}
				answer := "I notice the current message targets a different participant. I should stay quiet instead of composing a reply.\n\nI will not join this exchange."
				if calls.Add(1) == 2 && silence {
					answer = "NO_REPLY"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
			})
			task := retrySubmit(t, s, m)
			if calls.Load() != 2 || sends.Load() != 0 || task.AutoRetries != 1 {
				t.Fatal("规划旁白被发送或未正确重试", task, calls.Load(), sends.Load())
			}
			if silence {
				if task.Status != "completed" || task.Result != "本次无需回复" {
					t.Fatal(task)
				}
			} else if task.Status != "failed" || !task.ReplyRetryable || !strings.Contains(task.Error, "回复判断或规划") {
				t.Fatal(task)
			}
		})
	}
}

func TestOtherMentionRuleRechecksQueuedEvidence(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	m.Target, m.Mentions = "fixture-policy@chatroom", []string{"fixture-other"}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		st.Settings["reply.skip_other_mentions"] = false
		return nil
	}))
	q, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["reply.skip_other_mentions"] = true; return nil }))
	if _, err = s.refreshReplyContext(q); err == nil {
		t.Fatal("排队任务未复验新规则")
	}
	if err = s.validateReplyEvidence(q); err == nil {
		t.Fatal("发送前未复验新规则")
	}
}

func TestDirectGroupTriggerBypassesProbabilityAndCooldown(t *testing.T) {
	for _, tc := range []struct {
		name                                                                string
		mention, quote, other, cross, unavailable, fakeName, followup, want bool
	}{
		{name: "protocol-self-mention", mention: true, want: true},
		{name: "verified-self-quote", quote: true, want: true},
		{name: "quote-in-pending-batch", quote: true, followup: true, want: true},
		{name: "quote-other-member", quote: true, other: true},
		{name: "quote-other-conversation", quote: true, cross: true},
		{name: "unavailable-self-quote", quote: true, unavailable: true},
		{name: "forged-quote-display-name", quote: true, other: true, fakeName: true},
		{name: "quote-self-but-address-other", quote: true, mention: true, other: true},
		{name: "typed-nickname-is-not-mention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if tc.quote {
					found := false
					for _, turn := range body.Messages {
						found = found || strings.Contains(turn.Content, "〔引用本人消息〕")
					}
					if !found {
						t.Error("模型请求缺失引用本人标记")
					}
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"看到了，这次直接回应你的问题"}}]}`))
			})
			m.Target, m.Content = "fixture-direct@chatroom", "@示例昵称 你怎么看？"
			original := Message{ID: "fixture-direct-original", Target: m.Target, Sender: "本人昵称", Self: true, Source: "ai", Kind: "text", Content: "这是本人之前说的话", Status: "sent", Time: m.Time}
			if tc.other && !tc.mention {
				original.Self, original.SenderID = false, "fixture-other"
			}
			if tc.cross {
				original.Target = "fixture-another@chatroom"
			}
			if tc.mention {
				m.Mentions = []string{s.store.snapshot().Account.ID}
				if tc.other {
					m.Mentions = []string{"fixture-other"}
				}
			}
			if tc.quote {
				m.Kind = "quote"
				m.Rich = &RichMessage{QuoteID: original.ID, Author: original.Sender, Quote: original.Content, QuoteUnavailable: tc.unavailable}
				if tc.fakeName {
					m.Rich.Author = s.store.snapshot().Account.Name
				}
			}
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{original}
				st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
				st.Settings["reply.cooldown"] = float64(3600)
				st.Tasks = []Task{{ID: "fixture-previous-task", Target: m.Target, Kind: "chat", Source: "wechat", Status: "completed", Created: time.Now().Format(time.RFC3339)}}
				return nil
			}))
			replyRegressionCheck(t, s.Record(m))
			if tc.followup {
				next := m
				next.ID, next.Content, next.Kind, next.Rich, next.Mentions = "fixture-direct-next", "再补充一点", "text", nil, nil
				replyRegressionCheck(t, s.Record(next))
			}
			if !tc.want {
				if s.hasGroupReplyBatch(s.store.snapshot(), m.Target) || len(s.store.snapshot().Tasks) != 1 || calls.Load() != 0 {
					t.Fatal("非本人指向或不可用引用越过触发规则")
				}
				return
			}
			flushReplyBatchForTest(t, s, m.Target)
			st := s.store.snapshot()
			if len(st.Tasks) != 2 {
				t.Fatal("直接指向没有排队", len(st.Tasks))
			}
			task := waitModelTask(t, s, st.Tasks[1].ID)
			if task.Status != "completed" || calls.Load() != 1 || sends.Load() != 1 {
				t.Fatal(task, calls.Load(), sends.Load())
			}
			trigger := "@ 本人"
			if tc.quote {
				trigger = "引用本人消息"
			}
			found := false
			for _, l := range s.store.snapshot().Logs {
				found = found || strings.Contains(l.Message, "自动回复等待合并；原因："+trigger)
			}
			if !found {
				t.Fatal("缺少直接触发原因日志")
			}
		})
	}
}

package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

func TestStandaloneOperationHintDoesNotStartAnotherChatTurn(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"NO_REPLY: 无需补充"}}]}`)
	})
	m.Content = (writeIntentMissing{}).Error()
	s.autoReply(m)
	st := s.store.snapshot()
	if len(st.Tasks) != 0 || calls.Load() != 0 || sends.Load() != 0 {
		t.Fatal("把独立校验提示变成新的聊天任务")
	}
	found := false
	for _, log := range st.Logs {
		found = found || log.Level == "INFO" && log.Target == m.Target && strings.Contains(log.Message, "仅为操作参数校验提示")
	}
	if !found {
		t.Fatal("没有记录跳过原因")
	}
	m.ID, m.Content = "fixture-question-about-hint", "为什么会出现这句提示："+m.Content
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	s.autoReply(m)
	st = s.store.snapshot()
	if len(st.Tasks) == 0 {
		t.Fatal("关于校验提示的真实问题也被跳过")
	}
	task := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if task.Status != "completed" || calls.Load() != 1 {
		t.Fatal(task, calls.Load())
	}
}

func TestReplySilenceReasonIsLoggedWithoutSending(t *testing.T) {
	for _, tc := range []struct{ marker, reason string }{
		{"NO_REPLY: 非本人对话", "模型判断：非本人对话"},
		{"No_REPLY：话题已结束", "模型判断：话题已结束"},
		{"[assistant]NO_REPLY: 非本人对话", "模型判断：非本人对话"},
		{"[assistant] NO_REPLY：重复附和", "模型判断：重复附和"},
		{"[assistant]: [NO_REPLY]: 话题已结束", "模型判断：话题已结束"},
		{"[NO_REPLY] 沉默原因：重复告别、话题已结束", "模型判断：话题已结束"},
		{"NO_REPLY\n原因：话题已结束", "模型判断：话题已结束"},
		{"NO_REPLY", "模型未提供具体原因"},
		{"NO_REPLY: fixture-private-text-not-a-reason", "模型未提供具体原因"},
	} {
		t.Run(tc.marker, func(t *testing.T) {
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, tc.marker)
			})
			got := retrySubmit(t, s, m)
			if got.Status != "completed" || got.Result != "本次无需回复" || sends.Load() != 0 {
				t.Fatal(got, sends.Load())
			}
			found := false
			for _, log := range s.store.snapshot().Logs {
				if log.Target == m.Target && strings.Contains(log.Message, "自动回复模型沉默") && strings.Contains(log.Message, got.ID) {
					found = true
					if log.Level != "INFO" || !strings.Contains(log.Message, tc.reason) || !strings.Contains(log.Message, m.ID) || strings.Contains(log.Message, "fixture-private-text-not-a-reason") {
						t.Fatal(log)
					}
				}
			}
			if !found {
				t.Fatal("缺少模型不回复原因")
			}
		})
	}
}

func TestAutomaticReplySkipBranchesHaveReasonsAtInfo(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(*State, *Message)
	}{
		{"self", "本人发送", func(st *State, m *Message) { m.Self = true }},
		{"type", "消息类型", func(st *State, m *Message) { m.Kind = "call" }},
		{"media", "自动识别已关闭", func(st *State, m *Message) { m.Kind = "audio"; st.Settings["audio.auto"] = false }},
		{"switch", "总开关已关闭", func(st *State, m *Message) { st.Settings["reply.enabled"] = false }},
		{"offline", "账号未在线", func(st *State, m *Message) { st.Account.Status = "offline" }},
		{"manual", "人工或暂停", func(st *State, m *Message) { st.Contacts[0].Mode = "manual" }},
		{"private", "私聊自动回复触发已关闭", func(st *State, m *Message) { st.Settings["reply.private"] = "关闭" }},
		{"keyword", "未匹配触发词", func(st *State, m *Message) {
			st.Settings["reply.private"] = "关键词"
			st.Settings["reply.keywords"] = "fixture-no-match"
		}},
		{"probability", "未命中普通群消息回复概率", func(st *State, m *Message) {
			st.Contacts[0].Kind = "group"
			st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
		}},
		{"cooldown", "冷却时间", func(st *State, m *Message) {
			st.Tasks = []Task{{ID: "fixture-completed", Source: "wechat", Kind: "chat", Target: m.Target, Status: "completed", Created: time.Now().Format(time.RFC3339)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
			replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["logging.level"] = "INFO"; tc.change(st, &m); return nil }))
			before := len(s.store.snapshot().Logs)
			s.autoReply(m)
			logs := s.store.snapshot().Logs
			if tc.name == "switch" || tc.name == "manual" || tc.name == "private" {
				if len(logs) != before || sends.Load() != 0 {
					t.Fatal("未开启智能回复的会话仍记录例行日志")
				}
				return
			}
			if len(logs) <= before || sends.Load() != 0 {
				t.Fatal("未记录跳过原因")
			}
			log := logs[len(logs)-1]
			if log.Level != "INFO" || log.Target != m.Target || !strings.Contains(log.Message, tc.reason) || !strings.Contains(log.Message, m.ID) || strings.Contains(log.Message, m.Content) {
				t.Fatal(log)
			}
		})
	}
}

func TestReplyFailureHasTaskAndSourceInLog(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	got := retrySubmit(t, s, m)
	found := false
	for _, log := range s.store.snapshot().Logs {
		if strings.Contains(log.Message, "自动回复未完成") && strings.Contains(log.Message, got.ID) {
			found = log.Level == "ERROR" && strings.Contains(log.Message, m.ID) && strings.Contains(log.Message, "HTTP 503")
		}
	}
	if got.Status != "failed" || !found {
		t.Fatal(got, "缺少完整失败关联")
	}
}

func TestReplyBatchCancellationAndAccountIsolation(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	m.Target = "fixture-log-batch@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	st := s.store.snapshot()
	s.accumulateGroupReply(st, m, time.Now())
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "manual"; return nil }))
	flushReplyBatchForTest(t, s, m.Target)
	logs := s.store.snapshot().Logs
	if !strings.Contains(logs[len(logs)-1].Message, "等待合并期间停止：当前会话处于人工或暂停模式") {
		t.Fatal(logs[len(logs)-1])
	}
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Target: m.Target, BatchIDs: []string{m.ID}}
	q.AccountEpoch++
	before := len(logs)
	s.recordReplyDecision(q, "fixture-old-task", "INFO", "取消", "fixture-old-account-log")
	if len(s.store.snapshot().Logs) != before {
		t.Fatal("旧账号日志混入当前账号")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.waitReplyTurn(ctx, "fixture-cancel", q); err == nil {
		t.Fatal("取消未生效")
	}
}

func TestReplyLogAppendDoesNotRewriteArchiveAndRollsBack(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	st := s.store.snapshot()
	_, err := s.store.db.Exec(`CREATE TRIGGER fixture_no_archive_rewrite BEFORE UPDATE ON state_rows BEGIN SELECT RAISE(FAIL,'archive must not be rewritten'); END`)
	replyRegressionCheck(t, err)
	s.replyDecision(m, "跳过", "fixture-append-reason")
	got := s.store.snapshot()
	if got.Revision != st.Revision+1 || got.Messages[0].Content != m.Content {
		t.Fatal("日志未独立提交或改写聊天归档")
	}
	_, err = s.store.db.Exec(`CREATE TRIGGER fixture_log_failure BEFORE INSERT ON state_logs BEGIN SELECT RAISE(FAIL,'log failure'); END`)
	replyRegressionCheck(t, err)
	written, err := s.store.appendLog(st.Account.ID, st.AccountEpoch, "INFO", "ai", m.Target, "fixture-failed-log")
	after := s.store.snapshot()
	if err == nil || written || after.Revision != got.Revision || len(after.Logs) != len(got.Logs) {
		t.Fatal("数据库失败发布了虚假日志")
	}
}

func TestReplyInactiveSessionsDoNotEmitRoutineLogs(t *testing.T) {
	for _, mode := range []string{"manual", "paused", "missing", "ai-disabled", "global-disabled", "blacklisted"} {
		t.Run(mode, func(t *testing.T) {
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				switch mode {
				case "manual", "paused":
					st.Contacts[0].Mode = mode
				case "missing":
					st.Contacts = nil
				case "ai-disabled":
					for i := range st.Plugins {
						if st.Plugins[i].ID == "ai" {
							st.Plugins[i].Enabled = false
						}
					}
				case "global-disabled":
					st.Settings["reply.enabled"] = false
				case "blacklisted":
					st.Settings["reply.blacklist"] = m.Target
				}
				return nil
			}))
			before := len(s.store.snapshot().Logs)
			for _, kind := range []string{"text", "audio", "call"} {
				m.Kind = kind
				s.autoReply(m)
			}
			if len(s.store.snapshot().Logs) != before {
				t.Fatal("未开启会话出现日志")
			}
		})
	}
}

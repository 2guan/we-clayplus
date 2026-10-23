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
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestWriteIntentUsesOnlyCurrentOriginalText(t *testing.T) {
	s := newServer(t, true)
	for _, tc := range []struct {
		name, operation, kind, text, value string
		mentions                           []string
		allow                              bool
	}{
		{"group-name", "chatroom.SetName", "text", "请把群名改为离线新名称", "离线新名称", nil, true},
		{"create-and-name", "chatroom.SetName", "text", "用两位好友建群并命名为离线新名称，然后邀请另一位", "离线新名称", nil, true},
		{"announcement", "chatroom.SetAnnouncement", "text", "请发布群公告：明天开会", "明天开会", nil, true},
		{"remark", "contact.SetRemark", "text", "请把备注改成有福", "有福", nil, true},
		{"other-value", "chatroom.SetName", "text", "请把群名改为离线新名称", "模型编造的名称", nil, false},
		{"ordinary", "chatroom.SetName", "text", "你好", "离线新名称", nil, false},
		{"follow-up", "chatroom.SetName", "text", "就按刚刚那个来吧", "离线新名称", nil, false},
		{"negation", "chatroom.SetName", "text", "不要把群名改为离线新名称", "离线新名称", nil, false},
		{"reported-speech", "chatroom.SetName", "text", "他说请把群名改为离线新名称", "离线新名称", nil, false},
		{"quoted", "chatroom.SetName", "quote", "请把群名改为离线新名称", "离线新名称", nil, false},
		{"other-mention", "chatroom.SetName", "text", "@别人\u2005请把群名改为离线新名称", "离线新名称", nil, false},
		{"self-mention", "chatroom.SetName", "text", "@本人\u2005请把群名改为离线新名称", "离线新名称", []string{"fixture-self"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Message{ID: "fixture-current", Target: "fixture-target", Kind: tc.kind, Content: tc.text, Mentions: tc.mentions, Status: "sent"}
			if err := s.store.change(func(st *State) error {
				st.Account.ID = "fixture-self"
				st.Messages = []Message{{ID: "fixture-history", Target: m.Target, Kind: "text", Content: "请把群名改为离线新名称", Status: "sent"}, m}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			q := ai.Request{Target: m.Target, Conversation: &ai.Conversation{Current: contextcleaner.Message{ID: m.ID}}}
			if got := s.currentWriteIntent(q, tc.operation, tc.value); got != tc.allow {
				t.Errorf("授权判断错误：got %v want %v", got, tc.allow)
			}
		})
	}
}

func TestReplyOperationClaimsNeedEvidence(t *testing.T) {
	q := ai.Request{Prompt: "你再改一次", Context: []contextcleaner.Message{{Role: "user", Text: "把群名改成离线测试群"}}}
	for _, text := range []string{"改好了。", "改了，系统可能有点延迟。", "改真改了，就是缓存没刷过来。", "我这边显示的就是新群名，你自己刷新一下。", "刚点进去又提交了一遍，这次盯着改完的。"} {
		if replyOperationError(q, ai.Result{Text: text}) == nil {
			t.Errorf("未阻止无依据的操作声明: %s", text)
		}
	}
	for _, text := range []string{"我没有实际修改，请直接告诉我要改成的群名。", "当前群名需要向微信核对，我不能确认已经修改。", "大家聊得还挺热闹。"} {
		if err := replyOperationError(q, ai.Result{Text: text}); err != nil {
			t.Error(text, err)
		}
	}
	if err := replyOperationError(ai.Request{Prompt: "帮我润色这段文案"}, ai.Result{Text: "改好了，你看看。"}); err != nil {
		t.Fatal("误拦普通文本编辑", err)
	}
	q.Tools = []ai.Tool{{Name: "chatroom__SetName"}}
	if err := replyOperationError(q, ai.Result{Text: "好的"}); err != nil {
		t.Fatal("工具可用不代表模型必须调用", err)
	}
	q.Prompt = "请把这段文案再改一次"
	if err := replyOperationError(q, ai.Result{Text: "改好了，请看看这个版本。"}); err != nil {
		t.Fatal("历史群名话题误拦本轮文案编辑", err)
	}
	q.Prompt = "请改一下标题"
	if err := replyOperationError(q, ai.Result{Text: "改好了。"}); err != nil {
		t.Fatal("历史群名话题误拦本轮标题编辑", err)
	}
	q.Prompt = "群名改好了吗"
	if err := replyOperationError(q, ai.Result{Text: "改好了。"}); err == nil {
		t.Fatal("当前群名话题的虚构成功回执未被拦截")
	}
}

func TestCurrentMessageAuthorizesWriteTool(t *testing.T) {
	for _, tc := range []struct {
		name, input, kind, text string
		tool, execute           bool
	}{
		{"direct", "请把群名改为离线新名称", "text", "", true, true},
		{"follow-up", "就按刚刚那个来吧", "text", "", true, false},
		{"ordinary", "你好", "text", "", true, false},
		{"restart-chat", "重新聊吧，刚才没看见", "text", "", true, false},
		{"ordinary-reminder", "记得拉伸，别练完就躺着", "text", "", true, false},
		{"explicit-denial", "没有要改群名，继续聊刚才的话题", "text", "", true, false},
		{"quoted", "请把群名改为离线新名称", "quote", "", true, false},
		{"negation", "先别动名字了", "text", "", true, false},
		{"reported-speech", "刚才有人提议换个称呼，你怎么看", "text", "", true, false},
		{"discussion", "你觉得这个称呼合适吗", "text", "可以先讨论一下想表达的风格。", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var operations, sends atomic.Int32
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Tools    []struct{ Function struct{ Name string } }
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				wantTools := 2
				if len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Role == "tool" && !tc.execute {
					wantTools = 0
				}
				if len(body.Tools) != wantTools {
					t.Error("初始工具不应受句式影响，拒绝后纠正不再开放工具", len(body.Tools), wantTools)
				}
				contextText := ""
				for _, turn := range body.Messages {
					contextText += turn.Content
				}
				if len(body.Messages) < 3 || !strings.Contains(body.Messages[0].Content, "当前原始消息须明确说明操作和完整的新内容") || !strings.Contains(contextText, "离线新名称") {
					t.Error("模型缺少当前消息授权规则或会话上下文")
				}
				message := map[string]any{"content": tc.text}
				if tc.tool && body.Messages[len(body.Messages)-1].Role != "tool" {
					message = map[string]any{"tool_calls": []any{map[string]any{"function": map[string]string{"name": "chatroom__SetName", "arguments": `{"content":"离线新名称"}`}}}}
				} else if tc.tool && tc.execute {
					message = map[string]any{"content": "已更新群名。"}
				} else if tc.tool {
					if !strings.Contains(body.Messages[len(body.Messages)-1].Content, "current_write_intent_missing") {
						t.Error("未将拒绝执行的结果反馈给模型")
					}
					message = map[string]any{"content": "接着聊，你最近在听什么歌？"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
			})
			m.Target, m.Content, m.Kind = "fixture-semantic@chatroom", tc.input, tc.kind
			s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName", "SetAnnouncement"}}}
			replyRegressionCheck(t, s.initBehaviors())
			s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
				switch c.Operation {
				case "group-name":
					operations.Add(1)
					if c.Target != m.Target || c.Content != "离线新名称" {
						t.Error("工具目标或参数未绑定当前群")
					}
				case "send":
					sends.Add(1)
					if tc.tool {
						want := "接着聊，你最近在听什么歌？"
						if tc.execute {
							want = "已更新群名。"
						}
						if c.Content != want {
							t.Error("写操作未按当前消息授权", c.Content)
						}
					}
				default:
					t.Error(c.Operation)
				}
				return map[string]string{"id": "fixture-semantic-receipt"}, nil
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				history := Message{ID: "fixture-semantic-proposal", Target: m.Target, Kind: "text", Source: "ai", Self: true, Status: "sent", Content: "可以考虑离线新名称。", Time: time.Now().Add(-time.Minute).Format(time.RFC3339)}
				st.Messages = []Message{history, m}
				return nil
			}))
			task := retrySubmit(t, s, m)
			want := int32(0)
			if tc.execute {
				want = 1
			}
			if task.Status != "completed" || operations.Load() != want || sends.Load() != 1 {
				t.Fatal(task, operations.Load(), sends.Load())
			}
		})
	}
}

func TestMissingOperationCallRetriesBeforeSendingSuccess(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprint(succeeds), func(t *testing.T) {
			var calls, operations, sends atomic.Int32
			s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if succeeds && n == 3 {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"已将群名改为“离线新名称”。"}}]}`)
				} else if succeeds && n == 2 {
					fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"chatroom__SetName","arguments":"{\"content\":\"离线新名称\"}"}}]}}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"改好了。"}}]}`)
				}
			})
			m.Target, m.Content = "fixture-operation@chatroom", "@本人\u2005改群名为 离线新名称"
			s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
			replyRegressionCheck(t, s.initBehaviors())
			s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
				switch c.Operation {
				case "group-name":
					operations.Add(1)
				case "send":
					sends.Add(1)
					if c.Content != "已更新群名。" {
						t.Error("发送了模型编造的成功说明", c.Content)
					}
				default:
					t.Error(c.Operation)
				}
				return map[string]string{"id": "fixture-operation-receipt"}, nil
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				m.Mentions = []string{st.Account.ID}
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{m}
				return nil
			}))
			task := retrySubmit(t, s, m)
			if succeeds {
				if task.Status != "completed" || operations.Load() != 1 || sends.Load() != 1 || calls.Load() != 3 {
					t.Fatal(task, operations.Load(), sends.Load(), calls.Load())
				}
			} else if task.Status != "failed" || !task.ReplyRetryable || operations.Load() != 0 || sends.Load() != 0 || calls.Load() != 2 {
				t.Fatal(task, operations.Load(), sends.Load(), calls.Load())
			}
		})
	}
}

func TestDeniedWriteRecoveryFailuresNeverSendInternalText(t *testing.T) {
	for _, mode := range []string{"tool-again", "template", "record-format", "network", "safety"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				message := map[string]any{"tool_calls": []any{map[string]any{"function": map[string]string{"name": "chatroom__SetName", "arguments": `{"content":"离线名称"}`}}}}
				if n > 1 {
					switch mode {
					case "network":
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					case "template":
						message = map[string]any{"content": (writeIntentMissing{}).Error()}
					case "record-format":
						message = map[string]any{"content": "16:10:31 离线本人：继续聊。"}
					case "safety":
						message = map[string]any{"content": "fixture-denied-block"}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
			})
			m.Target, m.Content = "fixture-denied@chatroom", "接着刚才的话题聊吧"
			s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName"}}}
			replyRegressionCheck(t, s.initBehaviors())
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Account.Name = "离线本人"
				st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
				st.Messages = []Message{m}
				if mode == "safety" {
					st.Settings["prompt_guard.rules"] = `[{"id":"denied-output","name":"测试输出拦截","pattern":"fixture-denied-block","source":"output","action":"block","level":"high","enabled":true}]`
				}
				return nil
			}))
			task := retrySubmit(t, s, m)
			if task.Status != "failed" || task.Result != "" || calls.Load() != 2 || sends.Load() != 0 || task.ReplyRetryable != (mode != "safety") {
				t.Fatal("纠正失败被发送、成功标记或安全错误进入重试", task, calls.Load(), sends.Load())
			}
			var records int
			replyRegressionCheck(t, s.store.db.QueryRow("SELECT COUNT(*) FROM ai_action_records WHERE task=?", task.ID).Scan(&records))
			if records != 0 {
				t.Fatal("拒绝写入仍产生执行记录", records)
			}
			logged := false
			for _, log := range s.store.snapshot().Logs {
				logged = logged || log.Level == "WARN" && log.Target == m.Target && strings.Contains(log.Message, "chatroom__SetName") && strings.Contains(log.Message, "未执行")
			}
			if !logged {
				t.Fatal("缺少可诊断的拒绝操作日志")
			}
		})
	}
}

func TestDeniedWriteAfterExecutedOperationCannotReplay(t *testing.T) {
	var calls, writes, sends atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		name, content := "chatroom__SetName", "离线名称"
		switch calls.Add(1) {
		case 2:
			name, content = "chatroom__SetAnnouncement", "模型猜测的公告"
		case 3:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		args, _ := json.Marshal(map[string]string{"content": content})
		message := map[string]any{"tool_calls": []any{map[string]any{"function": map[string]string{"name": name, "arguments": string(args)}}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
	})
	m.Target, m.Content = "fixture-committed@chatroom", "请把群名改为离线名称"
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName", "SetAnnouncement"}}}
	replyRegressionCheck(t, s.initBehaviors())
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		switch c.Operation {
		case "group-name":
			writes.Add(1)
		case "send":
			sends.Add(1)
		default:
			t.Error("未授权操作被执行", c.Operation)
		}
		return map[string]string{"id": "fixture-committed-receipt"}, nil
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = []Message{m}
		return nil
	}))
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || task.ReplyRetryable || calls.Load() != 3 || writes.Load() != 1 || sends.Load() != 0 {
		t.Fatal("已执行操作进入可重放重试或拒绝文案被发送", task, calls.Load(), writes.Load(), sends.Load())
	}
}

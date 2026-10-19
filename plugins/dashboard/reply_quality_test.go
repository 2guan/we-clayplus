package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
)

func fragmentedReplyFixture() string {
	var text strings.Builder
	text.WriteString("这是一段无关的开头。\n")
	for i := 0; i < 268; i++ {
		fmt.Fprintf(&text, "0%c\n", rune('一'+i))
	}
	return text.String()
}

func TestReplyQuality(t *testing.T) {
	var list, long, code strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&list, "%d. 正常列表中的第%d个事项\n", i, i)
		fmt.Fprintf(&long, "第%d项处理结果：已检查配置，建议核对端口%d。\n", i, 8000+i)
		code.WriteString("{\n}\n")
	}
	for _, tc := range []struct {
		name, text string
		blocked    bool
	}{
		{"fragmented", fragmentedReplyFixture(), true},
		{"repeated-lines", strings.Repeat("一段完全相同的回答\n\n", 12), true},
		{"repeated-phrase", "正常开头。" + strings.Repeat("请稍等一下", 30), true},
		{"repeated-character", strings.Repeat("哈", 100), true},
		{"short-chat", "哈哈哈哈，行啊，等你消息。", false},
		{"short-lines", strings.Repeat("好\n", 8), false},
		{"list", list.String(), false},
		{"long-answer", long.String(), false},
		{"code", "示例：\n```json\n" + code.String() + "```\n照这个结构改。", false},
		{"tilde-code", "~~~\n" + code.String() + "~~~", false},
		{"separator", strings.Repeat("-", 100) + "\n分隔线", false},
		{"tool-only", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := replyQualityError(tc.text); (err != nil) != tc.blocked {
				t.Fatal("异常判断不符", err)
			}
		})
	}
}

func TestReplyQualityPipelineAndManualRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		successAt int
		proactive bool
		want      []string
	}{
		{"fallback", 2, false, []string{"a", "b"}},
		{"second-round", 3, false, []string{"a", "b", "a"}},
		{"manual-retry-after-exhaustion", 5, false, []string{"a", "b", "a", "b", "a"}},
		{"proactive", 2, true, []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Model string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				mu.Lock()
				calls = append(calls, body.Model)
				n := len(calls)
				mu.Unlock()
				answer := fragmentedReplyFixture()
				if n == tc.successAt {
					answer = "已经恢复正常了。"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": answer}}}})
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				a := st.Models[0]
				a.ID, a.Name, a.Model = "a", "a", "a"
				b := a
				b.ID, b.Name, b.Model = "b", "b", "b"
				st.Models, st.Pipeline = []Model{a, b}, []string{"a", "b"}
				st.Settings["routing.fallback"] = true
				if tc.proactive {
					st.Policies = append(st.Policies, defaultBehavior("message.Send"))
				}
				return nil
			}))
			q := replyRequest(s.store.snapshot(), m)
			if tc.proactive {
				var err error
				q, err = proactiveRequest(s.store.snapshot(), m.Target)
				replyRegressionCheck(t, err)
			}
			id, err := s.queue(q)
			replyRegressionCheck(t, err)
			task := waitModelTask(t, s, id)
			if tc.successAt == 5 {
				if task.Status != "failed" || !task.ReplyRetryable || task.AutoRetries != 1 || task.Result != "" || sends.Load() != 0 || !strings.Contains(task.Error, "碎片短行") {
					t.Fatal("异常文本被发送或未显示重试", task, sends.Load())
				}
				replyRegressionCheck(t, s.retryReply(task.ID, m.Target))
				st := s.store.snapshot()
				task = waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
			}
			mu.Lock()
			got := append([]string(nil), calls...)
			mu.Unlock()
			if !slices.Equal(got, tc.want) || task.Status != "completed" || task.Result != "已经恢复正常了。" || task.ReplyRetryable || sends.Load() != 1 {
				t.Fatal("流水线或发送结果不符", got, task, sends.Load())
			}
			found := false
			for _, l := range s.store.snapshot().Logs {
				if strings.Contains(l.Message, "模型回复异常，未发送") && l.Target == m.Target {
					found = true
				}
			}
			if !found {
				t.Fatal("没有异常诊断日志")
			}
		})
	}
}

func TestReplyQualityDoesNotRetrySafetyBlock(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "fixture-blocked\n" + fragmentedReplyFixture()}}}})
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		second := st.Models[0]
		second.ID, second.Name = "quality-second", "quality-second"
		st.Models = append(st.Models, second)
		st.Pipeline = append(st.Pipeline, second.ID)
		st.Settings["routing.fallback"] = true
		st.Settings["prompt_guard.rules"] = `[{"id":"quality-block","name":"测试输出拦截","pattern":"fixture-blocked","source":"output","action":"block","level":"high","enabled":true}]`
		return nil
	}))
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || task.ReplyRetryable || task.AutoRetries != 0 || calls.Load() != 1 || sends.Load() != 0 {
		t.Fatal("安全拦截进入了质量重试", task, sends.Load())
	}
	// 质量规则仅作用于微信回复；后台分析、编辑器不应用碎片短行限制。
	if _, _, err := filterSafetyOutput(s.store.snapshot(), ai.Request{Kind: "insight"}, ai.Result{Text: fragmentedReplyFixture()}, ""); err != nil {
		t.Fatal("误拦截分析资料", err)
	}
}

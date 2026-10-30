package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestReplyControlPartsCannotReachAnySend(t *testing.T) {
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	q, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["reply.max_parts"] = float64(5)
		return nil
	}))
	s.host = replyRegressionHost(func(context.Context, control.Command) (any, error) {
		t.Error("控制内容校验前就开始了外部操作")
		return nil, nil
	})
	for _, text := range []string{
		"前一句\n\n[NO_REPLY] 沉默原因：话题已结束",
		"前一句\n\n[assistant]后一句",
		"<voice>[NO_REPLY] 沉默原因：话题已结束</voice>",
	} {
		if err := replyPlanError(s.store.snapshot(), q, ai.Result{Text: text}); err == nil {
			t.Fatal("分段控制内容通过模型输出检查", text)
		} else if strings.Contains(text, "NO_REPLY") && !strings.Contains(err.Error(), "沉默标记") {
			t.Fatal("没有在媒体能力检查前识别控制标记", err)
		}
		if err := s.deliverReplyEvidence(context.Background(), m.Target, text, "text", nil, &q); err == nil {
			t.Fatal("分段控制内容通过最终发送检查", text)
		}
	}
}

func TestInternalImageRecordsCannotReachAnySend(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应请求模型") })
	q, err := s.refreshReplyContext(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["reply.max_parts"] = float64(5); return nil }))
	for _, text := range []string{
		"[已发送·AI生成图片（不代表真实拍摄或现实经历）；尚未完成识图，不能推断画面]\n生成意图（不代表实际画面）：一只猫坐在窗边",
		"正常开头\n\n[本人发送图片；正在识图，不能推断画面]",
		"[收到图片；模型识图描述，仅供参考] 一只猫",
		"[本人发送图片；识图失败，画面内容未知]",
		"<voice>生成意图（不代表实际画面）：一只猫</voice>",
		"<reply ref=\"m1234abcd\" mode=\"quote\">生成意图（不代表实际画面）：一只猫</reply>",
		"```text\n生成意图（不代表实际画面）：一只猫\n```",
	} {
		if err := replyPlanError(s.store.snapshot(), q, ai.Result{Text: text}); err == nil || !strings.Contains(err.Error(), "内部图片记录") {
			t.Fatalf("内部记录未被拦截：%v", err)
		}
		for _, evidence := range []*ai.Request{&q, nil} {
			if err := s.deliverReplyEvidence(context.Background(), m.Target, text, "text", nil, evidence); err == nil {
				t.Fatal("发送入口放行了内部记录")
			}
		}
	}
	if sends.Load() != 0 {
		t.Fatal("校验前发生了发送", sends.Load())
	}
}

func TestInternalImageRecordRetry(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				answer := "[已发送·AI生成图片（不代表真实拍摄或现实经历）；尚未完成识图，不能推断画面]\n生成意图（不代表实际画面）：一只猫坐在窗边"
				if calls.Add(1) > 1 && recover {
					answer = "换个角度看看。"
				}
				captionResult(w, answer)
			})
			task := retrySubmit(t, s, m)
			if calls.Load() != 2 || task.AutoRetries != 1 {
				t.Fatal("未重试一次", task, calls.Load())
			}
			if recover {
				if task.Status != "completed" || task.Result != "换个角度看看。" || sends.Load() != 1 {
					t.Fatal("没有只发送纠正后的回复", task, sends.Load())
				}
			} else if task.Status != "failed" || !task.ReplyRetryable || sends.Load() != 0 || !strings.Contains(task.Error, "内部图片记录") {
				t.Fatal("异常内容外发或未开放人工重试", task, sends.Load())
			}
			found := false
			for _, log := range s.store.snapshot().Logs {
				found = found || strings.Contains(log.Message, "内部图片记录") && strings.Contains(log.Message, "未发送")
			}
			if !found {
				t.Fatal("没有记录拦截原因")
			}
		})
	}
}

func fragmentedReplyFixture() string {
	var text strings.Builder
	text.WriteString("这是一段无关的开头。\n")
	for i := 0; i < 268; i++ {
		fmt.Fprintf(&text, "0%c\n", rune('一'+i))
	}
	return text.String()
}

func TestReplyRecordLabels(t *testing.T) {
	conversation := &ai.Conversation{SelfName: "离线本人", Names: map[string]string{"fixture-other": "离线成员"}}
	for _, tc := range []struct {
		text    string
		blocked bool
	}{
		{"16:10:31 离线本人：行，那我起头。", true},
		{"〔m1234abcd〕16:10:31 离线本人：行，那我起头。", true},
		{"2026-10-22 16:10 离线本人〔本人〕：接着聊。", true},
		{"正常开头。\n16:11:00 离线成员：模型冒充另一成员。", true},
		{"16:30 见，到时候给你消息。", false},
		{"日程安排：\n16:30 会议：需求评审。", false},
		{"> 16:10:31 离线本人：这是引用的原话。", false},
		{"```text\n16:10:31 离线本人：这是日志示例。\n```", false},
		{(writeIntentMissing{}).Error(), true},
	} {
		if err := replyQualityError(tc.text, conversation); (err != nil) != tc.blocked {
			t.Errorf("记录格式判断不符: %q: %v", tc.text, err)
		}
	}
}

func TestReplyRecordLabelsRetryWithoutSendingMalformedText(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			var calls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				text := "16:10:31 离线本人：这是错误的聊天记录格式。"
				if calls.Add(1) == 2 && recover {
					text = "我们接着聊。"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.Name = "离线本人"; return nil }))
			task := retrySubmit(t, s, m)
			if calls.Load() != 2 || task.AutoRetries != 1 {
				t.Fatal("未走完整重试流程", task, calls.Load())
			}
			if recover {
				if task.Status != "completed" || task.Result != "我们接着聊。" || sends.Load() != 1 {
					t.Fatal(task, sends.Load())
				}
			} else if task.Status != "failed" || !task.ReplyRetryable || sends.Load() != 0 || !strings.Contains(task.Error, "记录标签") {
				t.Fatal(task, sends.Load())
			}
		})
	}
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
		{"ordinary-image-text", "这张图里有一只猫，换个角度再试试。", false},
		{"image-protocol", `<image prompt="一只猫坐在窗边" ref="last"/>`, false},
		{"planning", "I notice the current message is for another participant, so I should stay quiet.\n\nI will not interrupt.", true},
		{"planning-zh", "当前消息是在问另一个人，我应该保持沉默。", true},
		{"planning-nonreply", "那俩表情是发给你看的，又没@我，我就不凑热闹了", true},
		{"planning-stay-quiet", "你们聊，我先不插话了。", true},
		{"ordinary-nonreply", "我不参与这个项目，交给另一组负责。", false},
		{"quoted-nonreply", "> 你们聊，我先不插话了。", false},
		{"explained-nonreply", "他说“你们聊，我不插话了”，是在表示礼貌。", false},
		{"role-continuation", "正常正文\n[assistant]继续错误示范", true},
		{"role-code", "```text\n[assistant]示例\n```", false},
		{"wechat-emotion", "[微笑] 好的", false},
		{"ordinary-english", "I noticed the upgrade is ready. You can try the new version.", false},
		{"quoted-planning", "> I notice the current message is for another participant, so I should stay quiet.\n\n这是引用示例。", false},
		{"code-planning", "```text\nI notice the current message is for another participant, so I should stay quiet.\n```", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := replyQualityError(tc.text, nil); (err != nil) != tc.blocked {
				t.Fatal("异常判断不符", err)
			}
		})
	}
}

func TestNonReplyPlanningRetriesToSilentWithoutSending(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		answer := "那俩表情是发给你看的，又没@我，我就不凑热闹了"
		if calls.Add(1) > 1 {
			answer = "[assistant]NO_REPLY: 非本人对话"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
	})
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || task.Result != "本次无需回复" || task.AutoRetries != 1 || calls.Load() != 2 || sends.Load() != 0 {
		t.Fatal("把不插话判断或角色标签沉默协议发到微信", task, calls.Load(), sends.Load())
	}
	found := false
	for _, log := range s.store.snapshot().Logs {
		found = found || strings.Contains(log.Message, task.ID) && strings.Contains(log.Message, "模型判断：非本人对话")
	}
	if !found {
		t.Fatal("没有记录不回复原因")
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

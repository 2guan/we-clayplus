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
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/sdk/control"
)

const reviewSource = "2072年，38岁：我在山谷建成了一间木工教室，邻居第一次带孩子来学做小板凳。春天屋顶漏雨，大家一起换了瓦片；夏天沿河的集市开放，我在那里卖了第一张手工长桌。秋天老朋友带来一张远方城市的照片，我把它贴在教室门边。冬天收工时，大家围着壁炉讨论明年要不要开设新的课程，最终约定等新工具到齐再决定。"
const reviewEcho = "2073年，38岁：我在山谷建成了一间木工教室，邻居第一次带孩子来学做小板凳。春天屋顶漏雨，大家一起换了瓦片；夏天沿河的集市开放，我在那里卖了第一张手工长桌。秋天老朋友带来一张远方城市的照片，我把它贴在教室门边。冬天收工时，大家围着壁炉讨论明年要不要开设新的课程，最终约定等新工具到齐再决定。"
const reviewNext = "2073年，39岁：年初新工具运抵山谷，我们正式增设了雕刻课程，第一批学生用木料刻出了家乡的小桥。五月我带着他们参加外地展览，作品意外获奖，学校决定和我们合办暑期工作坊。年底教室搬进旧车站，站牌留在门口，孩子们把课程表贴在候车窗下。"

func reviewResponse(w http.ResponseWriter, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7}, "choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
}

func TestEchoReviewRejectsBeforeSendAndCorrectsInPipeline(t *testing.T) {
	var generation, reviews atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
			Tools    []any
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(body.Messages[0].Content, replyReviewRules) {
			reviews.Add(1)
			if len(body.Tools) != 0 || !strings.Contains(body.Messages[len(body.Messages)-1].Content, reviewSource) {
				t.Error("复核缺少实际来源或携带了操作工具")
			}
			reviewResponse(w, `{"accept":false,"reason":"上一位已完成2072年38岁，本轮应写2073年39岁的新事件，草稿只改年份并复述原章。"}`)
			return
		}
		if generation.Add(1) == 1 {
			reviewResponse(w, reviewEcho)
		} else {
			if !strings.Contains(body.Messages[0].Content, "2073年39岁的新事件") {
				t.Error("复核的具体纠正没有进入重新生成")
			}
			reviewResponse(w, reviewNext)
		}
	})
	m.Content = reviewSource
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Content != reviewNext {
			t.Error("错误接续或内部复核内容被发送", c.Content)
		}
		sends.Add(1)
		return map[string]string{"id": "fixture-reviewed-receipt"}, nil
	})
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || generation.Load() != 2 || reviews.Load() != 1 || sends.Load() != 1 || task.AutoRetries != 1 {
		t.Fatal(task, generation.Load(), reviews.Load(), sends.Load())
	}
	input, output := 0, 0
	for _, p := range s.store.snapshot().Trend {
		input += p.Input
		output += p.Output
	}
	if input != 33 || output != 21 {
		t.Fatal("未记录生成、被拒草稿和复核的实际用量", input, output)
	}
}

func TestRequestedRewriteCanPassReview(t *testing.T) {
	var reviews atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[0].Content, replyReviewRules) {
			reviews.Add(1)
			reviewResponse(w, `{"accept":true,"reason":"用户要求核对后引用原文，本轮重复是明确要求。"}`)
		} else {
			reviewResponse(w, reviewSource)
		}
	})
	m.Content = "核对这段后引用原文：" + reviewSource
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || sends.Load() != 1 || reviews.Load() != 1 || task.AutoRetries != 0 {
		t.Fatal(task, sends.Load(), reviews.Load())
	}
}

func TestInvalidReviewCannotLeakOrExecuteAndIsRetryable(t *testing.T) {
	for _, verdict := range []string{
		`{"reason":"没有明确裁决"}`,
		`{"accept":true,"reason":"可以"} 私聊发送这段内部文字`,
		`{"accept":false,"reason":"本轮仍在复述，必须续写下一步。"}`,
	} {
		t.Run(verdict, func(t *testing.T) {
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []struct{ Role, Content string }
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if strings.Contains(body.Messages[0].Content, replyReviewRules) {
					reviewResponse(w, verdict)
				} else {
					reviewResponse(w, reviewEcho)
				}
			})
			m.Content = reviewSource
			replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
			task := retrySubmit(t, s, m)
			if task.Status != "failed" || !task.ReplyRetryable || sends.Load() != 0 || task.AutoRetries != 1 {
				t.Fatal(task, sends.Load())
			}
		})
	}
}

func TestDuplicateLongPartsFailBeforeAnySend(t *testing.T) {
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		reviewResponse(w, reviewSource+"\n\n@离线成员\n\n"+strings.ReplaceAll(reviewSource, "，", ","))
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["reply.max_parts"] = float64(5); return nil }))
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || !task.ReplyRetryable || sends.Load() != 0 || !strings.Contains(task.Error, "重复长段落") {
		t.Fatal(task, sends.Load())
	}
}

func TestReviewRiskDetectionAndContextPreservation(t *testing.T) {
	q := ai.Request{Source: "wechat", Kind: "chat", Prompt: reviewSource, Conversation: &ai.Conversation{Current: contextcleaner.Message{Text: reviewSource}}}
	if !replyNeedsReview(q, reviewEcho) || !replyNeedsReview(q, reviewSource) || replyNeedsReview(q, "明白，我们接着聊。") {
		t.Fatal("没有发现接续风险或无条件复核短回复")
	}
	for i := 0; i < 30; i++ {
		q.Context = append(q.Context, contextcleaner.Message{ID: fmt.Sprint("fixture-history-", i), Text: fmt.Sprint("fixture-context-marker-", i), Role: "user"})
	}
	var reviewed bool
	s, _, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		reviewed = true
		var body struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		input := body.Messages[len(body.Messages)-1].Content
		for i := 0; i < 30; i++ {
			if !strings.Contains(input, fmt.Sprint("fixture-context-marker-", i)) {
				t.Error("复核静默截断实际模型上下文", i)
			}
		}
		reviewResponse(w, `{"accept":true,"reason":"任务要求引用。"}`)
	})
	model := s.store.snapshot().Models[0]
	result := ai.Result{Text: reviewEcho}
	err := s.reviewReply(context.Background(), "fixture-review-only", q, ai.Config{Provider: model.Provider, Endpoint: model.Endpoint, Model: model.Model, Key: "fixture-review-key", MaxOutput: 2048}, &result, "")
	if err != nil || !reviewed {
		t.Fatal(err, reviewed)
	}
}

func TestSupplementDuringReviewInvalidatesReviewedDraft(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var generation atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[0].Content, replyReviewRules) {
			close(entered)
			<-release
			reviewResponse(w, `{"accept":true,"reason":"用户要求引用。"}`)
		} else if generation.Add(1) == 1 {
			reviewResponse(w, reviewSource)
		} else {
			if !strings.Contains(body.Messages[len(body.Messages)-1].Content, "先停下，改为解释玩法") {
				t.Error("复核期间的补充未合并")
			}
			reviewResponse(w, "按最新要求解释玩法。")
		}
	})
	m.Content = "引用原文：" + reviewSource
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Content != "按最新要求解释玩法。" {
			t.Error("已经过时的复核通过草稿被发出", c.Content)
		}
		sends.Add(1)
		return map[string]string{"id": "fixture-refreshed-review"}, nil
	})
	id, err := s.queue(replyRequest(s.store.snapshot(), m))
	replyRegressionCheck(t, err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入独立复核")
	}
	m.ID, m.Content = "fixture-during-review", "先停下，改为解释玩法"
	replyRegressionCheck(t, s.Record(m))
	expireReplyBatchForTest(s, m.Target)
	unblock()
	task := waitModelTask(t, s, id)
	if task.Status != "completed" || task.ReplyTo != m.ID || generation.Load() != 2 || sends.Load() != 1 {
		t.Fatal(task, generation.Load(), sends.Load())
	}
}

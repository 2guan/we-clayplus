package dashboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/sdk/control"
)

const protocolImage = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII="

func TestPseudoOrUnofferedImageToolRecoversToOneImage(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			var chat, imageCalls atomic.Int32
			s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/images/edits" {
					imageCalls.Add(1)
					fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, protocolImage)
					return
				}
				var body struct {
					Messages []struct{ Role, Content string }
					Tools    []any
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if !strings.Contains(body.Messages[0].Content, "本轮可用生图") {
					t.Error("承接请求没有获得图片能力")
				}
				n := chat.Add(1)
				if n == 1 {
					if len(body.Tools) == 0 {
						t.Error("未提供已授权行为工具，测试未覆盖两类协议并存")
					}
					if native {
						fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-image-call","function":{"name":"image_gen","arguments":"{\"prompt\":\"离线场景\",\"selfie\":true}"}}]}}]}`)
					} else {
						fmt.Fprint(w, `{"choices":[{"message":{"content":"行，发你<tool_call><function=image_gen><parameter=prompt>离线场景</parameter></function></tool_call>"}}]}`)
					}
					return
				}
				if len(body.Tools) != 0 {
					t.Error("协议纠正仍提供原生写操作工具")
				}
				fmt.Fprint(w, `{"choices":[{"message":{"content":"<image ref=\"selfie\" prompt=\"离线场景\"/>"}}]}`)
			})
			m.Content = "来一张"
			data, _ := base64.StdEncoding.DecodeString(protocolImage)
			reference, err := s.saveMedia(data, "image/png")
			replyRegressionCheck(t, err)
			s.cfg.Capabilities = map[string]control.Capability{"contact": {Enabled: []string{"SetRemark"}}}
			replyRegressionCheck(t, s.initBehaviors())
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Messages = []Message{m}
				st.Settings["reply.image_enabled"] = true
				st.Settings["image.reference"] = reference
				model := st.Models[0]
				model.ID, model.Name, model.Model = "fixture-image-model", "fixture-image-model", "fixture-image-model"
				model.Capabilities = []string{"image"}
				st.Models = append(st.Models, model)
				if st.Pipelines == nil {
					st.Pipelines = map[string][]string{}
				}
				st.Pipelines["image"] = []string{model.ID}
				return nil
			}))
			s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
				if imageCalls.Load() != 1 || strings.Contains(c.Content, "tool_call") || !strings.Contains(c.Content, "AI生成图片·自拍风格") {
					t.Error("假调用代码或未生成图片的应承被发送", c.Operation, c.Content)
				}
				sends.Add(1)
				return map[string]string{"id": "fixture-protocol-image-receipt"}, nil
			})
			task := retrySubmit(t, s, m)
			if task.Status != "completed" || task.AutoRetries != 1 || chat.Load() != 2 || imageCalls.Load() != 1 || sends.Load() != 1 {
				t.Fatal(task, chat.Load(), imageCalls.Load(), sends.Load())
			}
			raw, err := s.store.getSecret("reply-trace:" + s.store.snapshot().Account.ID + ":" + task.ID)
			replyRegressionCheck(t, err)
			var trace replyTrace
			replyRegressionCheck(t, json.Unmarshal([]byte(raw), &trace))
			if len(trace.ToolNames) != 0 || !slices.Contains(trace.OutputModes, "image") {
				t.Fatal("实际工具与媒体能力没有留痕", trace.ToolNames, trace.OutputModes)
			}
		})
	}
}

func TestUnofferedCallExhaustionLeavesRetryWithoutAnyWrite(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"fixture-forbidden","function":{"name":"imaginary_write","arguments":"{}"}}]}}]}`)
	})
	task := retrySubmit(t, s, m)
	if task.Status != "failed" || !task.ReplyRetryable || task.AutoRetries != 1 || calls.Load() != 2 || sends.Load() != 0 {
		t.Fatal(task, calls.Load(), sends.Load())
	}
}

func TestImageCapabilityUsesConfigurationInsteadOfCurrentKeywords(t *testing.T) {
	s, _, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("无需访问模型") })
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["reply.image_enabled"] = true
		st.Models[0].Capabilities = append(st.Models[0].Capabilities, "image")
		return nil
	}))
	st := s.store.snapshot()
	_, enabled := replyOutputCapabilities(st, "再来一张", "text")
	if !enabled {
		t.Fatal("上下文承接被关键词挡住")
	}
	st.Settings["reply.image_enabled"] = false
	_, enabled = replyOutputCapabilities(st, "给我自拍", "text")
	if enabled {
		t.Fatal("绕过生图开关")
	}
	st.Settings["reply.image_enabled"] = true
	st.Models[0].Enabled = false
	_, enabled = replyOutputCapabilities(st, "给我自拍", "text")
	if enabled {
		t.Fatal("使用停用的图片模型")
	}
}

func TestLeakedToolProtocolIsNotAnAssistantExample(t *testing.T) {
	old := contextcleaner.Message{Role: "assistant", Source: "ai", Text: "行，给你<tool_call><function=image_gen></function></tool_call>"}
	q := ai.Request{Kind: "chat", Source: "wechat", Prompt: "继续", Context: []contextcleaner.Message{old}, Conversation: &ai.Conversation{}}
	for _, m := range ai.ConversationMessages(q) {
		if strings.Contains(m.Text, "<tool_call>") {
			t.Fatal("旧伪工具协议仍作为示范", m)
		}
	}
	if !strings.Contains(q.Context[0].Text, "<tool_call>") {
		t.Fatal("修改了原始历史副本")
	}
}

func TestImagePromptIsNotReviewedAsChatProse(t *testing.T) {
	text := strings.Repeat("室内自然光，穿着深色衣服，神情自然放松，日常生活摄影风格。", 5)
	q := ai.Request{Source: "wechat", Kind: "chat", Conversation: &ai.Conversation{Current: contextcleaner.Message{Text: text}}}
	if replyNeedsReview(q, `<image ref="selfie" prompt="`+text+`"/>`) {
		t.Fatal("将生成参数误当聊天正文复核")
	}
	if !replyNeedsReview(q, text) {
		t.Fatal("跳过了真正的重复长正文")
	}
}

// 文件名：reply_test.go
// 主要功能：脱机验证混合回复解析、条数限制、媒体流水线、失败停止和人工接管。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestReplyProtocol(t *testing.T) {
	parts, err := parseReply("第一行\n仍在同条\r\n\r\n第二条<voice>[轻笑]你好 &amp; 再见</voice><image ref=\"selfie\" prompt=\"阳光 &amp; 花园\"/>", 5)
	if err != nil || len(parts) != 4 || parts[0].Text != "第一行\n仍在同条" || parts[2].Text != "[轻笑]你好 & 再见" || parts[3].Reference != "selfie" {
		t.Fatalf("混合计划错误 %+v %v", parts, err)
	}
	for _, text := range []string{"", "<voice>未闭合", "<voice design=\"x\">台词</voice>", "<voice><image prompt=\"x\"/></voice>", "<image prompt=\"x\" ref=\"remote\"/>", "<image prompt=\"x\"/><image prompt=\"y\"/>", "<voice>x</voice><voice>y</voice>", "```xml\n<voice>x</voice>\n```", "<image prompt=\"x\"><image prompt=\"y\"/></image>", "1\n\n2\n\n3\n\n4\n\n5\n\n6"} {
		if _, err := parseReply(text, 5); err == nil {
			t.Errorf("危险或超限输出被接受 %q", text)
		}
	}
}
func replyServer(t *testing.T) (*Server, string) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0].ID
	if err := s.store.change(func(st *State) error {
		st.Settings["reply.enabled"] = true
		st.Settings["reply.targets"] = target
		st.Settings["reply.interval_ms"] = float64(0)
		st.Settings["routing.concurrency"] = float64(1)
		st.Contacts[0].Mode = "auto"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, target
}
func TestMixedReplyAndSingleSlot(t *testing.T) {
	s, target := replyServer(t)
	before := len(s.store.snapshot().Messages)
	// 媒体子任务与父任务共享槽位，不借用新的并发名额，也不发起真实网络请求。
	s.taskCancel["parent"] = func() {}
	defer delete(s.taskCancel, "parent")
	err := s.deliverReply(context.Background(), target, "你好\n\n第二条<voice>一句语音</voice><image ref=\"selfie\" prompt=\"晴天花园\"/>")
	if err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	got := st.Messages[before:]
	if len(got) != 4 {
		t.Fatalf("消息数 %d", len(got))
	}
	for i, kind := range []string{"text", "text", "audio", "image"} {
		if got[i].Kind != kind || got[i].Status != "sent" || got[i].Target != target {
			t.Fatalf("顺序错误 %+v", got)
		}
	}
	if !strings.Contains(got[3].Content, "晴天花园") || got[3].Media == "" || got[2].Media == "" {
		t.Fatal("媒体或记忆描述丢失")
	}
	children := 0
	for _, task := range st.Tasks {
		if task.Name == "自动回复媒体生成" {
			children++
			if task.Delivery != "sent" {
				t.Fatal(task)
			}
		}
	}
	if children != 2 {
		t.Fatal(children)
	}
}
func TestReplyPreflightAndUnknownStops(t *testing.T) {
	t.Run("未配置图片模型不发送前置文本", func(t *testing.T) {
		s, target := replyServer(t)
		s.store.change(func(st *State) error { st.Pipelines = map[string][]string{"image": {}}; return nil })
		before := len(s.store.snapshot().Messages)
		if err := s.deliverReply(context.Background(), target, "给你看看<image prompt=\"猫\"/>"); err == nil || len(s.store.snapshot().Messages) != before {
			t.Fatal("缺少模型仍外发")
		}
	})
	t.Run("未知发送结果不继续", func(t *testing.T) {
		s, target := replyServer(t)
		s.store.change(func(st *State) error { st.Scenario = "error"; return nil })
		before := len(s.store.snapshot().Messages)
		if err := s.deliverReply(context.Background(), target, "第一条\n\n第二条"); err == nil {
			t.Fatal("失败未报告")
		}
		got := s.store.snapshot().Messages[before:]
		if len(got) != 1 || got[0].Status != "unknown" {
			t.Fatal(got)
		}
	})
	t.Run("媒体生成失败不外发", func(t *testing.T) {
		s, target := replyServer(t)
		s.store.change(func(st *State) error { st.Scenario = "error"; return nil })
		before := len(s.store.snapshot().Messages)
		if err := s.deliverReply(context.Background(), target, "给你听<voice>你好</voice>"); err == nil || len(s.store.snapshot().Messages) != before {
			t.Fatal("生成失败仍发送")
		}
	})
}
func TestReplyTakeoverAndCancellation(t *testing.T) {
	s, target := replyServer(t)
	s.store.change(func(st *State) error { st.Settings["reply.interval_ms"] = float64(250); return nil })
	before := len(s.store.snapshot().Messages)
	done := make(chan error, 1)
	go func() { done <- s.deliverReply(context.Background(), target, "第一条\n\n第二条") }()
	deadline := time.Now().Add(time.Second)
	for len(s.store.snapshot().Messages) == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s.store.change(func(st *State) error { st.Contacts[0].Mode = "manual"; return nil })
	if err := <-done; err == nil || len(s.store.snapshot().Messages) != before+1 {
		t.Fatal("人工接管未停止后续发送", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.deliverReply(ctx, target, "不能发送"); err == nil {
		t.Fatal("取消仍发送")
	}
}
func TestReplyMediaScopeAndQuota(t *testing.T) {
	s, target := replyServer(t)
	if _, _, err := s.replyMediaBytes("/media/../console.db"); err == nil {
		t.Fatal("路径穿越")
	}
	if _, _, err := s.loadMessageMedia(context.Background(), Message{ID: "missing", Target: target}); err == nil {
		t.Fatal("未归档媒体被读取")
	}
	s.store.change(func(st *State) error {
		st.Messages = append(st.Messages, Message{ID: "other-photo", Target: "other", Kind: "image", Media: "/sample-landscape.svg"})
		return nil
	})
	if _, _, err := s.loadMessageMedia(context.Background(), Message{ID: "other-photo", Target: target}); err == nil {
		t.Fatal("跨会话读取")
	}
	s.store.change(func(st *State) error {
		st.Settings["routing.daily_calls"] = float64(1)
		st.Tasks = []Task{{ID: "existing", Created: time.Now().Format(time.RFC3339)}}
		return nil
	})
	if _, err := s.replyMedia(context.Background(), ai.Request{Kind: "tts", Target: target, Prompt: "hello"}); err == nil {
		t.Fatal("媒体绕过每日限额")
	}
}
func TestReplyVoicePreferenceAndIncoming(t *testing.T) {
	s, target := replyServer(t)
	s.store.change(func(st *State) error { st.Settings["tts.reply"] = "跟随来信"; return nil })
	before := len(s.store.snapshot().Messages)
	if err := s.deliverReplyFor(context.Background(), target, "你好\n\n很高兴见到你", "audio"); err != nil {
		t.Fatal(err)
	}
	got := s.store.snapshot().Messages[before:]
	if len(got) != 1 || got[0].Kind != "audio" {
		t.Fatal(got)
	}
	s.store.change(func(st *State) error {
		st.Messages = append(st.Messages, Message{ID: "incoming-test", Target: target, Kind: "image", Media: "/sample-landscape.svg"})
		return nil
	})
	q, err := s.prepareIncoming(context.Background(), ai.Request{Kind: "chat", Source: "wechat", Target: target, InputKind: "image", InputID: "incoming-test"})
	if err != nil || !strings.Contains(q.Prompt, "模拟识图") {
		t.Fatal(q.Prompt, err)
	}
	// 最近图片的引用只限当前会话；相同调用仍经过图片模型和正常发送。
	if err := s.deliverReply(context.Background(), target, "<image ref=\"last\" prompt=\"改成暖色\"/>"); err != nil {
		t.Fatal(err)
	}
}

// 服务端白名单、模型 HTTP 响应、发送计划及持久化状态串联验证，Host 为仅本机的记录器。
type replyCaptureHost struct {
	commands []control.Command
	failAt   int
}

func (h *replyCaptureHost) Execute(_ context.Context, c control.Command) (any, error) {
	h.commands = append(h.commands, c)
	if len(h.commands) == h.failAt {
		return nil, fmt.Errorf("synthetic unknown receipt")
	}
	return map[string]string{"id": fmt.Sprintf("receipt-%d", len(h.commands))}, nil
}
func TestAutoReplyEndToEndFailureAndMemory(t *testing.T) {
	s := newServer(t, false)
	host := &replyCaptureHost{failAt: 2}
	s.host = host
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Messages[0].Content, "微信输出协议") || !strings.Contains(body.Messages[0].Content, "专属人设") {
			t.Error("专属人设或服务端协议未注入")
		}
		if strings.Contains(body.Messages[1].Content, "失败的旧自拍") {
			t.Error("发送失败的记录进入上下文")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"第一条\n\n第二条\n\n第三条"}}],"usage":{"prompt_tokens":10,"completion_tokens":8}}`)
	}))
	defer remote.Close()
	s.store.putSecret("fixture-vendor", "fixture-key")
	s.store.change(func(st *State) error {
		st.Account = Account{ID: "synthetic-self", Status: "online"}
		st.Contacts = []Contact{{ID: "synthetic-peer", Kind: "friend", Mode: "auto"}}
		st.Settings["reply.enabled"] = true
		st.Settings["reply.targets"] = "synthetic-peer"
		st.Settings["reply.interval_ms"] = float64(0)
		st.Models = []Model{{ID: "fixture-model", Name: "fixture", VendorID: "fixture-vendor", Model: "fixture", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-model"}
		st.Overrides["synthetic-peer"] = map[string]any{"prompt": "专属人设"}
		st.Messages = []Message{{ID: "unknown-photo", Target: "synthetic-peer", Kind: "image", Self: true, Source: "ai", Status: "unknown", Content: "失败的旧自拍", Time: time.Now().Format(time.RFC3339)}}
		return nil
	})
	if err := s.Record(Message{ID: "new", Target: "synthetic-peer", SenderID: "synthetic-peer", Kind: "text", Content: "你好", Time: time.Now().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	if len(st.Tasks) == 0 {
		t.Fatal("未创建自动回复任务")
	}
	task := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if task.Status != "failed" || !strings.Contains(task.Error, "第 2 条") || len(host.commands) != 2 {
		t.Fatalf("未停止或未记录失败 %+v %d", task, len(host.commands))
	}
	st = s.store.snapshot()
	if len(st.Messages) != 4 || st.Messages[2].Status != "sent" || st.Messages[3].Status != "unknown" {
		t.Fatal("回执状态错误")
	}
	// 任务终态先落盘，用量汇总随后更新；等待该异步观测而非依赖调度顺序。
	deadline := time.Now().Add(time.Second)
	for (len(st.Trend) == 0 || st.Trend[len(st.Trend)-1].Input != 10) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		st = s.store.snapshot()
	}
	if len(st.Trend) == 0 || st.Trend[len(st.Trend)-1].Input != 10 {
		t.Fatal("发送失败丢失真实模型用量")
	}
}

func TestAutoReplyRecordEnvelopeBoundary(t *testing.T) {
	s := newServer(t, false)
	host := &replyCaptureHost{}
	s.host = host
	wrapped := `{"status":"sent","sender_id":"fixture-self","actor_type":"assistant_ai","id":"fixture-model-invented-id","session":"fixture-envelope-peer","role":"assistant","source":"ai","kind":"text","text":"正常回复正文"}`
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture", 400)
			return
		}
		if len(body.Messages) != 3 || !strings.Contains(body.Messages[1].Content, "〔本人〕：正常回复正文") || strings.Contains(body.Messages[1].Content, "fixture-model-invented-id") {
			t.Error("历史未合并为纯文本，或包装元数据仍在模型请求中")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": wrapped}}}, "usage": map[string]int{"prompt_tokens": 8, "completion_tokens": 6}})
	}))
	defer remote.Close()
	if err := s.store.putSecret("fixture-envelope-vendor", "fixture-only-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(st *State) error {
		st.Account = Account{ID: "fixture-self", Status: "online"}
		st.Settings["reply.enabled"] = true
		st.Settings["reply.targets"] = "fixture-envelope-peer"
		st.Settings["reply.interval_ms"] = float64(0)
		st.Contacts = []Contact{{ID: "fixture-envelope-peer", Kind: "friend", Mode: "auto"}}
		st.Models = []Model{{ID: "fixture-envelope-model", Name: "fixture-model", Model: "fixture-model", VendorID: "fixture-envelope-vendor", Provider: "mimo", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-envelope-model"}
		st.Messages = []Message{{ID: "fixture-old", Target: "fixture-envelope-peer", Self: true, Source: "ai", Status: "sent", Kind: "text", Content: wrapped, Time: time.Now().Format(time.RFC3339)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Message{ID: "fixture-envelope-current", Target: "fixture-envelope-peer", SenderID: "fixture-envelope-peer", Source: "wechat", Status: "sent", Kind: "text", Content: "请正常回复", Time: time.Now().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	task := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if task.Status != "completed" || task.Result != "正常回复正文" || len(host.commands) != 1 || host.commands[0].Content != "正常回复正文" {
		t.Fatalf("包装元数据进入实际发送或任务正文：%+v %+v", task, host.commands)
	}
	st = s.store.snapshot()
	if st.Messages[0].Content != wrapped || st.Messages[len(st.Messages)-1].ID == "fixture-model-invented-id" || st.Messages[len(st.Messages)-1].Content != "正常回复正文" {
		t.Fatal("篡改旧归档或采用了模型编造的回执")
	}
	_, _, err := filterSafetyOutput(st, ai.Request{Source: "wechat", Kind: "chat"}, ai.Result{Text: strings.Replace(wrapped, "正常回复正文", `fixture-credential-should-not-leak`, 1)}, "fixture-credential-should-not-leak")
	if err == nil {
		t.Fatal("拆包装绕过凭据保护")
	}
}

func TestAutoReplyRejectsUnprovidedTool(t *testing.T) {
	s := newServer(t, false)
	host := &replyCaptureHost{}
	s.host = host
	s.cfg.Capabilities = map[string]control.Capability{"contact": {Enabled: []string{"SetRemark"}}}
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tools"]; ok {
			t.Error("普通闲聊仍有工具定义")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"function":{"name":"contact__SetRemark","arguments":"{\"content\":\"synthetic\"}"}}]}}]}`)
	}))
	defer remote.Close()
	if err := s.store.putSecret("fixture-intent-vendor", "fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(st *State) error {
		st.Account = Account{ID: "fixture-intent-self", Status: "online"}
		st.Contacts = []Contact{{ID: "fixture-intent-peer", Kind: "friend", Mode: "auto"}}
		st.Settings["reply.enabled"] = true
		st.Settings["reply.targets"] = "fixture-intent-peer"
		st.Models = []Model{{ID: "fixture-intent-model", VendorID: "fixture-intent-vendor", Name: "fixture-intent", Model: "fixture-intent", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-intent-model"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Message{ID: "fixture-intent-current", Target: "fixture-intent-peer", SenderID: "fixture-intent-peer", Kind: "text", Content: "你好", Time: time.Now().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	task := waitModelTask(t, s, st.Tasks[len(st.Tasks)-1].ID)
	if task.Status != "failed" || !strings.Contains(task.Error, "本轮未提供") || len(host.commands) != 0 {
		t.Fatalf("未提供工具被执行：%+v %+v", task, host.commands)
	}
}

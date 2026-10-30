package dashboard

import (
	"context"
	"encoding/base64"
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

func imageAnalysisFixture(t *testing.T, handler http.HandlerFunc) (*Server, Message, *atomic.Int32) {
	t.Helper()
	s, m, sends := retryFixture(t, handler)
	m.ID, m.Kind, m.Content, m.Target = "fixture-description-photo", "image", "[图片]", "fixture-description@chatroom"
	data, _ := base64.StdEncoding.DecodeString(protocolImage)
	path, err := s.saveMedia(data, "image/png")
	replyRegressionCheck(t, err)
	m.Media = path
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Messages = nil
		st.Contacts[0].ID, st.Contacts[0].Kind = m.Target, "group"
		st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
		st.Settings["vision.auto"], st.Settings["vision.cache"] = true, true
		st.Settings["routing.concurrency"] = float64(1)
		st.Models[0].Capabilities = append(st.Models[0].Capabilities, "vision")
		if st.Pipelines == nil {
			st.Pipelines = map[string][]string{}
		}
		st.Pipelines["vision"] = []string{st.Models[0].ID}
		return nil
	}))
	return s, m, sends
}

func waitImageDescription(t *testing.T, s *Server, m Message, status string) Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if saved, ok := imageMessage(s.store.snapshot(), m.Target, m.ID); ok && saved.ImageAnalysis != nil && saved.ImageAnalysis.Status == status {
			return saved
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("图片描述未进入%s，快照：%+v", status, s.store.snapshot().Messages)
	return Message{}
}

func captionResult(w http.ResponseWriter, text string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}}}, "usage": map[string]int{"prompt_tokens": 9, "completion_tokens": 7}})
}

func TestImageDescriptionRunsWithoutReplyAndCachesOnlySameConversation(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		captionResult(w, "一只橙色的猫坐在蓝色椅子上，画面右下角有数字42。")
	})
	replyRegressionCheck(t, s.Record(m))
	first := waitImageDescription(t, s, m, "completed")
	if sends.Load() != 0 || calls.Load() != 1 || first.Content != "[图片]" || first.ImageAnalysis.Model == "" || first.ImageAnalysis.TaskID == "" {
		t.Fatal(first, sends.Load(), calls.Load())
	}
	st := s.store.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.awaitImageAnalysis(ctx, st.Account.ID, st.AccountEpoch, m.Target, m.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	copy := m
	copy.ID, copy.SenderID, copy.Sender = "fixture-second-photo", "fixture-second-person", "另一成员"
	replyRegressionCheck(t, s.Record(copy))
	second := waitImageDescription(t, s, copy, "completed")
	if !second.ImageAnalysis.CacheHit || calls.Load() != 1 || second.SenderID != copy.SenderID {
		t.Fatal(second, calls.Load())
	}
	other := m
	other.ID, other.Target = "fixture-other-photo", "fixture-other@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = append(st.Contacts, Contact{ID: other.Target, Kind: "group", Mode: "auto"})
		st.Overrides[other.Target] = map[string]any{"probability": float64(0)}
		return nil
	}))
	replyRegressionCheck(t, s.Record(other))
	third := waitImageDescription(t, s, other, "completed")
	if third.ImageAnalysis.CacheHit || calls.Load() != 2 {
		t.Fatal("跨会话复用了描述", third, calls.Load())
	}
	current := Message{ID: "fixture-followup", Target: m.Target, Content: "刚才那张是什么", Kind: "text", Time: time.Now().Format(time.RFC3339)}
	conv, history := buildReplyContext(s.store.snapshot(), current, time.Now())
	clean, err := contextcleaner.Prepare(contextcleaner.Config{Enabled: true, Media: true, Budget: 100000, MaxMessages: 300}, history, m.Target)
	replyRegressionCheck(t, err)
	q := ai.Request{Kind: "chat", Source: "wechat", Prompt: current.Content, Context: clean.Messages, Conversation: &conv}
	text := ""
	for _, turn := range ai.ConversationMessages(q) {
		text += turn.Text + "\n"
	}
	if strings.Count(text, "一只橙色的猫") != 2 || strings.Contains(text, "image媒体") || strings.Contains(text, other.ID) {
		t.Fatal("历史描述被清理或跨会话混用", text)
	}
}

func TestIncomingImageSharesBackgroundResultWithSingleChatSlot(t *testing.T) {
	var vision, chat atomic.Int32
	s, m, sends := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Content json.RawMessage }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := string(body.Messages[len(body.Messages)-1].Content)
		if strings.HasPrefix(content, "[") {
			vision.Add(1)
			captionResult(w, "桌上有一杯红茶。")
		} else {
			chat.Add(1)
			if strings.Count(content, "桌上有一杯红茶") != 1 {
				t.Error("本轮识图结果丢失或重复", content)
			}
			captionResult(w, "这杯红茶看起来不错。")
		}
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	task := retrySubmit(t, s, m)
	if task.Status != "completed" || vision.Load() != 1 || chat.Load() != 1 || sends.Load() != 1 {
		t.Fatal(task, vision.Load(), chat.Load(), sends.Load())
	}
	waitImageDescription(t, s, m, "completed")
}

func TestImageDescriptionFailureCanRetryWithoutSendingReply(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	s, m, sends := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		captionResult(w, "一把黄色雨伞。")
	})
	replyRegressionCheck(t, s.Record(m))
	failed := waitImageDescription(t, s, m, "failed")
	if failed.ImageAnalysis.Description != "" || failed.ImageAnalysis.Error == "" || calls.Load() != 2 {
		t.Fatal(failed, calls.Load())
	}
	fail.Store(false)
	replyRegressionCheck(t, s.retryImageAnalysis(m.Target, m.ID))
	done := waitImageDescription(t, s, m, "completed")
	if done.ImageAnalysis.Description != "一把黄色雨伞。" || calls.Load() != 3 || sends.Load() != 0 {
		t.Fatal(done, calls.Load(), sends.Load())
	}
}

func TestImageDescriptionInheritedReplyMode(t *testing.T) {
	for _, kind := range []string{"friend", "group"} {
		for _, source := range []string{"ai", "manual", "wechat"} {
			t.Run(kind+"/"+source, func(t *testing.T) {
				var calls atomic.Int32
				s, m, sends := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					captionResult(w, "桌上有一杯红茶。")
				})
				if kind == "friend" {
					m.Target = "fixture-description-friend"
				}
				m.Self, m.Source, m.ImageGenerated = source != "wechat", source, source == "ai"
				replyRegressionCheck(t, s.store.change(func(st *State) error {
					st.Contacts[0].ID, st.Contacts[0].Kind, st.Contacts[0].Mode = m.Target, kind, "paused"
					st.Settings["reply.default_"+kind] = "自动"
					st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
					st.Messages = []Message{m}
					return nil
				}))
				st := s.store.snapshot()
				if !st.Contacts[0].ModeInherited || st.Contacts[0].Mode != "auto" {
					t.Fatal("测试会话没有继承自动模式")
				}
				if source == "ai" {
					s.scheduleImageAnalysis(m)
				} else {
					replyRegressionCheck(t, s.retryImageAnalysis(m.Target, m.ID))
				}
				done := waitImageDescription(t, s, m, "completed")
				if done.ImageAnalysis.Description != "桌上有一杯红茶。" || calls.Load() != 1 || sends.Load() != 0 || !strings.Contains(contextMessage(s.store.snapshot(), done).Text, "桌上有一杯红茶") {
					t.Fatal("没有保存并注入描述，或额外发送了回复", done, calls.Load(), sends.Load())
				}
				// 校验不能将有效模式写回数据库，默认切成人工后须立即生效。
				replyRegressionCheck(t, s.store.change(func(st *State) error {
					if st.Contacts[0].Mode != "paused" {
						t.Error("识图污染了继承设置", st.Contacts[0].Mode)
					}
					st.Settings["reply.default_"+kind] = "人工"
					return nil
				}))
				if err := s.retryImageAnalysis(m.Target, m.ID); err == nil {
					t.Fatal("默认切成人工后仍允许识图")
				}
				// 默认恢复自动，显式暂停仍应优先。
				replyRegressionCheck(t, s.store.change(func(st *State) error {
					st.Settings["reply.default_"+kind] = "自动"
					st.Overrides[m.Target]["mode"] = "paused"
					return nil
				}))
				if err := s.retryImageAnalysis(m.Target, m.ID); err == nil {
					t.Fatal("显式暂停的会话仍允许识图")
				}
			})
		}
	}
}

func TestImageDescriptionScopesAndRevokedEvidence(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		captionResult(w, "不应存储的旧描述")
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "manual"; return nil }))
	replyRegressionCheck(t, s.Record(m))
	time.Sleep(30 * time.Millisecond)
	saved, _ := imageMessage(s.store.snapshot(), m.Target, m.ID)
	if saved.ImageAnalysis != nil || calls.Load() != 0 {
		t.Fatal("人工模式仍自动识图")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Contacts[0].Mode = "auto"; return nil }))
	replyRegressionCheck(t, s.retryImageAnalysis(m.Target, m.ID))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未开始识图")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages[0].Status = "revoked_by_other"; return nil }))
	unblock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.store.snapshot().Tasks) > 0 {
			task := s.store.snapshot().Tasks[0]
			if task.Status == "completed" {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, item := range s.store.snapshot().Messages {
		if item.ID == m.ID && item.ImageAnalysis != nil && item.ImageAnalysis.Description != "" {
			t.Fatal("撤回后仍写入描述", item)
		}
	}
	if err := s.retryImageAnalysis(m.Target, m.ID); err == nil {
		t.Fatal("允许重新识别已撤回图片")
	}
}

func TestGeneratedImageAnalyzedOnlyAfterConfirmedSend(t *testing.T) {
	var calls atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		captionResult(w, "白色背景上有一个绿色圆形。")
	})
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if reject.Load() {
			return nil, fmt.Errorf("fixture unknown delivery")
		}
		return map[string]string{"id": "fixture-sent-generated"}, nil
	})
	q := actionRequest{Target: m.Target, Group: "image", Media: protocolImage, MIME: "image/png", Content: "原始生成意图", generatedImage: true, generationPrompt: "蓝色方形"}
	if err := s.send(context.Background(), q, "ai"); err == nil {
		t.Fatal("发送失败被当作成功")
	}
	time.Sleep(25 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("未知发送被当成已发送图片分析")
	}
	reject.Store(false)
	replyRegressionCheck(t, s.send(context.Background(), q, "ai"))
	m.ID = "fixture-sent-generated"
	done := waitImageDescription(t, s, m, "completed")
	ctx := contextMessage(s.store.snapshot(), done)
	if calls.Load() != 1 || !done.ImageGenerated || !strings.Contains(ctx.Text, "绿色圆形") || !strings.Contains(ctx.Text, "生成意图（不代表实际画面）：蓝色方形") || !strings.Contains(ctx.Text, "AI生成") || !ctx.MediaText {
		t.Fatal(done, ctx)
	}
}

func TestMediaDownloadDoesNotWriteAcrossAccountSwitch(t *testing.T) {
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("不应调用模型") })
	m.Media = ""
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = []Message{m}; return nil }))
	st := s.store.snapshot()
	replyRegressionCheck(t, s.store.putSecret(accountMessageMediaKey(st.Account.ID, m), `{}`))
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-other-account"; return nil }))
		return base64.StdEncoding.DecodeString(protocolImage)
	})
	if _, _, err := s.loadMessageMediaFor(context.Background(), m, st.Account.ID, st.AccountEpoch); err == nil {
		t.Fatal("跨账号下载结果被采用")
	}
	if s.store.snapshot().Messages[0].Media != "" {
		t.Fatal("旧媒体写到了新账号")
	}
}

func TestImageDescriptionSurvivesStoreReopen(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		captionResult(w, "红色屋顶旁有一棵树。")
	})
	replyRegressionCheck(t, s.Record(m))
	done := waitImageDescription(t, s, m, "completed")
	s.Close()
	restored, err := openStore(s.cfg.DataDir, false)
	replyRegressionCheck(t, err)
	defer restored.db.Close()
	photo, ok := imageMessage(restored.snapshot(), m.Target, m.ID)
	if !ok || photo.ImageAnalysis == nil || photo.ImageAnalysis.Status != "completed" || photo.ImageAnalysis.Description != done.ImageAnalysis.Description || photo.ImageAnalysis.SHA256 == "" || calls.Load() != 1 {
		t.Fatal(photo, calls.Load())
	}
}

func TestImageDescriptionDoesNotCrossAccountRoundTrip(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
			return
		}
		captionResult(w, "新账号自己的图片描述")
	})
	original := s.store.snapshot()
	replyRegressionCheck(t, s.Record(m))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未开始旧账号识图")
	}
	replyRegressionCheck(t, s.SetAccount(Account{ID: "fixture-description-other-account", Status: "online"}))
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = []Contact{{ID: m.Target, Kind: "group", Mode: "auto"}}
		st.Messages = nil
		st.Models = original.Models
		st.Pipelines = original.Pipelines
		st.Settings["reply.enabled"] = true
		st.Settings["vision.auto"] = true
		st.Settings["vision.cache"] = true
		st.Overrides[m.Target] = map[string]any{"probability": float64(0)}
		return nil
	}))
	// 故意使用相同会话与消息ID，验证账号代次而非仅按消息ID绑定。
	replyRegressionCheck(t, s.Record(m))
	next := waitImageDescription(t, s, m, "completed")
	if next.ImageAnalysis.Description != "新账号自己的图片描述" || next.ImageAnalysis.CacheHit || calls.Load() != 2 {
		t.Fatal(next, calls.Load())
	}
}

func TestGeneratedImageEchoPreservesExistingDescription(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := imageAnalysisFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		captionResult(w, "白底绿色圆形")
	})
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		echo := m
		echo.ID, echo.Self, echo.Source = "fixture-early-image-echo", true, "wechat"
		replyRegressionCheck(t, s.Record(echo))
		waitImageDescription(t, s, echo, "completed")
		return map[string]string{"id": echo.ID}, nil
	})
	replyRegressionCheck(t, s.send(context.Background(), actionRequest{Target: m.Target, Group: "image", Media: protocolImage, MIME: "image/png", generatedImage: true, generationPrompt: "绿色圆形"}, "ai"))
	m.ID = "fixture-early-image-echo"
	done := waitImageDescription(t, s, m, "completed")
	if !done.ImageGenerated || done.ImageAnalysis.Description != "白底绿色圆形" || calls.Load() != 1 {
		t.Fatal("发送回执合并丢失描述或重复识图", done, calls.Load())
	}
	count := 0
	for _, saved := range s.store.snapshot().Messages {
		if saved.Target == m.Target {
			count++
		}
	}
	if count != 1 {
		t.Fatal("回显未合并", count)
	}
}

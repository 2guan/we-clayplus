// 文件名：voice_test.go
// 主要功能：验证合成到发送的会话绑定、并发去重、失败状态与脱机媒体隔离。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

func completedVoice(t *testing.T, s *Server, target string) string {
	t.Helper()
	w := call(t, s, "/api/tasks", ai.Request{Kind: "tts", Prompt: "仅用于脱机验证的文字", Target: target}, nil, "")
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	id := result.ID
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, _ := s.Task(id)
		if task.Status == "completed" {
			return id
		}
		if task.Status == "failed" {
			t.Fatal(task.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("语音任务没有完成")
	return ""
}

func TestVoiceTaskDelivery(t *testing.T) {
	s := newServer(t, true)
	state := s.store.snapshot()
	target := state.Contacts[0].ID
	id := completedVoice(t, s, target)
	if err := s.sendTaskVoice(context.Background(), actionRequest{ID: id, Target: state.Contacts[1].ID}); err == nil {
		t.Fatal("语音被发往其他会话")
	}
	before := len(s.store.snapshot().Messages)
	var successes atomic.Int32
	var wg sync.WaitGroup
	// 多次同时提交同一个任务，只有一次可进入发送；不依赖前端按钮禁用。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.sendTaskVoice(context.Background(), actionRequest{ID: id, Target: target}); err == nil {
				successes.Add(1)
			} else {
				t.Log(err)
			}
		}()
	}
	wg.Wait()
	state = s.store.snapshot()
	if successes.Load() != 1 || len(state.Messages) != before+1 {
		t.Fatalf("发送成功次数 %d，消息数 %d -> %d", successes.Load(), before, len(state.Messages))
	}
	var found bool
	for _, m := range state.Messages {
		if m.Content == "仅用于脱机验证的文字" && m.Target == target && m.Self {
			found = true
			if m.Kind != "audio" || m.Status != "sent" || m.Media == "" {
				t.Fatal("发送记录不是有效语音")
			}
			w := call(t, s, m.Media, nil, nil, "")
			if w.Code != 200 || w.Body.Len() < 44 || w.Body.String()[:4] != "RIFF" {
				t.Fatal("发送后音频不可播放")
			}
		}
	}
	if !found {
		t.Fatal("缺少语音记录")
	}
	task, _ := s.Task(id)
	if task.Delivery != "sent" {
		t.Fatal("未保存发送状态")
	}
}

func TestVoiceFailureAndInvalidMedia(t *testing.T) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0].ID
	id := completedVoice(t, s, target)
	if err := s.store.change(func(v *State) error { v.Scenario = "error"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.sendTaskVoice(context.Background(), actionRequest{ID: id, Target: target}); err == nil {
		t.Fatal("模拟失败返回成功")
	}
	task, _ := s.Task(id)
	if task.Delivery != "unknown" || task.Error == "" {
		t.Fatalf("失败没有保留待核对状态: %+v", task)
	}
	before := len(s.store.snapshot().Messages)
	if err := s.sendTaskVoice(context.Background(), actionRequest{ID: id, Target: target}); err == nil || len(s.store.snapshot().Messages) != before {
		t.Fatal("失败后重复发起了发送")
	}
	for _, item := range []Task{
		{ID: "invalid-kind", Kind: "chat", Status: "completed", Target: target, Result: "/sample-audio.wav"},
		{ID: "invalid-status", Kind: "tts", Status: "cancelled", Target: target, Result: "/sample-audio.wav"},
		{ID: "invalid-path", Kind: "tts", Status: "completed", Target: target, Result: "/media/../console.db"},
		{ID: "invalid-remote", Kind: "tts", Status: "completed", Target: target, Result: "https://invalid.example/audio.wav"},
	} {
		if err := s.store.change(func(v *State) error { v.Tasks = append(v.Tasks, item); return nil }); err != nil {
			t.Fatal(err)
		}
		if err := s.sendTaskVoice(context.Background(), actionRequest{ID: item.ID, Target: target}); err == nil {
			t.Fatalf("无效任务被发送: %s", item.ID)
		}
	}
}

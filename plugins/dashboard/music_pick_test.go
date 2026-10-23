package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMusicPickIntentAndValidation(t *testing.T) {
	for _, query := range []string{"随便点一首舒缓的", "推荐一首轻快的英文歌", "舒缓的", "来一首治愈的歌", "孙燕姿 随机一首"} {
		if !musicNeedsPick(query) {
			t.Error("未识别选曲要求", query)
		}
	}
	for _, query := range []string{"完美的一天", "孙燕姿 完美的第一天", "安静", "《随便》", "故事"} {
		if musicNeedsPick(query) {
			t.Error("明确歌名误当随机要求", query)
		}
	}
	for _, raw := range []string{`not json`, `{"songs":[]}`, `{"songs":[{"title":"舒缓"}]}`, `{"songs":[{"title":"歌名","artist":"https://example.com/audio"}]}`} {
		if _, err := parseMusicPicks(raw); err == nil {
			t.Fatal("接受了无效的选曲结果", raw)
		}
	}
	picks, err := parseMusicPicks("```json\n{\"songs\":[{\"title\":\"示例曲\",\"artist\":\"示例歌手\"}]}\n```")
	if err != nil || len(picks) != 1 || picks[0] != "示例歌手 示例曲" {
		t.Fatal(picks, err)
	}
}

func TestMusicPickUsesConfiguredPipelineWithoutSending(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Model    string
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, turn := range body.Messages {
			if strings.Contains(turn.Content, "fixture-retry-input") {
				t.Error("选曲不应附带聊天归档")
			}
		}
		if body.Model == "fixture" {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"songs":[{"title":"示例舒缓曲","artist":"示例演唱者"}]}`}}}})
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		second := st.Models[0]
		second.ID, second.Model = "fixture-pick-second", "fixture-pick-two"
		st.Models = append(st.Models, second)
		st.Pipeline = append(st.Pipeline, second.ID)
		st.Settings["routing.fallback"] = true
		return nil
	}))
	queries, id, err := s.pickMusic(context.Background(), businessRequest{Plugin: "music", Target: m.Target, Query: "随便点一首舒缓的"}, s.store.snapshot())
	if err != nil || len(queries) != 1 || queries[0] != "示例演唱者 示例舒缓曲" || calls.Load() != 2 || sends.Load() != 0 {
		t.Fatal(queries, id, err, calls.Load(), sends.Load())
	}
	task, err := s.Task(id)
	replyRegressionCheck(t, err)
	if task.Name != "点歌推荐" || task.Source != "music-pick" || task.Model != "fixture-retry-model" {
		t.Fatal("未沿用模型中心流水线", task)
	}
}

func TestMusicPickCancellationStopsModelTask(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	s, m, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := s.pickMusic(ctx, businessRequest{Plugin: "music", Target: m.Target, Query: "随机一首"}, s.store.snapshot())
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未开始选曲")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("取消没有传递")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消后仍在等待选曲")
	}
	if sends.Load() != 0 {
		t.Fatal("选曲取消后发生发送")
	}
}

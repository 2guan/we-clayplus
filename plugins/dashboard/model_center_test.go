// 文件名：model_center_test.go
// 主要功能：验证模型流水线顺序、超时切换、独立测活、厂商密钥隔离和行为名单拒绝路径。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestContextMessageLimits(t *testing.T) {
	for _, count := range []int{501, 99999, 100000} {
		wantValid := count <= 99999
		if err := validateModel(Model{Context: count}); (err == nil) != wantValid {
			t.Fatalf("模型上下文 %d 条校验错误：%v", count, err)
		}
		if err := validateSettings("memory", map[string]any{"messages": float64(count)}); (err == nil) != wantValid {
			t.Fatalf("全局上下文 %d 条校验错误：%v", count, err)
		}
	}
}

func TestPipelineTimeoutAndExplicitProbe(t *testing.T) {
	s := newServer(t, false)
	var mu sync.Mutex
	calls := []string{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model       string  `json:"model"`
			Temperature float64 `json:"temperature"`
			Messages    []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		calls = append(calls, b.Model)
		mu.Unlock()
		if b.Model == "slow" {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		if b.Model == "good" && (b.Temperature != 0.3 || !strings.Contains(b.Messages[1].Content, "最新") || strings.Contains(b.Messages[1].Content, "过去")) {
			t.Error("模型独立参数或上下文裁剪未生效")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer remote.Close()
	temp := 0.3
	_ = s.store.putSecret("v", "fixture")
	_ = s.store.change(func(st *State) error {
		st.Models = []Model{{ID: "slow", VendorID: "v", Name: "slow", Model: "slow", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}, Timeout: 1}, {ID: "good", VendorID: "v", Name: "good", Model: "good", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}, Temperature: &temp, Context: 1}, {ID: "unused", VendorID: "v", Model: "unused", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"slow", "good", "unused"}
		return nil
	})
	id, err := s.Submit(ai.Request{Kind: "chat", Prompt: "测试", History: []string{"过去", "最新"}})
	if err != nil {
		t.Fatal(err)
	}
	task := waitModelTask(t, s, id)
	if task.Status != "completed" || task.Model != "good" {
		t.Fatalf("未故障切换 %+v", task)
	}
	mu.Lock()
	got := append([]string{}, calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"slow", "good"}) {
		t.Fatal(got)
	}
	id, err = s.Submit(ai.Request{Kind: "chat", Model: "slow", Prompt: "测活"})
	if err != nil {
		t.Fatal(err)
	}
	if waitModelTask(t, s, id).Status != "failed" {
		t.Fatal("测活被备用模型代答")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 || calls[2] != "slow" {
		t.Fatal(calls)
	}
}
func waitModelTask(t *testing.T, s *Server, id string) Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, _ := s.Task(id)
		if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("任务超时")
	return Task{}
}
func TestVendorSecretAndPolicyBoundaries(t *testing.T) {
	s := newServer(t, true)
	v := Vendor{Name: "测试厂商", Endpoint: "https://fixture.invalid/v1", Provider: "mimo", Key: "fixture-secret"}
	if err := s.modelAction(actionRequest{Kind: "vendor", Vendor: &v, Models: []Model{{Model: "fixture-chat", Capabilities: []string{"chat"}, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	state := s.store.snapshot()
	vendor := state.Vendors[len(state.Vendors)-1]
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "fixture-secret") || !vendor.Configured {
		t.Fatal("密钥泄漏或状态错误")
	}
	secret, err := s.store.getSecret(vendor.ID)
	if err != nil || secret != "fixture-secret" {
		t.Fatal("厂商密钥未加密持久化")
	}
	vendor.Endpoint = "https://different.invalid"
	if s.modelAction(actionRequest{Kind: "vendor", Vendor: &vendor}) == nil {
		t.Fatal("改变目标地址时静默复用旧密钥")
	}
	s.cfg.Capabilities = map[string]control.Capability{"contact": {Enabled: []string{"SetRemark"}}}
	policy := BehaviorPolicy{Operation: "contact.SetRemark", Enabled: true, Scope: "private", ListMode: "allow", Targets: []string{"mock-contact-1"}}
	if err := s.saveBehavior(&policy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteAI(context.Background(), policy.Operation, control.Command{Target: "mock-contact-2"}); err == nil {
		t.Fatal("绕过白名单")
	}
	if _, err := s.ExecuteAI(context.Background(), policy.Operation, control.Command{Target: "mock-contact-1"}); err != nil {
		t.Fatal(err)
	}
	policy.ListMode = "deny"
	_ = s.saveBehavior(&policy)
	if _, err := s.ExecuteAI(context.Background(), policy.Operation, control.Command{Target: "mock-contact-1"}); err == nil {
		t.Fatal("绕过黑名单")
	}
	policy.Operation = "contact.Delete"
	if s.saveBehavior(&policy) == nil {
		t.Fatal("未接通能力被授权")
	}
}

// 用不含新字段的旧快照验证升级，必须保留默认模型与原加密密钥。
func TestLegacyModelMigrationPreservesRoutingAndKey(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.putSecret("b", "fixture-legacy")
	old := initialState(false)
	old.Models = []Model{{ID: "a", Name: "a", Provider: "openai", Model: "a", Endpoint: "https://fixture.invalid", Capabilities: []string{"chat"}, Enabled: true}, {ID: "b", Name: "b", Provider: "openai", Model: "b", Endpoint: "https://fixture.invalid", Capabilities: []string{"chat"}, Enabled: true, Configured: true}}
	old.Settings["routing.chat"] = "b"
	raw, _ := json.Marshal(old)
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(raw, &doc)
	delete(doc, "vendors")
	delete(doc, "pipeline")
	delete(doc, "policies")
	raw, _ = json.Marshal(doc)
	if _, err = st.db.Exec("UPDATE documents SET body=? WHERE id='state'", raw); err != nil {
		t.Fatal(err)
	}
	st.db.Close()
	migrated, err := openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.db.Close()
	state := migrated.snapshot()
	if !reflect.DeepEqual(state.Pipeline, []string{"b", "a"}) || len(state.Vendors) != 2 || state.Models[1].VendorID != "b" {
		t.Fatalf("迁移丢失旧路由 %+v", state.Pipeline)
	}
	if key, err := migrated.getSecret("b"); err != nil || key != "fixture-legacy" {
		t.Fatal("迁移丢失密钥")
	}
}

// 媒体默认路由依次故障切换，显式测活保持隔离，并验证各流水线不能串入其他能力。
func TestMediaPipelines(t *testing.T) {
	for _, kind := range []string{"tts", "image", "video_generation"} {
		t.Run(kind, func(t *testing.T) {
			s := newServer(t, false)
			var mu sync.Mutex
			calls := []string{}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "video/mp4")
					w.Write([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"))
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				model, _ := body["model"].(string)
				mu.Lock()
				calls = append(calls, model)
				mu.Unlock()
				if model == "first" {
					http.Error(w, "unavailable", 503)
					return
				}
				switch kind {
				case "tts":
					w.Header().Set("Content-Type", "audio/wav")
					w.Write([]byte("fixture-wave"))
				case "image":
					fmt.Fprint(w, `{"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII="}]}`)
				case "video_generation":
					fmt.Fprint(w, `{"id":"video_fixture","status":"completed"}`)
				}
			}))
			defer remote.Close()
			_ = s.store.putSecret("v", "fixture")
			_ = s.store.change(func(st *State) error {
				st.Pipeline = []string{"chat-id"}
				for _, id := range []string{"first", "second", "unused"} {
					st.Models = append(st.Models, Model{ID: id, VendorID: "v", Name: id, Model: id, Provider: "openai", Endpoint: remote.URL, Capabilities: []string{kind}, Enabled: true})
				}
				return nil
			})
			if err := s.modelAction(actionRequest{Kind: "pipeline", Group: kind, Pipeline: []string{"first", "second", "unused"}}); err != nil {
				t.Fatal(err)
			}
			if err := s.modelAction(actionRequest{Kind: "pipeline", Group: "chat", Pipeline: []string{"first"}}); err == nil {
				t.Fatal("允许跨能力模型")
			}
			if !reflect.DeepEqual(s.store.snapshot().Pipeline, []string{"chat-id"}) {
				t.Fatal("覆盖了对话流水线")
			}
			id, err := s.Submit(ai.Request{Kind: kind, Prompt: "测试"})
			if err != nil {
				t.Fatal(err)
			}
			if task := waitModelTask(t, s, id); task.Status != "completed" || task.Model != "second" || !strings.HasPrefix(task.Result, "/media/") {
				t.Fatalf("媒体未切换 %+v", task)
			}
			id, err = s.Submit(ai.Request{Kind: kind, Model: "first", Prompt: "单模型"})
			if err != nil {
				t.Fatal(err)
			}
			if waitModelTask(t, s, id).Status != "failed" {
				t.Fatal("指定模型被备用代答")
			}
			mu.Lock()
			got := append([]string{}, calls...)
			mu.Unlock()
			if !reflect.DeepEqual(got, []string{"first", "second", "first"}) {
				t.Fatal(got)
			}
			if err := s.modelAction(actionRequest{Kind: "pipeline", Group: kind, Pipeline: []string{}}); err != nil {
				t.Fatal(err)
			}
			id, err = s.Submit(ai.Request{Kind: kind, Prompt: "空流水线"})
			if err != nil {
				t.Fatal(err)
			}
			if waitModelTask(t, s, id).Status != "failed" {
				t.Fatal("清空后仍调用默认模型")
			}
		})
	}
}

// 声音设计和克隆不允许在失败时降级成普通 TTS；显式指定模式也必须匹配。
func TestSpeechPipelineModeIsolation(t *testing.T) {
	s := newServer(t, true)
	_ = s.store.change(func(st *State) error {
		st.Models = []Model{{ID: "plain", Name: "普通", Model: "plain-tts", Capabilities: []string{"tts"}, Enabled: true}, {ID: "design", Name: "设计", Model: "mimo-voicedesign", Capabilities: []string{"tts"}, Enabled: true}}
		st.Pipelines = map[string][]string{"tts": {"plain", "design"}}
		return nil
	})
	id, err := s.Submit(ai.Request{Kind: "tts", SpeechMode: "design", Prompt: "你好", Design: "温柔"})
	if err != nil {
		t.Fatal(err)
	}
	if task := waitModelTask(t, s, id); task.Model != "设计" || task.Status != "completed" {
		t.Fatalf("模式错误 %+v", task)
	}
	id, err = s.Submit(ai.Request{Kind: "tts", Model: "plain", SpeechMode: "clone", Prompt: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if waitModelTask(t, s, id).Status != "failed" {
		t.Fatal("克隆降级为普通音色")
	}
}

// 服务端重启保留各媒体顺序，包括用户明确清空的流水线。
func TestMediaPipelinePersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	err = st.change(func(s *State) error {
		s.Pipelines = map[string][]string{"tts": {}, "image": {"demo-create"}, "video_generation": {"demo-create"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openStore(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	state := reopened.snapshot()
	if len(modelPipeline(state, "tts")) != 0 || !reflect.DeepEqual(modelPipeline(state, "image"), []string{"demo-create"}) || !reflect.DeepEqual(state.Pipeline, []string{"demo-chat"}) {
		t.Fatal("媒体顺序在重启后丢失")
	}
}

func TestPendingVideoDoesNotStartBackup(t *testing.T) {
	s := newServer(t, false)
	var mu sync.Mutex
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		fmt.Fprint(w, `{"id":"video_pending","status":"queued"}`)
	}))
	defer remote.Close()
	_ = s.store.putSecret("v", "fixture")
	_ = s.store.change(func(st *State) error {
		for _, id := range []string{"first", "backup"} {
			st.Models = append(st.Models, Model{ID: id, VendorID: "v", Name: id, Model: id, Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"video_generation"}, Timeout: 1})
		}
		st.Pipelines = map[string][]string{"video_generation": {"first", "backup"}}
		return nil
	})
	id, err := s.Submit(ai.Request{Kind: "video_generation", Prompt: "等待中的视频"})
	if err != nil {
		t.Fatal(err)
	}
	task := waitModelTask(t, s, id)
	mu.Lock()
	defer mu.Unlock()
	if task.Status != "failed" || calls != 1 || !strings.Contains(task.Error, "已停止自动切换") {
		t.Fatalf("重复生成 %+v %d", task, calls)
	}
}

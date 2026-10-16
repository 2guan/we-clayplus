package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestDebugModelLogsRealHTTPAndOnlyAdminCanRead(t *testing.T) {
	s := newServer(t, false)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"<think>fixture-raw-thinking</think>fixture-answer"}}]}`)
	}))
	defer remote.Close()
	if err := s.store.putSecret("fixture-debug-vendor", "fixture-debug-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(st *State) error {
		st.Settings["logging.level"] = "DEBUG"
		st.Models = []Model{{ID: "fixture-debug-model", Name: "fixture-debug-model", VendorID: "fixture-debug-vendor", Model: "fixture", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-debug-model"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, err := s.Submit(ai.Request{Kind: "chat", Prompt: "fixture-request", System: "fixture-system"})
	if err != nil {
		t.Fatal(err)
	}
	task := waitModelTask(t, s, id)
	if task.Status != "completed" || task.Result != "fixture-answer" {
		t.Fatal(task)
	}
	var logs string
	for _, l := range s.store.snapshot().Logs {
		if l.Component == "debug" {
			logs += l.Message
		}
	}
	for _, text := range []string{"模型请求原文", "模型响应原文", "fixture-request", "fixture-system", "fixture-raw-thinking", id} {
		if !strings.Contains(logs, text) {
			t.Fatal("缺少调试原文", text, logs)
		}
	}
	if strings.Contains(logs, "fixture-debug-key") {
		t.Fatal("出现凭据")
	}
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "fixture-password-123"}, nil, "")
	admin := w.Result().Cookies()[0]
	w = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-viewer", Password: "fixture-password-123", Role: "operator", Enabled: true}, admin, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "/api/auth/login", map[string]string{"name": "fixture-viewer", "password": "fixture-password-123"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "/api/state", nil, w.Result().Cookies()[0], "")
	var state State
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, l := range state.Logs {
		if l.Component == "debug" {
			t.Fatal("非管理员读到调试原文")
		}
	}
}

func TestDebugOperationLogsDoNotChangeReceiptsOrRunWhenOff(t *testing.T) {
	s := newServer(t, true)
	if err := s.store.change(func(st *State) error { st.Settings["logging.level"] = "DEBUG"; return nil }); err != nil {
		t.Fatal(err)
	}
	receipt := &struct{ Ticket string }{Ticket: "fixture-private-ticket"}
	host := debugHost{s: s, Host: replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation == "remark" {
			return nil, fmt.Errorf("fixture-operation-failed")
		}
		return receipt, nil
	})}
	for _, operation := range []string{"group-name", "remark"} {
		out, err := host.Execute(context.Background(), control.Command{Operation: operation, Target: "fixture-room", Content: "fixture-new-name", Params: json.RawMessage(`{"ticket":"fixture-private-ticket"}`)})
		if operation == "group-name" && (out != receipt || err != nil) {
			t.Fatal("回执被改变")
		}
	}
	var logs string
	count := 0
	for _, l := range s.store.snapshot().Logs {
		if l.Component == "debug" {
			logs += l.Message
			count++
		}
	}
	if count != 4 || !strings.Contains(logs, "succeeded") || !strings.Contains(logs, "fixture-operation-failed") || strings.Contains(logs, receipt.Ticket) {
		t.Fatal(logs)
	}
	if err := s.store.change(func(st *State) error { st.Settings["logging.level"] = "INFO"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, _ = host.Execute(context.Background(), control.Command{Operation: "group-name"})
	after := 0
	for _, l := range s.store.snapshot().Logs {
		if l.Component == "debug" {
			after++
		}
	}
	if after != count {
		t.Fatal("关闭后仍记录")
	}
}

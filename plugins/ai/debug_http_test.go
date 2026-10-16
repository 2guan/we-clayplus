package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/internal/debuglog"
)

func TestDebugHTTPPreservesWireAndCapturesBeforeCleaning(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var sent string
			response := `{"choices":[{"message":{"content":"<think>推理原文</think>回复原文"}}]}`
			if stream {
				response = "data: {\"choices\":[{\"delta\":{\"content\":\"<think>推理原文</think>回复原文\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
			}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				sent = string(body)
				if r.Header.Get("Authorization") != "Bearer fixture-debug-key" {
					t.Error("认证被日志改写")
				}
				w.Header().Set("Content-Type", "application/json")
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, response)
			}))
			defer remote.Close()
			var events []map[string]any
			ctx := debuglog.WithSink(context.Background(), func(_ string, fields map[string]any) { events = append(events, fields) })
			cfg := Config{Endpoint: remote.URL, Key: "fixture-debug-key", Model: "fixture", Provider: "openai"}
			q := Request{Kind: "chat", Prompt: "当前原文", System: "人设原文"}
			var result Result
			var err error
			if stream {
				q.Kind = "insight"
				result, err = ExecuteStream(ctx, cfg, q, false, func(string) {})
			} else {
				result, err = Execute(ctx, cfg, q, false)
			}
			if err != nil || result.Text != "<think>推理原文</think>回复原文" {
				t.Fatal(result, err)
			}
			if len(events) != 2 || events[0]["body"] != sent || events[1]["body"] != response || events[0]["request_id"] != events[1]["request_id"] {
				t.Fatal("没有保留完整请求/响应", events)
			}
			if strings.Contains(fmt.Sprint(events), cfg.Key) {
				t.Fatal("日志出现认证密钥")
			}
		})
	}
}

func TestDebugHTTPErrorAndMediaRedaction(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"fixture-debug-key","b64_json":"fixture-media"}`)
	}))
	defer remote.Close()
	var events []map[string]any
	ctx := debuglog.WithSink(context.Background(), func(_ string, f map[string]any) { events = append(events, f) })
	_, _, err := request(ctx, remote.URL, "fixture-debug-key", "application/json", []byte(`{"messages":[{"content":"保留正文"}],"image_url":{"url":"data:image/png;base64,fixture-media"}}`), nil)
	if err == nil || len(events) != 2 {
		t.Fatal(err, events)
	}
	logs := fmt.Sprint(events)
	if strings.Contains(logs, "fixture-debug-key") || strings.Contains(logs, "fixture-media") || !strings.Contains(logs, "保留正文") || !strings.Contains(logs, "凭据已脱敏") {
		t.Fatal(logs)
	}
	if events[1]["status"] != 400 {
		t.Fatal(events)
	}
}

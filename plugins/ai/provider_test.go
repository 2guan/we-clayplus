// 文件名：provider_test.go
// 主要功能：证明六类 Mock 能力不会产生网络请求，并校验真实协议错误与取消语义。
// 作者：2guan
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type denyTransport struct{ calls atomic.Int32 }

func (d *denyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, fmt.Errorf("测试禁止网络访问")
}
func TestMockNeverUsesNetwork(t *testing.T) {
	old := http.DefaultTransport
	blocked := &denyTransport{}
	http.DefaultTransport = blocked
	t.Cleanup(func() { http.DefaultTransport = old })
	for _, kind := range []string{"chat", "vision", "audio", "video", "tts", "image", "insight", "video_generation"} {
		result, err := Execute(context.Background(), Config{Endpoint: "http://unexpected.invalid", Key: "not-a-real-key"}, Request{Kind: kind, Prompt: "本地样例"}, true)
		if err != nil || result.Text == "" {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if blocked.calls.Load() != 0 {
		t.Fatal("Mock 发起了网络请求")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Execute(ctx, Config{}, Request{Kind: "chat"}, true); err == nil {
		t.Fatal("未遵守取消信号")
	}
}
func TestProviderHTTPFailureAndUsage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("请求契约不匹配")
		}
		if calls == 1 {
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"本机协议样例"}}],"usage":{"prompt_tokens":12,"completion_tokens":8}}`)
	}))
	defer server.Close()
	cfg := Config{Endpoint: server.URL + "/v1", Key: "fixture-key", Model: "test"}
	_, err := Execute(context.Background(), cfg, Request{Kind: "chat", Prompt: "hello"}, false)
	if !Retryable(err) {
		t.Fatalf("429 未识别：%v", err)
	}
	result, err := Execute(context.Background(), cfg, Request{Kind: "chat", Prompt: "hello"}, false)
	if err != nil || result.InputTokens != 12 || result.OutputTokens != 8 {
		t.Fatalf("未透传用量：%+v %v", result, err)
	}
}

func TestInsightStreamDetectsTruncation(t *testing.T) {
	for _, test := range []struct {
		name, events, wantError string
	}{
		{
			name:      "output limit",
			events:    `data: {"choices":[{"delta":{"content":"部分报告"},"finish_reason":"length"}]}` + "\n\n" + "data: [DONE]\n\n",
			wantError: "Token 上限",
		},
		{
			name:   "completed without done marker",
			events: `data: {"choices":[{"delta":{"content":"完整报告"},"finish_reason":"stop"}]}` + "\n\n",
		},
		{
			name:      "premature eof",
			events:    `data: {"choices":[{"delta":{"content":"部分报告"}}]}` + "\n\n",
			wantError: "提前结束",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, test.events)
			}))
			defer server.Close()
			result, err := streamChatCompletions(context.Background(), server.URL, "fixture-key", []byte(`{}`), func(string) {})
			if test.wantError == "" {
				if err != nil || result.Text != "完整报告" {
					t.Fatalf("expected complete report, got %q, %v", result.Text, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) || result.Text == "" {
				t.Fatalf("expected partial report and %q error, got %q, %v", test.wantError, result.Text, err)
			}
		})
	}
}

func TestInsightStreamAcceptsLargeReasoningPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 部分模型会把推理内容放在每个 SSE 事件中；它不应挤占正文，也不应在 32 MB 处中断。
		chunk := strings.Repeat("x", 1<<20)
		for range 33 {
			_, _ = fmt.Fprintf(w, "data: {\"reasoning_content\":\"%s\"}\n\n", chunk)
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"分析完成\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	result, err := streamChatCompletions(context.Background(), server.URL, "fixture-key", []byte(`{}`), func(string) {})
	if err != nil || result.Text != "分析完成" {
		t.Fatalf("large reasoning stream was not handled: %q, %v", result.Text, err)
	}
}

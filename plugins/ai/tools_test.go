package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToolResultsRoundTripForEveryProtocol(t *testing.T) {
	for _, provider := range []string{"openai", "mimo", "anthropic", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]json.RawMessage
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal(body)
				if calls == 1 {
					if !strings.Contains(string(raw), "target_ref") {
						t.Error("每工具的参数 schema 未发送")
					}
					switch provider {
					case "anthropic":
						fmt.Fprint(w, `{"content":[{"type":"thinking","thinking":"opaque","signature":"fixture-signature"},{"type":"tool_use","id":"fixture-call","name":"contact__Get","input":{"target_ref":"ref_fixture"}}]}`)
					case "gemini":
						if !strings.Contains(string(raw), "parametersJsonSchema") {
							t.Error("Gemini JSON Schema 字段错误")
						}
						fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"fixture-call","name":"contact__Get","args":{"target_ref":"ref_fixture"}},"thoughtSignature":"fixture-signature"}]}}]}`)
					default:
						fmt.Fprint(w, `{"choices":[{"message":{"reasoning_content":"fixture-reasoning","tool_calls":[{"id":"fixture-call","type":"function","function":{"name":"contact__Get","arguments":"{\"target_ref\":\"ref_fixture\"}"}}]}}]}`)
					}
					return
				}
				if !strings.Contains(string(raw), "fixture-call") || !strings.Contains(string(raw), "服务端查询结果") {
					t.Error("工具ID或真实结果没有回传", string(raw))
				}
				switch provider {
				case "anthropic":
					if !strings.Contains(string(raw), "fixture-signature") || !strings.Contains(string(raw), "tool_result") {
						t.Error("思考签名或工具结果丢失")
					}
					fmt.Fprint(w, `{"content":[{"type":"text","text":"已查询"}]}`)
				case "gemini":
					if !strings.Contains(string(raw), "fixture-signature") || !strings.Contains(string(raw), "functionResponse") {
						t.Error("思考签名或函数结果丢失")
					}
					fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"已查询"}]}}]}`)
				default:
					if !strings.Contains(string(raw), "tool_call_id") || !strings.Contains(string(raw), "fixture-reasoning") {
						t.Error("调用关联或推理上下文丢失")
					}
					fmt.Fprint(w, `{"choices":[{"message":{"content":"已查询"}}]}`)
				}
			}))
			defer server.Close()
			c := Config{Provider: provider, Endpoint: server.URL, Key: "fixture-key", Model: "fixture-model", Timeout: time.Second}
			q := Request{Kind: "chat", Prompt: "查找联系人", Tools: []Tool{{Name: "contact__Get", Parameters: map[string]any{"type": "object", "properties": map[string]any{"target_ref": map[string]string{"type": "string"}}}}}}
			first, e := Execute(context.Background(), c, q, false)
			if e != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "fixture-call" {
				t.Fatal(first, e)
			}
			q.ToolHistory = []ToolExchange{{Result: first, Outputs: []ToolOutput{{ID: "fixture-call", Name: "contact__Get", Content: `{"name":"服务端查询结果"}`}}}}
			final, e := Execute(context.Background(), c, q, false)
			if e != nil || final.Text != "已查询" || calls != 2 {
				t.Fatal(final, e, calls)
			}
		})
	}
}

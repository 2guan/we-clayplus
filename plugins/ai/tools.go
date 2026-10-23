package ai

import (
	"encoding/json"
	"fmt"
)

// ToolExchange 保留模型原始调用及服务端结果，后续请求不重复执行历史调用。
type ToolExchange struct {
	Result  Result
	Outputs []ToolOutput
}
type ToolOutput struct{ ID, Name, Content string }

func toolSchema(t Tool) map[string]any {
	if t.Parameters != nil {
		return t.Parameters
	}
	return map[string]any{"type": "object", "properties": map[string]any{"content": map[string]string{"type": "string"}}, "required": []string{"content"}, "additionalProperties": false}
}
func openAIToolHistory(history []ToolExchange) []any {
	out := []any{}
	for _, h := range history {
		calls := []any{}
		for _, c := range h.Result.ToolCalls {
			calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]string{"name": c.Name, "arguments": c.Arguments}})
		}
		msg := map[string]any{"role": "assistant", "content": h.Result.Text, "tool_calls": calls}
		if h.Result.Reasoning != "" {
			msg["reasoning_content"] = h.Result.Reasoning
		}
		out = append(out, msg)
		for _, o := range h.Outputs {
			out = append(out, map[string]any{"role": "tool", "tool_call_id": o.ID, "content": o.Content})
		}
	}
	return out
}
func anthropicToolHistory(history []ToolExchange) []any {
	out := []any{}
	for _, h := range history {
		var content any = h.Result.Native
		if len(h.Result.Native) == 0 {
			blocks := []any{}
			if h.Result.Text != "" {
				blocks = append(blocks, map[string]string{"type": "text", "text": h.Result.Text})
			}
			for _, c := range h.Result.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(c.Arguments), &args)
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": args})
			}
			content = blocks
		}
		out = append(out, map[string]any{"role": "assistant", "content": content})
		blocks := []any{}
		for _, o := range h.Outputs {
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": o.ID, "content": o.Content})
		}
		out = append(out, map[string]any{"role": "user", "content": blocks})
	}
	return out
}
func geminiToolHistory(history []ToolExchange) []any {
	out := []any{}
	for _, h := range history {
		var parts any = h.Result.Native
		if len(h.Result.Native) == 0 {
			blocks := []any{}
			for _, c := range h.Result.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(c.Arguments), &args)
				blocks = append(blocks, map[string]any{"functionCall": map[string]any{"name": c.Name, "args": args, "id": c.ID}})
			}
			parts = blocks
		}
		out = append(out, map[string]any{"role": "model", "parts": parts})
		blocks := []any{}
		for _, o := range h.Outputs {
			var data any
			if json.Unmarshal([]byte(o.Content), &data) != nil {
				data = map[string]string{"text": o.Content}
			}
			blocks = append(blocks, map[string]any{"functionResponse": map[string]any{"name": o.Name, "id": o.ID, "response": data}})
		}
		out = append(out, map[string]any{"role": "user", "parts": blocks})
	}
	return out
}

func nativeTools(provider string, tools []Tool) []any {
	out := []any{}
	for _, t := range tools {
		switch provider {
		case "anthropic":
			out = append(out, map[string]any{"name": t.Name, "description": t.Description, "input_schema": toolSchema(t)})
		case "gemini":
			out = append(out, map[string]any{"name": t.Name, "description": t.Description, "parametersJsonSchema": toolSchema(t)})
		default:
			out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": toolSchema(t)}})
		}
	}
	return out
}

// parseNativeResult 保留签名/思考块，以便原样回传供应商要求的工具上下文。
func parseNativeResult(provider string, raw []byte) (Result, error) {
	var v struct {
		Content    json.RawMessage `json:"content"`
		Candidates []struct {
			Content struct {
				Parts json.RawMessage `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		GeminiUsage struct {
			Input  int `json:"promptTokenCount"`
			Output int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return Result{}, fmt.Errorf("模型响应格式无效")
	}
	result := Result{InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output, Native: v.Content}
	if provider == "gemini" {
		if len(v.Candidates) == 0 {
			return result, fmt.Errorf("模型没有返回可用内容")
		}
		result.Native = v.Candidates[0].Content.Parts
		result.InputTokens = v.GeminiUsage.Input
		result.OutputTokens = v.GeminiUsage.Output
	}
	var blocks []struct {
		Type, ID, Name, Text string
		Thought              bool            `json:"thought"`
		Input                json.RawMessage `json:"input"`
		Call                 *struct {
			Name, ID string
			Args     json.RawMessage `json:"args"`
		} `json:"functionCall"`
	}
	if json.Unmarshal(result.Native, &blocks) != nil {
		return Result{}, fmt.Errorf("模型响应内容无效")
	}
	for _, b := range blocks {
		if (provider == "anthropic" && b.Type == "text") || (provider == "gemini" && !b.Thought) {
			result.Text += b.Text
		}
		if b.Type == "tool_use" {
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: string(b.Input)})
		}
		if b.Call != nil {
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: b.Call.ID, Name: b.Call.Name, Arguments: string(b.Call.Args)})
		}
	}
	for i := range result.ToolCalls {
		if result.ToolCalls[i].ID == "" {
			result.ToolCalls[i].ID = fmt.Sprintf("call_%d", i)
		}
	}
	if result.Text == "" && len(result.ToolCalls) == 0 {
		return result, fmt.Errorf("模型返回空内容")
	}
	return result, nil
}

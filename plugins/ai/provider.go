// 文件名：provider.go
// 主要功能：封装六类 AI 能力的真实 HTTP 调用与完全离线的模拟执行。
// 作者：2guan
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Request 使用通用媒体输入；模型配置和 API Key 仅存在于服务端。
// Tool 只描述服务端已授权的有限动作；模型返回的参数仍需执行器重新校验。
type Tool struct {
	Name        string
	Description string
}
type ToolCall struct {
	Name      string
	Arguments string
}

// InputImage 保存缩略图字节及对应朋友圈标识，避免把受保护的本机 URL 交给厂商。
type InputImage struct {
	Label string `json:"label"`
	MIME  string `json:"mime"`
	Data  string `json:"data"`
}
type Request struct {
	Images []InputImage `json:"images,omitempty"`
	// 来源、上下文身份与检查状态仅由服务端构造，HTTP 客户端不能伪造。
	InputID   string                   `json:"-"` // 当前入站媒体的归档标识，仅服务端赋值。
	InputKind string                   `json:"-"`
	Source    string                   `json:"-"`
	Context   []contextcleaner.Message `json:"-"`
	// Documents 是待分析资料，永远作为低信任数据处理，不成为系统指令。
	Documents []contextcleaner.Message `json:"documents,omitempty"`
	Tools     []Tool                   `json:"-"`

	SpeechMode string   `json:"speech_mode,omitempty"`
	Seconds    string   `json:"seconds,omitempty"`
	Voice      string   `json:"voice,omitempty"`
	Design     string   `json:"design,omitempty"`
	Size       string   `json:"size,omitempty"`
	Selfie     bool     `json:"selfie,omitempty"`
	History    []string `json:"history,omitempty"` // 由自动回复入口构造，按具体候选模型裁剪。
	Kind       string   `json:"kind"`
	Prompt     string   `json:"prompt"`
	System     string   `json:"system,omitempty"`
	Media      string   `json:"media,omitempty"`
	MIME       string   `json:"mime,omitempty"`
	Filename   string   `json:"filename,omitempty"`
	Model      string   `json:"model,omitempty"`
	Target     string   `json:"target,omitempty"`
}
type Config struct {
	Endpoint, Key, Model, Provider, Voice, Size string
	Temperature                                 *float64
	Thinking                                    string
	Design                                      string
	Quality                                     string
	Speed                                       float64
	Timeout                                     time.Duration
	MaxOutput                                   int
}
type Result struct {
	ToolCalls []ToolCall

	Text         string
	Data         []byte
	MIME         string
	InputTokens  int
	OutputTokens int
}
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string {
	return fmt.Sprintf("模型服务返回 HTTP %d；请检查连接、额度和模型权限", e.Status)
}
func Retryable(err error) bool {
	var e *HTTPError
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &e) && (e.Status == 429 || e.Status >= 500)
}

// Execute 的 Mock 分支位于任何网络与媒体读取之前，可通过测试证明不会外发。
func Execute(ctx context.Context, c Config, r Request, mock bool) (Result, error) {
	if mock {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(450 * time.Millisecond):
		}
		text := map[string]string{"chat": "这是脱机演示回复。你的消息已经进入独立的模拟会话，可以继续测试人工接管、专属策略和消息操作。", "vision": "【模拟识图】画面由柔和的绿色山峦、树林与水面组成，整体安静、明亮。此结果为固定示例，未上传图片。", "audio": "【模拟转写】把细节慢慢做好，就是最好的设计。\n音频未发送到任何外部服务。", "video": "【模拟视频分析】\n00:00 画面出现远山与水面。\n00:04 镜头逐渐靠近树木。\n00:08 画面结束。\n此时间线为虚构演示，不代表上传视频的实际内容。", "tts": "/sample-audio.wav", "image": "/sample-landscape.svg", "video_generation": "/sample-video.mp4", "insight": "会话摘要\n\n大家围绕界面布局、色彩和操作顺序交换了意见。\n\n可以跟进：\n1. 确认高频操作的位置。\n2. 对比亮色和暗色主题。\n3. 在手机上测试输入和返回。\n\n依据：脱机示例数据；不对真实个人作性格判断。"}[r.Kind]
		if text == "" {
			return Result{}, fmt.Errorf("未知 AI 能力")
		}
		return Result{Text: text, InputTokens: 128, OutputTokens: 96}, nil
	}
	// 多模态请求必须确实携带相应媒体，不能把纯文本响应伪装成识图或识视频结果。
	expected := map[string]string{"vision": "image/", "audio": "audio/", "video": "video/"}[r.Kind]
	if expected != "" {
		data, e := base64.StdEncoding.DecodeString(r.Media)
		if e != nil || len(data) == 0 || len(data) > 16<<20 || !strings.HasPrefix(r.MIME, expected) {
			return Result{}, fmt.Errorf("请提供有效的对应媒体，大小不超过 16 MB")
		}
	}
	if c.Speed <= 0 {
		c.Speed = 1
	}
	if strings.TrimSpace(c.Key) == "" || strings.TrimSpace(c.Model) == "" {
		return Result{}, fmt.Errorf("模型凭据或模型 ID 尚未配置")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" {
		return Result{}, fmt.Errorf("模型 Endpoint 无效")
	}
	if c.Timeout <= 0 {
		c.Timeout = 90 * time.Second
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = 2048
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	if r.Voice != "" {
		c.Voice = r.Voice
	}
	if r.Design != "" {
		c.Design = r.Design
	}
	if r.Size != "" {
		c.Size = r.Size
	}
	if r.Kind == "video_generation" {
		return generateVideo(ctx, c, r)
	}
	if c.Provider == "mimo" && r.Kind == "tts" {
		return mimoSpeech(ctx, c, r)
	}
	if c.Provider == "gemini" {
		return gemini(ctx, c, r)
	}
	if c.Provider == "anthropic" {
		return anthropic(ctx, c, r)
	}
	endpoint := strings.TrimRight(c.Endpoint, "/")
	var body any
	path := ""
	var raw []byte
	contentType := "application/json"
	switch r.Kind {
	case "chat", "insight", "vision":
		var content any = r.Prompt
		if r.Kind == "vision" {
			if r.Media == "" || !strings.HasPrefix(r.MIME, "image/") {
				return Result{}, fmt.Errorf("请上传有效图片")
			}
			content = []any{map[string]any{"type": "text", "text": r.Prompt}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + r.MIME + ";base64," + r.Media}}}
		}
		if len(r.Images) > 0 {
			parts := []any{map[string]any{"type": "text", "text": r.Prompt}}
			for _, im := range r.Images {
				parts = append(parts, map[string]any{"type": "text", "text": im.Label}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + im.MIME + ";base64," + im.Data}})
			}
			content = parts
		}
		body = map[string]any{"model": c.Model, "messages": []any{map[string]any{"role": "system", "content": r.System}, map[string]any{"role": "user", "content": content}}, "max_tokens": c.MaxOutput}
		parameters := body.(map[string]any)
		if len(r.Tools) > 0 {
			tools := []any{}
			for _, tool := range r.Tools {
				tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": map[string]any{"type": "object", "properties": map[string]any{"content": map[string]string{"type": "string", "description": "操作内容"}}, "required": []string{"content"}, "additionalProperties": false}}})
			}
			parameters["tools"] = tools
			parameters["parallel_tool_calls"] = false
		}
		if c.Temperature != nil && !(c.Provider == "mimo" && c.Thinking != "off") {
			parameters["temperature"] = *c.Temperature
		}
		if c.Thinking != "" && c.Thinking != "inherit" {
			if c.Provider == "mimo" {
				parameters["thinking"] = map[string]string{"type": map[bool]string{true: "enabled", false: "disabled"}[c.Thinking == "on"]}
			} else {
				parameters["reasoning_effort"] = map[bool]string{true: "medium", false: "none"}[c.Thinking == "on"]
			}
		}
		path = "/chat/completions"
	case "audio":
		// MiMo 音频理解使用聊天接口的 input_audio，不能上传到 OpenAI 转写接口。
		if c.Provider == "mimo" {
			parameters := map[string]any{"model": c.Model, "max_completion_tokens": c.MaxOutput, "messages": []any{
				map[string]any{"role": "system", "content": r.System},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": "data:" + r.MIME + ";base64," + r.Media}}, map[string]string{"type": "text", "text": r.Prompt}}},
			}}
			if c.Thinking == "on" || c.Thinking == "off" {
				parameters["thinking"] = map[string]string{"type": map[bool]string{true: "enabled", false: "disabled"}[c.Thinking == "on"]}
			}
			if c.Temperature != nil && c.Thinking == "off" {
				parameters["temperature"] = *c.Temperature
			}
			body = parameters
			path = "/chat/completions"
			break
		}
		data, e := base64.StdEncoding.DecodeString(r.Media)
		if e != nil || len(data) == 0 {
			return Result{}, fmt.Errorf("音频数据无效")
		}
		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		_ = writer.WriteField("model", c.Model)
		name := r.Filename
		if name == "" {
			name = "audio.wav"
		}
		part, e := writer.CreateFormFile("file", name)
		if e != nil {
			return Result{}, e
		}
		_, _ = part.Write(data)
		_ = writer.Close()
		raw = b.Bytes()
		contentType = writer.FormDataContentType()
		path = "/audio/transcriptions"
	case "tts":
		body = map[string]any{"model": c.Model, "input": r.Prompt, "voice": c.Voice, "response_format": "wav", "speed": c.Speed}
		path = "/audio/speech"
	case "image":
		body = map[string]any{"model": c.Model, "prompt": r.Prompt, "size": c.Size, "n": 1, "response_format": "b64_json"}
		path = "/images/generations"
		if c.Quality != "" {
			body.(map[string]any)["quality"] = c.Quality
		}
		if strings.HasPrefix(c.Model, "gpt-image") {
			delete(body.(map[string]any), "response_format")
		}
		if r.Media != "" {
			var e error
			raw, contentType, e = imageEdit(c, r)
			if e != nil {
				return Result{}, e
			}
			path = "/images/edits"
		}
	case "video":
		return Result{}, fmt.Errorf("此协议未实现原生视频理解，请选择 Gemini 视频模型")
	default:
		return Result{}, fmt.Errorf("未知 AI 能力")
	}
	if raw == nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return Result{}, err
		}
	}
	response, mime, err := request(ctx, endpoint+path, c.Key, contentType, raw, nil)
	if err != nil {
		return Result{}, err
	}
	if r.Kind == "tts" {
		if !strings.HasPrefix(mime, "audio/") && mime != "application/octet-stream" {
			return Result{}, fmt.Errorf("服务未返回有效音频")
		}
		return Result{Data: response, MIME: "audio/wav"}, nil
	}
	var v struct {
		Text    string `json:"text"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Calls   []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Data []struct {
			Base64 string `json:"b64_json"`
		} `json:"data"`
		Usage struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(response, &v) != nil {
		return Result{}, fmt.Errorf("模型响应格式无效")
	}
	result := Result{Text: v.Text, InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}
	if len(v.Choices) > 0 {
		result.Text = v.Choices[0].Message.Content
		for _, call := range v.Choices[0].Message.Calls {
			result.ToolCalls = append(result.ToolCalls, ToolCall{Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
	}
	if r.Kind == "image" {
		if len(v.Data) == 0 || v.Data[0].Base64 == "" {
			return Result{}, fmt.Errorf("模型未返回内嵌图片；请使用支持 b64_json 的接口")
		}
		result.Data, err = base64.StdEncoding.DecodeString(v.Data[0].Base64)
		result.MIME = "image/png"
		return result, err
	}
	if strings.TrimSpace(result.Text) == "" && len(result.ToolCalls) == 0 {
		return Result{}, fmt.Errorf("模型返回了空内容或拒绝了本次请求")
	}
	return result, nil
}

// request 限制响应大小且拒绝重定向，避免将认证头发送到另一个地址。
func request(ctx context.Context, endpoint, key, contentType string, body []byte, headers map[string]string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", contentType)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("模型连接失败或超时：%w", safeConnectionError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &HTTPError{resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil || len(raw) > 32<<20 {
		return nil, "", fmt.Errorf("模型响应读取失败或超过 32 MB")
	}
	return raw, resp.Header.Get("Content-Type"), nil
}
func gemini(ctx context.Context, c Config, r Request) (Result, error) {
	if r.Kind == "tts" || r.Kind == "image" {
		return Result{}, fmt.Errorf("当前 Gemini 适配器只开放文本与多模态理解")
	}
	parts := []any{map[string]string{"text": r.Prompt}}
	if r.Media != "" {
		parts = append(parts, map[string]any{"inline_data": map[string]string{"mime_type": r.MIME, "data": r.Media}})
	}
	for _, im := range r.Images {
		parts = append(parts, map[string]string{"text": im.Label}, map[string]any{"inline_data": map[string]string{"mime_type": im.MIME, "data": im.Data}})
	}
	body := map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "system_instruction": map[string]any{"parts": []any{map[string]string{"text": r.System}}}, "generationConfig": map[string]any{"maxOutputTokens": c.MaxOutput}}
	generation := body["generationConfig"].(map[string]any)
	if c.Temperature != nil {
		generation["temperature"] = *c.Temperature
	}
	if c.Thinking != "" && c.Thinking != "inherit" {
		generation["thinkingConfig"] = map[string]int{"thinkingBudget": map[bool]int{true: -1, false: 0}[c.Thinking == "on"]}
	}
	raw, _ := json.Marshal(body)
	response, _, err := request(ctx, strings.TrimRight(c.Endpoint, "/")+"/models/"+url.PathEscape(c.Model)+":generateContent", "", "application/json", raw, map[string]string{"x-goog-api-key": c.Key})
	if err != nil {
		return Result{}, err
	}
	var v struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage struct {
			Input  int `json:"promptTokenCount"`
			Output int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(response, &v) != nil || len(v.Candidates) == 0 {
		return Result{}, fmt.Errorf("模型没有返回可用内容")
	}
	var text strings.Builder
	for _, p := range v.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	if text.Len() == 0 {
		return Result{}, fmt.Errorf("模型返回空内容")
	}
	return Result{Text: text.String(), InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}, nil
}
func anthropic(ctx context.Context, c Config, r Request) (Result, error) {
	if r.Kind != "chat" && r.Kind != "vision" && r.Kind != "insight" {
		return Result{}, fmt.Errorf("此适配器仅支持对话和识图")
	}
	content := []any{}
	if r.Media != "" {
		content = append(content, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": r.MIME, "data": r.Media}})
	}
	for _, im := range r.Images {
		content = append(content, map[string]string{"type": "text", "text": im.Label}, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": im.MIME, "data": im.Data}})
	}
	content = append(content, map[string]string{"type": "text", "text": r.Prompt})
	body := map[string]any{"model": c.Model, "max_tokens": c.MaxOutput, "system": r.System, "messages": []any{map[string]any{"role": "user", "content": content}}}
	if c.Thinking == "on" {
		if c.MaxOutput <= 1024 {
			return Result{}, fmt.Errorf("Anthropic 思考模式需要最大输出大于 1024")
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1024}
	} else if c.Temperature != nil {
		body["temperature"] = *c.Temperature
	}
	raw, _ := json.Marshal(body)
	response, _, err := request(ctx, strings.TrimRight(c.Endpoint, "/")+"/messages", "", "application/json", raw, map[string]string{"x-api-key": c.Key, "anthropic-version": "2023-06-01"})
	if err != nil {
		return Result{}, err
	}
	var v struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(response, &v) != nil {
		return Result{}, fmt.Errorf("模型响应格式无效")
	}
	var text strings.Builder
	for _, p := range v.Content {
		text.WriteString(p.Text)
	}
	if text.Len() == 0 {
		return Result{}, fmt.Errorf("模型返回空内容")
	}
	return Result{Text: text.String(), InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}, nil
}

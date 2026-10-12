// 文件名: plugins/ai/provider/claude.go
// 主要功能: Anthropic Claude 3.5 系列官方 API 消息驱动实现
// 作者: 2guan

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClaudeProvider Anthropic Claude 驱动
type ClaudeProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClaudeProvider 实例化 Claude 驱动
func NewClaudeProvider(apiKey, baseURL string, timeoutSec int) *ClaudeProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return &ClaudeProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Name 返回提供商标识
func (c *ClaudeProvider) Name() string {
	return "claude"
}

type claudeContentBlock struct {
	Type   string            `json:"type"`
	Text   string            `json:"text,omitempty"`
	Source *claudeImageSource `json:"source,omitempty"`
}

type claudeImageSource struct {
	Type      string `json:"type"`       // "base64" or "url"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png"
	Data      string `json:"data"`
}

type claudeMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeChatRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float32         `json:"temperature,omitempty"`
	System      string          `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
}

type claudeChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Chat 执行 Claude 对话请求
func (c *ClaudeProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	var systemPrompt string
	var messages []claudeMessage

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemPrompt += m.Content + "\n"
			continue
		}

		role := "user"
		if m.Role == RoleAssistant {
			role = "assistant"
		}

		var blocks []claudeContentBlock
		if len(m.Parts) == 0 {
			blocks = append(blocks, claudeContentBlock{
				Type: "text",
				Text: m.Content,
			})
		} else {
			for _, p := range m.Parts {
				if p.Type == PartTypeImage {
					// 提取 base64 或 URL
					mediaType := "image/jpeg"
					b64Data := p.ImageURL
					if strings.HasPrefix(b64Data, "data:") {
						parts := strings.SplitN(b64Data, ";base64,", 2)
						if len(parts) == 2 {
							mediaType = strings.TrimPrefix(parts[0], "data:")
							b64Data = parts[1]
						}
					}
					blocks = append(blocks, claudeContentBlock{
						Type: "image",
						Source: &claudeImageSource{
							Type:      "base64",
							MediaType: mediaType,
							Data:      b64Data,
						},
					})
				} else if p.Text != "" {
					blocks = append(blocks, claudeContentBlock{
						Type: "text",
						Text: p.Text,
					})
				}
			}
		}

		messages = append(messages, claudeMessage{
			Role:    role,
			Content: blocks,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	payload := claudeChatRequest{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      strings.TrimSpace(systemPrompt),
		Messages:    messages,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("[claude] 序列化请求失败: %w", err)
	}

	endpoint := fmt.Sprintf("%s/messages", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("[claude] 创建 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("[claude] 请求网络失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[claude] 读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[claude] 返回错误状态码 %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp claudeChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("[claude] 解析响应失败: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return nil, fmt.Errorf("[claude] 服务端返回错误: %s", chatResp.Error.Message)
	}

	var replyText strings.Builder
	for _, block := range chatResp.Content {
		if block.Type == "text" {
			replyText.WriteString(block.Text)
		}
	}

	return &ChatResponse{
		Content:          strings.TrimSpace(replyText.String()),
		PromptTokens:     chatResp.Usage.InputTokens,
		CompletionTokens: chatResp.Usage.OutputTokens,
		TotalTokens:      chatResp.Usage.InputTokens + chatResp.Usage.OutputTokens,
		Model:            chatResp.Model,
		Duration:         time.Since(start),
	}, nil
}

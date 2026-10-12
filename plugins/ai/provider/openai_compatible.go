// 文件名: plugins/ai/provider/openai_compatible.go
// 主要功能: 通用 OpenAI 兼容协议客户端驱动实现（支持流式/非流式、多模态视觉图文理解与 Token 统计）
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

// OpenAICompatibleConfig 基础连接配置
type OpenAICompatibleConfig struct {
	Name       string
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// OpenAICompatibleClient 通用 OpenAI 兼容驱动
type OpenAICompatibleClient struct {
	name       string
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewOpenAICompatibleClient 实例化通用客户端
func NewOpenAICompatibleClient(cfg OpenAICompatibleConfig) *OpenAICompatibleClient {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") && !strings.Contains(baseURL, "/v1/") {
		baseURL = baseURL + "/v1"
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	return &OpenAICompatibleClient{
		name:       cfg.Name,
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		httpClient: client,
	}
}

// Name 返回提供商标识
func (c *OpenAICompatibleClient) Name() string {
	return c.name
}

// OpenAI 协议内部传输结构体
type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string 或 []openAIContentPart
}

type openAIContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float32         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int `json:"index"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

// Chat 发送请求并解析回复
func (c *OpenAICompatibleClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	// 1. 组装请求消息体，转换为 OpenAI 格式
	var apiMsgs []openAIMessage
	for _, m := range req.Messages {
		if len(m.Parts) == 0 {
			// 单一文本格式
			apiMsgs = append(apiMsgs, openAIMessage{
				Role:    string(m.Role),
				Content: m.Content,
			})
		} else {
			// 多模态复合片段格式
			var parts []openAIContentPart
			for _, p := range m.Parts {
				switch p.Type {
				case PartTypeText:
					parts = append(parts, openAIContentPart{
						Type: "text",
						Text: p.Text,
					})
				case PartTypeImage:
					parts = append(parts, openAIContentPart{
						Type: "image_url",
						ImageURL: &openAIImageURL{
							URL: p.ImageURL,
						},
					})
				default:
					if p.Text != "" {
						parts = append(parts, openAIContentPart{
							Type: "text",
							Text: p.Text,
						})
					}
				}
			}
			apiMsgs = append(apiMsgs, openAIMessage{
				Role:    string(m.Role),
				Content: parts,
			})
		}
	}

	payload := openAIChatRequest{
		Model:       req.Model,
		Messages:    apiMsgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("[%s] 序列化请求体失败: %w", c.name, err)
	}

	endpoint := fmt.Sprintf("%s/chat/completions", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("[%s] 创建 HTTP 请求失败: %w", c.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	// 2. 执行网络调用
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("[%s] 请求网络失败: %w", c.name, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[%s] 读取响应数据失败: %w", c.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[%s] 请求返回异常状态码 %d: %s", c.name, resp.StatusCode, string(respBytes))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("[%s] 反序列化响应失败: %w (响应内容: %s)", c.name, err, string(respBytes))
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return nil, fmt.Errorf("[%s] 服务端返回错误: %s", c.name, chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("[%s] 服务端未返回有效生成的回复 choices", c.name)
	}

	replyContent := chatResp.Choices[0].Message.Content
	duration := time.Since(start)

	return &ChatResponse{
		Content:          strings.TrimSpace(replyContent),
		PromptTokens:     chatResp.Usage.PromptTokens,
		CompletionTokens: chatResp.Usage.CompletionTokens,
		TotalTokens:      chatResp.Usage.TotalTokens,
		Model:            chatResp.Model,
		Duration:         duration,
	}, nil
}

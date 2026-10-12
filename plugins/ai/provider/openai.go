// 文件名: plugins/ai/provider/openai.go
// 主要功能: OpenAI 官方 GPT-4o / o3-mini 多模态模型驱动封装
// 作者: 2guan

package provider

import (
	"time"
)

// OpenAIProvider OpenAI 模型驱动
type OpenAIProvider struct {
	*OpenAICompatibleClient
}

// NewOpenAIProvider 实例化 OpenAI 驱动
func NewOpenAIProvider(apiKey, baseURL string, timeoutSec int) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	client := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		Name:    "openai",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: timeout,
	})

	return &OpenAIProvider{OpenAICompatibleClient: client}
}

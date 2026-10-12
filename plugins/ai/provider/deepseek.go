// 文件名: plugins/ai/provider/deepseek.go
// 主要功能: DeepSeek 官方 API 高速推理与对话驱动封装
// 作者: 2guan

package provider

import (
	"time"
)

// DeepSeekProvider DeepSeek 模型驱动
type DeepSeekProvider struct {
	*OpenAICompatibleClient
}

// NewDeepSeekProvider 实例化 DeepSeek 驱动
func NewDeepSeekProvider(apiKey, baseURL string, timeoutSec int) *DeepSeekProvider {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	client := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		Name:    "deepseek",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: timeout,
	})

	return &DeepSeekProvider{OpenAICompatibleClient: client}
}

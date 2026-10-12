// 文件名: plugins/ai/provider/qwen.go
// 主要功能: 阿里通义千问 DashScope 兼容接口驱动实现
// 作者: 2guan

package provider

import (
	"time"
)

// QwenProvider 通义千问模型驱动
type QwenProvider struct {
	*OpenAICompatibleClient
}

// NewQwenProvider 实例化通义千问驱动
func NewQwenProvider(apiKey, baseURL string, timeoutSec int) *QwenProvider {
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	client := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		Name:    "qwen",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: timeout,
	})

	return &QwenProvider{OpenAICompatibleClient: client}
}

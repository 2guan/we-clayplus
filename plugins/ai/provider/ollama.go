// 文件名: plugins/ai/provider/ollama.go
// 主要功能: 本地私有化 Ollama 大模型驱动实现
// 作者: 2guan

package provider

import (
	"time"
)

// OllamaProvider 本地私有化 Ollama 驱动
type OllamaProvider struct {
	*OpenAICompatibleClient
}

// NewOllamaProvider 实例化 Ollama 驱动
func NewOllamaProvider(apiKey, baseURL string, timeoutSec int) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	if apiKey == "" {
		apiKey = "ollama"
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	client := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		Name:    "ollama",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Timeout: timeout,
	})

	return &OllamaProvider{OpenAICompatibleClient: client}
}

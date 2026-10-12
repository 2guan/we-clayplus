// 文件名: plugins/ai/router.go
// 主要功能: 多模态大模型路由网关、故障容灾自动降级转移与模型联通性测试
// 作者: 2guan

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/we-clay/we-clay/plugins/ai/provider"
)

// Router 大模型统一路由与容灾调度器
type Router struct {
	mu           sync.RWMutex
	providers    map[string]provider.Provider
	mimoProvider *provider.MimoProvider
	modelChain   []string
	defaultProv  string
	defaultModel string
}

// NewRouter 根据配置初始化路由网关
func NewRouter(cfg *Config) *Router {
	r := &Router{
		providers:    make(map[string]provider.Provider),
		modelChain:   cfg.ModelChain,
		defaultProv:  cfg.DefaultProvider,
		defaultModel: cfg.DefaultModel,
	}

	// 1. 初始化小米 mimo
	if cfg.Mimo.Enabled {
		apiKey := cfg.Mimo.ResolveAPIKey()
		mimo := provider.NewMimoProvider(apiKey, cfg.Mimo.BaseURL, cfg.Mimo.Model, cfg.Mimo.AudioModel, cfg.Mimo.TimeoutSec)
		r.providers["mimo"] = mimo
		r.mimoProvider = mimo
	}

	// 2. 初始化 DeepSeek
	if cfg.DeepSeek.Enabled {
		apiKey := cfg.DeepSeek.ResolveAPIKey()
		r.providers["deepseek"] = provider.NewDeepSeekProvider(apiKey, cfg.DeepSeek.BaseURL, cfg.DeepSeek.TimeoutSec)
	}

	// 3. 初始化 OpenAI
	if cfg.OpenAI.Enabled {
		apiKey := cfg.OpenAI.ResolveAPIKey()
		r.providers["openai"] = provider.NewOpenAIProvider(apiKey, cfg.OpenAI.BaseURL, cfg.OpenAI.TimeoutSec)
	}

	// 4. 初始化 Claude
	if cfg.Claude.Enabled {
		apiKey := cfg.Claude.ResolveAPIKey()
		r.providers["claude"] = provider.NewClaudeProvider(apiKey, cfg.Claude.BaseURL, cfg.Claude.TimeoutSec)
	}

	// 5. 初始化 通义千问
	if cfg.Qwen.Enabled {
		apiKey := cfg.Qwen.ResolveAPIKey()
		r.providers["qwen"] = provider.NewQwenProvider(apiKey, cfg.Qwen.BaseURL, cfg.Qwen.TimeoutSec)
	}

	// 6. 初始化 Ollama
	if cfg.Ollama.Enabled {
		apiKey := cfg.Ollama.ResolveAPIKey()
		r.providers["ollama"] = provider.NewOllamaProvider(apiKey, cfg.Ollama.BaseURL, cfg.Ollama.TimeoutSec)
	}

	return r
}

// Generate 执行对话生成，支持目标专属模型覆盖与多服务商容灾故障转移
func (r *Router) Generate(ctx context.Context, overrideModel string, messages []provider.ChatMessage, temp float32, maxTokens int) (*provider.ChatResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. 确定本次调用的提供商候选序列
	var candidateProviders []string

	// 若专属策略指定了模型名，尝试推断或匹配提供商
	if overrideModel != "" {
		lowerModel := strings.ToLower(overrideModel)
		switch {
		case strings.Contains(lowerModel, "mimo"):
			candidateProviders = append(candidateProviders, "mimo")
		case strings.Contains(lowerModel, "deepseek"):
			candidateProviders = append(candidateProviders, "deepseek")
		case strings.Contains(lowerModel, "gpt") || strings.Contains(lowerModel, "o3") || strings.Contains(lowerModel, "openai"):
			candidateProviders = append(candidateProviders, "openai")
		case strings.Contains(lowerModel, "claude"):
			candidateProviders = append(candidateProviders, "claude")
		case strings.Contains(lowerModel, "qwen"):
			candidateProviders = append(candidateProviders, "qwen")
		case strings.Contains(lowerModel, "llama") || strings.Contains(lowerModel, "ollama"):
			candidateProviders = append(candidateProviders, "ollama")
		}
	}

	// 加入默认提供商及故障转移链
	if r.defaultProv != "" {
		candidateProviders = append(candidateProviders, r.defaultProv)
	}
	candidateProviders = append(candidateProviders, r.modelChain...)

	// 去重且保持优先级顺序
	seen := make(map[string]bool)
	var finalCandidates []string
	for _, pName := range candidateProviders {
		pName = strings.TrimSpace(pName)
		if pName != "" && !seen[pName] {
			seen[pName] = true
			if _, ok := r.providers[pName]; ok {
				finalCandidates = append(finalCandidates, pName)
			}
		}
	}

	if len(finalCandidates) == 0 {
		return nil, fmt.Errorf("无可用的大模型提供商，请在控制台配置 API Key 并启用模型")
	}

	var lastErr error
	for _, pName := range finalCandidates {
		p := r.providers[pName]
		if p == nil {
			continue
		}

		// 决定模型名称
		modelName := r.defaultModel
		if overrideModel != "" {
			modelName = overrideModel
		}

		req := &provider.ChatRequest{
			Model:       modelName,
			Messages:    messages,
			Temperature: temp,
			MaxTokens:   maxTokens,
		}

		resp, err := p.Chat(ctx, req)
		if err == nil && resp != nil && resp.Content != "" {
			return resp, nil
		}

		lastErr = err
		slog.Warn("[ai router] 提供商调用失败，尝试容灾降级",
			"provider", pName,
			"model", modelName,
			"err", err)
	}

	return nil, fmt.Errorf("所有候选模型提供商调用均失败，最后错误: %w", lastErr)
}

// SynthesizeSpeech 通过 mimo 驱动进行语音合成
func (r *Router) SynthesizeSpeech(ctx context.Context, text string, voice string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.mimoProvider == nil {
		return nil, fmt.Errorf("未配置或未启用小米 mimo 驱动，无法进行语音合成")
	}
	return r.mimoProvider.SynthesizeSpeech(ctx, text, voice)
}

// TestProvider 测试指定提供商的联通性与响应延时
func (r *Router) TestProvider(ctx context.Context, name string) (*provider.ChatResponse, error) {
	r.mu.RLock()
	p, ok := r.providers[name]
	r.mu.RUnlock()

	if !ok || p == nil {
		return nil, fmt.Errorf("未找到名为 %s 的大模型提供商", name)
	}

	testReq := &provider.ChatRequest{
		Model: r.defaultModel,
		Messages: []provider.ChatMessage{
			{
				Role:    provider.RoleUser,
				Content: "你好，这是一条连接测试消息，请回复一句话证实服务正常。",
			},
		},
		Temperature: 0.3,
		MaxTokens:   50,
	}

	return p.Chat(ctx, testReq)
}

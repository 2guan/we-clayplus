// 文件名: plugins/ai/provider/provider.go
// 主要功能: 大模型统一多模态服务商接口与数据传输对象 (DTO) 定义
// 作者: 2guan

package provider

import (
	"context"
	"time"
)

// MessageRole 消息角色枚举
type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ContentPartType 多模态消息片段类型
type ContentPartType string

const (
	PartTypeText  ContentPartType = "text"
	PartTypeImage ContentPartType = "image_url"
	PartTypeAudio ContentPartType = "audio_url"
	PartTypeVideo ContentPartType = "video_url"
)

// ContentPart 承载多模态信息的单个片段（纯文本或媒体引用）
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL string          `json:"image_url,omitempty"` // URL 或 data:image/jpeg;base64,...
	AudioURL string          `json:"audio_url,omitempty"`
	VideoURL string          `json:"video_url,omitempty"`
}

// ChatMessage 单条对话消息实体
type ChatMessage struct {
	Role    MessageRole   `json:"role"`
	Content string        `json:"content,omitempty"` // 纯文本模式快捷字段
	Parts   []ContentPart `json:"parts,omitempty"`   // 多模态复合片段
}

// ChatRequest 统一大模型对话生成请求
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float32       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// ChatResponse 统一大模型响应结果与 Token 消耗审计
type ChatResponse struct {
	Content          string        `json:"content"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	Model            string        `json:"model"`
	Duration         time.Duration `json:"duration"`
}

// Provider 所有大模型接入必须实现的统一接口
type Provider interface {
	// Name 返回当前提供商唯一标识 (mimo, deepseek, openai, claude, qwen, ollama)
	Name() string

	// Chat 执行单次多模态/文本对话补全
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}

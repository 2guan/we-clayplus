package message

import (
	"context"
	"io"
)

// Ability 消息能力接口（供插件嵌入使用）
type Ability interface {
	// Send 发送消息
	Send(msg *Message) (*Send_Response, error)
	// Forward 转发消息给新接收者
	Forward(msg *Message, receiver string) (*Forward_Response, error)
	// Revoke 撤回当前 gateway 运行内发送且已保存完整回执的消息。
	// v0.5.1 起自动补齐原始 client_id/create_time；缺失时返回 ErrRevokeReceiptUnavailable。
	Revoke(receiver string, newMsgId uint64) (*Revoke_Response, error)
	// Pat 在 receiver 私聊或群聊中拍 pattedUsername；私聊两者填写同一微信 ID。
	Pat(receiver, pattedUsername string) (*Pat_Response, error)
	// Download 下载消息中的媒体资源（server-stream：流式返回）
	Download(msg *Message) (io.ReadCloser, error)
}

// VoiceTranscriptionAbility 是可选扩展，避免新增能力破坏已有 Ability 实现。
type VoiceTranscriptionAbility interface {
	TranscribeVoice(ctx context.Context, msg *Message) (string, error)
}

// Instance 消息能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

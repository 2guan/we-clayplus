package report

// Ability 状态通知能力接口（供插件嵌入使用）
type Ability interface {
	// StartTyping 通知对方正在输入
	StartTyping(receiver string) error
	// StopTyping 通知对方停止输入
	StopTyping(receiver string) error
	// ReadMessage 通知对方消息已读
	ReadMessage(receiver string) error
}

// Instance 状态通知能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

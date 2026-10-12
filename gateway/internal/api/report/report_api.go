// Package reportapi 提供状态通知服务的 API 接口定义。
package reportapi

// ReportService 状态通知服务 API 接口。
type ReportService interface {
	// StartTyping 通知对方正在输入
	StartTyping(receiver string) error
	// StopTyping 通知对方停止输入
	StopTyping(receiver string) error
	// ReadMessage 通知对方消息已读
	ReadMessage(receiver string) error
}

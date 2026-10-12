// Package reportability 提供状态通知能力的实现（直连型）。
package reportability

import (
	sdk "github.com/we-clay/we-clay/sdk/report"

	reportapi "github.com/we-clay/we-clay/gateway/internal/api/report"
)

// ability 状态通知能力实现（直连型）。
type ability struct {
	api reportapi.ReportService
}

func init() {
	sdk.Instance = &ability{api: reportapi.Get()}
}

// StartTyping 通知对方正在输入
func (a ability) StartTyping(receiver string) error {
	return a.api.StartTyping(receiver)
}

// StopTyping 通知对方停止输入
func (a ability) StopTyping(receiver string) error {
	return a.api.StopTyping(receiver)
}

// ReadMessage 通知对方消息已读
func (a ability) ReadMessage(receiver string) error {
	return a.api.ReadMessage(receiver)
}

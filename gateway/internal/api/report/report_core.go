// Package reportapi 提供状态通知服务的远程实现（通过 gRPC 调用 core）。
package reportapi

import (
	"errors"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 状态通知服务 远程实现（通过 gRPC 调用 core）。
type remote struct{}

// Get 获取 ReportService 单例（gRPC 模式）。
var Get = sync.OnceValue(func() ReportService {
	return &remote{}
})

// StartTyping 通知对方正在输入
func (w remote) StartTyping(receiver string) error {
	_, err := api.GetCore().Post("/api/message/start").Query("receiver", receiver).Do()
	return err
}

// StopTyping 通知对方停止输入
func (w remote) StopTyping(receiver string) error {
	_, err := api.GetCore().Post("/api/message/stop").Query("receiver", receiver).Do()
	return err
}

// ReadMessage 通知对方消息已读
func (w remote) ReadMessage(receiver string) error {
	return errors.New("ReadMessage not exposed by the existing core API contract")
}

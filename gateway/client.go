package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	sdkroot "github.com/we-clay/we-clay/sdk"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
)

//go:generate go run ../scripts/generate-client.go -root ..

var (
	// ErrNotImplemented 与 sdk.ErrNotImplemented 相同，占位方法不会发出请求。
	ErrNotImplemented = sdkroot.ErrNotImplemented
	// ErrClientClosed 表示所属 Run 已取消或退出；旧客户端不能用于下一次 Run。
	ErrClientClosed = errors.New("gateway 客户端已停止")
	// ErrAbilityPanic 表示底层能力发生 panic，操作结果可能未知，不应自动重试。
	ErrAbilityPanic = errors.New("底层能力执行异常，请核对操作结果")
)

type clientState struct {
	mu     sync.RWMutex
	ctx    context.Context
	closed bool
}

// close 等待正在执行的能力调用返回。调用前必须取消运行 context，以解除网络等待。
func (s *clientState) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// Context 返回本次 Run 的 context，可用于停止应用自己的后台任务。
func (c *Client) Context() context.Context { return c.state.ctx }

// SendText 主动向微信 ID 或群 ID 发送文字。昵称、备注不是收件人 ID。
func (c *Client) SendText(receiver, content string) (*message.Send_Response, error) {
	return c.Message.Send(&message.Message{
		Type: message.TypeText, Receiver: &contact.Contact{Username: receiver}, Content: content,
	})
}

// Pat 在私聊中拍一拍指定微信 ID。群聊请调用 Message.Pat(roomID, memberID)。
func (c *Client) Pat(username string) (*message.Pat_Response, error) {
	return c.Message.Pat(username, username)
}

func clientCall[T any](s *clientState, name string, fn func() (T, error)) (value T, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.ctx.Err() != nil {
		return value, ErrClientClosed
	}
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
			err = fmt.Errorf("%s: %w", name, ErrAbilityPanic)
		}
	}()
	return fn()
}

func clientDo(s *clientState, name string, fn func() error) error {
	_, err := clientCall(s, name, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

// 原 SDK 少量查询没有 error 返回值；保持签名，停止时返回零值。
func clientRead[T any](s *clientState, name string, fn func() T) T {
	value, err := clientCall(s, name, func() (T, error) { return fn(), nil })
	if errors.Is(err, ErrAbilityPanic) {
		slog.Error("直接调用能力失败", "method", name, "error", err)
	}
	return value
}

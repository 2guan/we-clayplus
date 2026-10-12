package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// GetSubscriptions 返回订阅的事件列表
func (p *ExamplePlugin) GetSubscriptions() []string {
	return []string{
		message.TypeText.Topic, // 文本消息
		"message::image",       // 图片消息
		"session::expired",     // 会话过期事件
	}
}

// OnEvent 事件处理函数
func (p *ExamplePlugin) OnEvent(e *plugin.Event) (bool, error) {
	// 更新统计
	p.stats.TotalMessages++

	// 处理不同类型的事件
	switch e.Topic {
	case "session::expired":
		return p.handleSessionExpired(e)
	case message.TypeText.Topic:
		return p.handleTextMessage(e)
	case "message::image":
		return p.handleImageMessage(e)
	default:
		return false, nil
	}
}

// handleSessionExpired 处理会话过期事件
func (p *ExamplePlugin) handleSessionExpired(e *plugin.Event) (bool, error) {
	slog.Info("[example] 会话过期", "sender", e.Sender)

	// 清理会话数据
	p.mu.Lock()
	delete(p.sessions, e.Sender)
	p.mu.Unlock()

	// 发送提醒消息
	_, err := p.message.Send(&message.Message{
		Receiver: &contact.Contact{Username: e.Sender},
		Content:  "会话已过期，请重新开始对话",
	})

	return true, err
}

// handleTextMessage 处理文本消息
func (p *ExamplePlugin) handleTextMessage(e *plugin.Event) (bool, error) {
	msg, ok := e.Payload.(*plugin.Event_Message)
	if !ok {
		return false, nil
	}

	content := strings.TrimSpace(msg.Message.Content)
	if content == "" {
		return false, nil
	}

	slog.Info("[example] 收到文本消息",
		"sender", e.Sender,
		"content", content)

	// 更新会话数据
	p.updateSession(e.Sender, content)

	// 检查是否启用自动回复
	if !p.Config.ReplyEnabled {
		return false, nil
	}

	// 检查消息长度
	if len(content) > p.Config.MaxLength {
		_, err := p.message.Send(&message.Message{
			Receiver: &contact.Contact{Username: e.Sender},
			Content:  fmt.Sprintf("消息太长了（%d > %d）", len(content), p.Config.MaxLength),
		})
		return true, err
	}

	// 劫持会话 10 秒
	p.session.Hold(p, e.Sender, 10*time.Second)

	// 回显消息
	reply := p.Config.EchoPrefix + content
	_, err := p.message.Send(&message.Message{
		Receiver: &contact.Contact{Username: e.Sender},
		Content:  reply,
	})

	return true, err
}

// handleImageMessage 处理图片消息
func (p *ExamplePlugin) handleImageMessage(e *plugin.Event) (bool, error) {
	slog.Info("[example] 收到图片消息", "sender", e.Sender)

	_, err := p.message.Send(&message.Message{
		Receiver: &contact.Contact{Username: e.Sender},
		Content:  "收到图片啦！",
	})

	return true, err
}

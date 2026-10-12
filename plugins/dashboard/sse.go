// 文件名: plugins/dashboard/sse.go
// 主要功能: 基于 Server-Sent Events (SSE) 的实时长连接管道与运行日志环形缓冲区（实时推流、多客户端分发）
// 作者: 2guan

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// LogEntry 单条系统日志条目
type LogEntry struct {
	ID        int64     `json:"id"`
	Level     string    `json:"level"`     // DEBUG, INFO, WARN, ERROR
	Message   string    `json:"message"`   // 日志正文
	Tag       string    `json:"tag"`       // 模块来源标签
	Timestamp time.Time `json:"timestamp"` // 产生时间
}

// SSEBroker 负责管理 Web 端的 SSE 长连接，并向所有在线控制台推送实时日志与新消息
type SSEBroker struct {
	mu         sync.RWMutex
	clients    map[chan []byte]bool
	ringBuffer []LogEntry
	ringCap    int
	nextID     int64
}

// NewSSEBroker 创建并初始化 SSE 广播管理器，默认环形缓冲区保留最近 3000 条日志
func NewSSEBroker(ringCap int) *SSEBroker {
	if ringCap <= 0 {
		ringCap = 3000
	}
	return &SSEBroker{
		clients:    make(map[chan []byte]bool),
		ringBuffer: make([]LogEntry, 0, ringCap),
		ringCap:    ringCap,
		nextID:     1,
	}
}

// AppendLog 将一条新产生的运行日志存入环形缓冲区，并广播给所有在线客户端
func (b *SSEBroker) AppendLog(level, tag, message string) {
	b.mu.Lock()
	entry := LogEntry{
		ID:        b.nextID,
		Level:     level,
		Tag:       tag,
		Message:   message,
		Timestamp: time.Now(),
	}
	b.nextID++

	// 环形队列防爆内存
	if len(b.ringBuffer) >= b.ringCap {
		b.ringBuffer = b.ringBuffer[1:]
	}
	b.ringBuffer = append(b.ringBuffer, entry)

	// 准备推送的数据
	data, _ := json.Marshal(map[string]any{
		"event": "log",
		"data":  entry,
	})
	b.mu.Unlock()

	b.Broadcast(data)
}

// GetRecentLogs 提取缓冲区中近期存储的日志快照
func (b *SSEBroker) GetRecentLogs(limit int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > len(b.ringBuffer) {
		limit = len(b.ringBuffer)
	}

	start := len(b.ringBuffer) - limit
	res := make([]LogEntry, limit)
	copy(res, b.ringBuffer[start:])
	return res
}

// Broadcast向所有当前在线监听的 SSE 客户端分发数据帧
func (b *SSEBroker) Broadcast(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	msg := []byte(fmt.Sprintf("data: %s\n\n", string(data)))
	for clientChan := range b.clients {
		select {
		case clientChan <- msg:
		default:
			// 慢客户端跳过，防止阻塞整个广播管道
		}
	}
}

// BroadcastEvent 广播指定业务类型的自定义事件（如微信新消息推送、扫码状态变更）
func (b *SSEBroker) BroadcastEvent(eventType string, payload any) {
	data, err := json.Marshal(map[string]any{
		"event": eventType,
		"data":  payload,
	})
	if err == nil {
		b.Broadcast(data)
	}
}

// ServeHTTP 处理前端客户端发起的 SSE 长连接请求 (`/api/logs/stream`)
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 确保客户端支持长连接响应 Flush
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "当前环境不支持流式传输", http.StatusBadRequest)
		return
	}

	// 设置 SSE 标准响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 为该连接创建独立的事件消费通道
	messageChan := make(chan []byte, 64)

	b.mu.Lock()
	b.clients[messageChan] = true
	// 先向新连接客户端推送近 100 条历史日志以快速充填终端视窗
	recent := b.ringBuffer
	if len(recent) > 100 {
		recent = recent[len(recent)-100:]
	}
	b.mu.Unlock()

	// 退出时安全注销客户端通道
	defer func() {
		b.mu.Lock()
		delete(b.clients, messageChan)
		close(messageChan)
		b.mu.Unlock()
	}()

	// 写入历史切片
	for _, entry := range recent {
		raw, _ := json.Marshal(map[string]any{
			"event": "log",
			"data":  entry,
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(raw))
	}
	flusher.Flush()

	// 保持连接与保活心跳
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			// 客户端断开连接
			return
		case msg := <-messageChan:
			_, err := w.Write(msg)
			if err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			// 发送心跳注释行防止代理层或网关静默超时切断
			_, err := fmt.Fprintf(w, ": heartbeat\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// BroadcastLog 广播运行日志（AppendLog 的便捷别名）
func (b *SSEBroker) BroadcastLog(level, tag, message string) {
	b.AppendLog(level, tag, message)
}

// BroadcastMessage 将微信聊天消息广播给在线 SSE 客户端
func (b *SSEBroker) BroadcastMessage(msg *ChatMessage) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return
	}
	frame := []byte(fmt.Sprintf("event: chat_message\ndata: %s\n\n", string(raw)))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for clientChan := range b.clients {
		select {
		case clientChan <- frame:
		default:
		}
	}
}


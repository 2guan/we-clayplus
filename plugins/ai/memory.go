// 文件名: plugins/ai/memory.go
// 主要功能: 对话上下文状态机、滑动窗口记忆管理与会话隔离机制
// 作者: 2guan

package main

import (
	"sync"
	"time"

	"github.com/we-clay/we-clay/plugins/ai/provider"
)

// SessionMemory 单个会话的历史消息状态
type SessionMemory struct {
	SessionID  string
	Messages   []provider.ChatMessage
	LastActive time.Time
}

// MemoryManager 全局会话记忆管理器
type MemoryManager struct {
	mu           sync.RWMutex
	sessions     map[string]*SessionMemory
	defaultRound int
}

// NewMemoryManager 实例化记忆管理器
func NewMemoryManager(defaultRound int) *MemoryManager {
	if defaultRound <= 0 {
		defaultRound = 10
	}
	m := &MemoryManager{
		sessions:     make(map[string]*SessionMemory),
		defaultRound: defaultRound,
	}

	// 启动后台定时巡检，清理 2 小时未活跃的会话记忆
	go m.cleanupLoop(2 * time.Hour)

	return m
}

// Append 添加一条新消息至会话记忆，并按滑动窗口截断
func (m *MemoryManager) Append(sessionID string, msg provider.ChatMessage, maxRounds int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[sessionID]
	if !ok {
		s = &SessionMemory{
			SessionID: sessionID,
		}
		m.sessions[sessionID] = s
	}

	s.Messages = append(s.Messages, msg)
	s.LastActive = time.Now()

	limit := maxRounds
	if limit <= 0 {
		limit = m.defaultRound
	}
	// 每个轮次包含一问一答两条消息
	maxMessages := limit * 2

	if len(s.Messages) > maxMessages {
		// 截取最新的 maxMessages 条
		s.Messages = s.Messages[len(s.Messages)-maxMessages:]
	}
}

// GetMessages 获取当前会话的历史消息切片副本
func (m *MemoryManager) GetMessages(sessionID string) []provider.ChatMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[sessionID]
	if !ok || len(s.Messages) == 0 {
		return nil
	}

	res := make([]provider.ChatMessage, len(s.Messages))
	copy(res, s.Messages)
	return res
}

// Clear 清空指定会话的上下文记忆
func (m *MemoryManager) Clear(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Count 返回当前缓存中的活跃会话数
func (m *MemoryManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *MemoryManager) cleanupLoop(ttl time.Duration) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, s := range m.sessions {
			if now.Sub(s.LastActive) > ttl {
				delete(m.sessions, id)
			}
		}
		m.mu.Unlock()
	}
}

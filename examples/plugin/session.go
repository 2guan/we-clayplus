package main

import "time"

// SessionData 会话数据
type SessionData struct {
	LastMessage string
	Count       int
	StartTime   time.Time
}

// updateSession 更新会话数据
func (p *ExamplePlugin) updateSession(sender, content string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if session, ok := p.sessions[sender]; ok {
		session.LastMessage = content
		session.Count++
	} else {
		p.sessions[sender] = &SessionData{
			LastMessage: content,
			Count:       1,
			StartTime:   time.Now(),
		}
	}
}

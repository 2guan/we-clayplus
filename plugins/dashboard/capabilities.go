// 文件名：capabilities.go
// 主要功能：为内建插件暴露受同一权限开关、并发和用量限制的异步 AI 任务入口。
// 作者：2guan
package dashboard

import (
	"fmt"
	"github.com/we-clay/we-clay/plugins/ai"
	"strings"
)

// Submit 仅提交生成任务；插件调用不能通过此接口附带微信发送副作用。
func (s *Server) Submit(q ai.Request) (string, error) {
	if strings.TrimSpace(q.Prompt) == "" {
		return "", fmt.Errorf("请输入任务内容")
	}
	q.Target = ""
	q.Source = "plugin"
	if q.System != "" {
		q.Prompt += "\n插件提供的任务要求：\n" + q.System
		q.System = ""
	}
	q.Tools = nil
	q.Context = nil
	return s.queue(q)
}

// Task 返回不可变副本；结果中不包含提供商凭据。
func (s *Server) Task(id string) (Task, error) {
	for _, t := range s.store.snapshot().Tasks {
		if t.ID == id && t.Status != "deleted" {
			return t, nil
		}
	}
	return Task{}, fmt.Errorf("任务不存在")
}

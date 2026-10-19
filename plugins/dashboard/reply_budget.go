package dashboard

import (
	"fmt"
	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

func materializeConversation(st State, q ai.Request, m Model, maxOutput int, c contextcleaner.Config, r safetyReport) (ai.Request, safetyReport, error) {
	conv := *q.Conversation
	conv.Memories = append([]ai.MemoryEvidence(nil), conv.Memories...)
	conv.References = append([]contextcleaner.Message(nil), conv.References...)
	if q.Source != "wechat-proactive" {
		conv.Current.Text = q.Prompt
	}
	q.Conversation = &conv
	if m.Context > 0 {
		c.MaxMessages = m.Context
	}
	if m.ContextTokens > 0 {
		c.Budget = m.ContextTokens
	}
	// 当前消息之外的去重和身份边界不依赖可选清洗插件开关。
	history := []contextcleaner.Message{}
	seen := map[string]bool{}
	if q.Source != "wechat-proactive" {
		seen[conv.Current.ID] = true
	}
	for _, v := range conv.References {
		seen[v.ID] = true
	}
	for _, v := range q.Context {
		if seen[v.ID] && v.ID != "" {
			continue
		}
		seen[v.ID] = true
		history = append(history, v)
	}
	// 本轮正文独立保存，单条旧历史超限时可以整条舍弃。
	bounded := history[:0]
	for _, v := range history {
		if c.Budget > 0 && contextcleaner.Estimate(v.Text) > c.Budget {
			r.Changes = append(r.Changes, contextcleaner.Change{ID: v.ID, Reason: "单条旧历史超过模型预算"})
			continue
		}
		bounded = append(bounded, v)
	}
	// 清洗与条数限制继续复用；容量只按实际渲染的短文本计算，不按归档JSON预先裁剪。
	cleanConfig := c
	cleanConfig.Budget = int(^uint(0) >> 1)
	clean, err := contextcleaner.Prepare(cleanConfig, bounded, q.Target)
	r.Changes = append(r.Changes, clean.Changes...)
	if err != nil {
		return q, r, err
	}
	q.Context = clean.Messages
	reserve := contextcleaner.Estimate(q.System) + maxOutput + 512
	for _, tool := range q.Tools {
		reserve += contextcleaner.Estimate(tool.Name+tool.Description) + 256
	}
	budget := settingNumber(st, "context_cleaner.max_input") - reserve
	if m.ContextTokens > 0 {
		budget = min(budget, m.ContextTokens)
	} else {
		budget = min(budget, c.Budget)
	}
	if budget <= 0 {
		return q, r, fmt.Errorf("当前请求没有可用的模型上下文预算")
	}
	for ai.ConversationSize(q) > budget {
		switch {
		case len(conv.Memories) > 0:
			last := conv.Memories[len(conv.Memories)-1]
			conv.Memories = conv.Memories[:len(conv.Memories)-1]
			r.Changes = append(r.Changes, contextcleaner.Change{ID: last.ID, Reason: "超出模型预算，未采用记忆"})
		case len(q.Context) > 0:
			r.Changes = append(r.Changes, contextcleaner.Change{ID: q.Context[0].ID, Reason: "超出模型预算，裁剪较早对话"})
			q.Context = q.Context[1:]
		case len(conv.References) > 1:
			last := conv.References[len(conv.References)-1]
			conv.References = conv.References[:len(conv.References)-1]
			r.Changes = append(r.Changes, contextcleaner.Change{ID: last.ID, Reason: "超出模型预算，裁剪补充证据"})
		default:
			return q, r, fmt.Errorf("本轮消息及必要引用超过模型上下文预算，请缩短消息或提高模型预算")
		}
	}
	r.Messages = append(append([]contextcleaner.Message{}, q.Context...), conv.References...)
	r.Tokens = ai.ConversationSize(q) + contextcleaner.Estimate(q.System)
	return q, r, nil
}

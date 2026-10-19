// 主要功能：后台手动触发一次基于当前会话上下文的主动文字发言。
package dashboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/we-clay/we-clay/plugins/ai"
)

const proactivePrompt = "请结合本会话近期对话，以本人口吻主动接一句自然、简短的话。不要重复刚说过的内容，不编造对方的新发言，只输出一句文字正文。"

func wechatReplySource(source string) bool { return source == "wechat" || source == "wechat-proactive" }

func (s *Server) proactiveAllowed(target string) bool {
	st := s.store.snapshot()
	return !s.cfg.DisableAI && st.Account.Status == "online" && pluginEnabled(st, "ai") && replyTargetAllowed(st, target) && behaviorAllowed(st, "message.Send", target) == nil
}

func (s *Server) requestReplyAllowed(q ai.Request) bool {
	if q.Source == "wechat-proactive" {
		return s.proactiveAllowed(q.Target)
	}
	return s.replyAllowed(q.Target)
}

func proactiveRequest(st State, target string) (ai.Request, error) {
	for i := len(st.Messages) - 1; i >= 0; i-- {
		m := st.Messages[i]
		if m.Target != target || !contextAvailable(m) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		q := replyRequest(st, m)
		q.Source, q.Prompt, q.InputID, q.InputKind = "wechat-proactive", proactivePrompt, "", ""
		return q, nil
	}
	return ai.Request{}, fmt.Errorf("当前会话没有可用上下文，暂时无法主动接话")
}

func (s *Server) proactiveReply(target string) error {
	st := s.store.snapshot()
	if !s.proactiveAllowed(target) {
		return fmt.Errorf("请确认微信在线、AI已启用且该会话允许AI发送消息")
	}
	q, err := proactiveRequest(st, target)
	if err != nil {
		return err
	}
	_, err = s.queue(q)
	return err
}

// 主动群发言没有“当前说话人”，只允许本群共享记忆；私聊允许该联系人的记忆。
func proactiveMemorySpeaker(st State, m Message) Message {
	m.SenderID = ""
	if !memoryGroup(st, m.Target) {
		m.SenderID = m.Target
	}
	return m
}

func (s *Server) deliverProactive(ctx context.Context, q ai.Request, text string) error {
	parts, err := parseReply(text, 5)
	if err != nil {
		return err
	}
	var lines []string
	for _, p := range parts {
		if p.Kind != "text" {
			return fmt.Errorf("主动说一句只支持文字，本次未发送")
		}
		lines = append(lines, p.Text)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.validateReplyEvidence(q); err != nil {
		return err
	}
	if !s.proactiveAllowed(q.Target) {
		return fmt.Errorf("当前会话已不允许AI主动发言")
	}
	// 只调用一次文字发送；不因分段、全局语音偏好或动作标签生成额外操作。
	return s.send(ctx, actionRequest{Target: q.Target, Content: strings.Join(lines, " ")}, "ai")
}

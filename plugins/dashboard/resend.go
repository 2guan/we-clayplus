// 文件名：resend.go
// 主要功能：从可信归档重建失败消息，复用人工发送链路并保护原记录。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"fmt"
)

func (s *Server) resend(ctx context.Context, q actionRequest) error {
	if !s.resendMu.TryLock() {
		return fmt.Errorf("正在重发，请勿重复操作")
	}
	defer s.resendMu.Unlock()
	var original *Message
	for _, m := range s.store.snapshot().Messages {
		if m.ID == q.ID && m.Target == q.Target {
			copy := m
			original = &copy
			break
		}
	}
	if original == nil || !original.Self || (original.Status != "unknown" && original.Status != "failed") {
		return fmt.Errorf("仅可重发本账号发送失败或结果未知的消息")
	}
	if original.Status == "unknown" && !q.Enabled {
		return fmt.Errorf("结果未知可能已发送，请确认重复发送风险")
	}
	m := original
	next := actionRequest{Mentions: m.Mentions, Target: m.Target, Content: m.Content, Group: m.Kind, RetryID: m.ID}
	// 不从浏览器接收正文、附件或引用内容，防止重发时替换原消息。
	switch m.Kind {
	case "text":
	case "quote":
		if m.Rich == nil || m.Rich.QuoteID == "" {
			return fmt.Errorf("引用信息缺失，无法重发")
		}
		next.Group = "text"
		next.QuoteID = m.Rich.QuoteID
	case "emoji":
		if m.Rich == nil || m.Rich.StickerID == "" {
			return fmt.Errorf("表情原始信息缺失，无法重发")
		}
		next.StickerID = m.Rich.StickerID
	case "image", "audio", "video", "file":
		if m.Media == "" {
			return fmt.Errorf("原附件未保存，无法重发")
		}
		data, mime, err := s.replyMediaBytes(m.Media)
		if err != nil {
			return fmt.Errorf("原附件无法读取：%w", err)
		}
		next.Media = base64.StdEncoding.EncodeToString(data)
		next.MIME = mime
	default:
		return fmt.Errorf("此消息类型暂不支持重发")
	}
	return s.send(ctx, next, "manual")
}

// 群聊先等待补充再生成一轮回复；原始消息逐条归档，工具授权仍绑定末条原始消息。
package dashboard

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

const groupReplyQuiet = 7 * time.Second
const groupReplyMaximum = 15 * time.Second

type groupReplyBatch struct {
	account     string
	epoch       uint64
	target      string
	ids         []string
	first, last time.Time
	wake        chan struct{}
}

func (b *groupReplyBatch) deadline() time.Time {
	deadline := b.last.Add(groupReplyQuiet)
	if maximum := b.first.Add(groupReplyMaximum); maximum.Before(deadline) {
		deadline = maximum
	}
	return deadline
}
func (s *Server) hasGroupReplyBatch(st State, target string) bool {
	s.replyBatchMu.Lock()
	defer s.replyBatchMu.Unlock()
	b := s.replyBatches[target]
	return b != nil && b.account == st.Account.ID && b.epoch == st.AccountEpoch
}

func (s *Server) accumulateGroupReply(st State, m Message, now time.Time) {
	s.replyBatchMu.Lock()
	if s.replyBatches == nil {
		s.replyBatches = map[string]*groupReplyBatch{}
	}
	b := s.replyBatches[m.Target]
	if b != nil && b.account == st.Account.ID && b.epoch == st.AccountEpoch {
		if !slices.Contains(b.ids, m.ID) {
			b.ids = append(b.ids, m.ID)
			b.last = now
		}
		select {
		case b.wake <- struct{}{}:
		default:
		}
		s.replyBatchMu.Unlock()
		reason := "已并入同群待回复消息，不单独生成回复；本批统一交给模型判断"
		if trigger := replyDirectedAtSelf(st, contextMessage(st, m)); trigger != "" {
			reason = trigger + "；" + reason
		}
		s.replyDecision(m, "合并等待", reason)
		return
	}
	if len(s.replyBatches) >= 256 {
		s.replyBatchMu.Unlock()
		s.replyQueueError(m, fmt.Errorf("待合并群聊已达到 256 个上限"))
		return
	}
	b = &groupReplyBatch{account: st.Account.ID, epoch: st.AccountEpoch, target: m.Target, ids: []string{m.ID}, first: now, last: now, wake: make(chan struct{}, 1)}
	s.replyBatches[m.Target] = b
	s.workers.Add(1)
	s.replyBatchMu.Unlock()
	go func() {
		defer s.workers.Done()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			s.replyBatchMu.Lock()
			active := s.replyBatches[b.target] == b
			deadline := b.deadline()
			s.replyBatchMu.Unlock()
			if !active {
				return
			}
			if !time.Now().Before(deadline) && s.flushGroupReply(b, time.Now()) {
				return
			}
			timer.Reset(time.Until(deadline))
			select {
			case <-s.ctx.Done():
				s.cancelGroupReplyBatch(b, "应用停止，尚未提交的合并消息不补发")
				return
			case <-b.wake:
				continue
			case <-timer.C:
				if s.flushGroupReply(b, time.Now()) {
					return
				}
			}
		}
	}()
	trigger := replyDirectedAtSelf(st, contextMessage(st, m))
	if trigger == "" {
		trigger = "普通群消息，命中回复概率"
	}
	s.replyDecision(m, "等待合并", trigger+"；停顿 7 秒后处理，首条消息最多等待 15 秒")
}

// 返回 false 表示期间收到新消息，尚未到最新截止时间。
func (s *Server) flushGroupReply(b *groupReplyBatch, now time.Time) bool {
	s.replyBatchMu.Lock()
	if s.replyBatches[b.target] != b {
		s.replyBatchMu.Unlock()
		return true
	}
	if now.Before(b.deadline()) {
		s.replyBatchMu.Unlock()
		return false
	}
	delete(s.replyBatches, b.target)
	ids := slices.Clone(b.ids)
	s.replyBatchMu.Unlock()
	q := ai.Request{AccountID: b.account, AccountEpoch: b.epoch, Target: b.target, BatchIDs: ids}
	if s.ctx.Err() != nil {
		s.recordReplyDecision(q, "", "INFO", "取消", "应用停止，尚未提交的合并消息不补发")
		return true
	}
	st := s.store.snapshot()
	if st.Account.ID != b.account || st.AccountEpoch != b.epoch {
		return true
	}
	if reason := s.replyBlockedReason(st, b.target); reason != "" {
		s.recordReplyDecision(q, "", "INFO", "取消", "等待合并期间停止："+reason)
		return true
	}
	// 等待期间的撤回、删除和新名单规则都生效，不把无效消息提交模型。
	valid := []string{}
	var latest Message
	for _, id := range ids {
		reason := "归档消息已删除或不存在"
		for _, m := range st.Messages {
			if m.ID != id || m.Target != b.target {
				continue
			}
			switch {
			case m.Self:
				reason = "消息标记为本人发送，不触发自动回复"
			case !contextAvailable(m):
				reason = "消息已撤回、删除或状态不可用，当前状态：" + m.Status
			case skipOtherGroupMention(st, m):
				reason = "消息仅 @ 其他成员，默认不插话"
			default:
				valid = append(valid, id)
				latest = m
				reason = ""
			}
			break
		}
		if reason != "" {
			q.BatchIDs = []string{id}
			s.recordReplyDecision(q, "", "INFO", "跳过", reason)
		}
	}
	if len(valid) == 0 {
		return true
	}
	s.queueAutoReply(batchReplyRequest(st, latest, valid), latest)
	return true
}

func batchReplyRequest(st State, m Message, ids []string) ai.Request {
	q := replyRequest(st, m)
	q.BatchIDs = slices.Clone(ids)
	// 来源在刷新时完整复验；这里先填充，保证首次输入防护覆盖整批消息。
	q.Conversation.Pending, _ = pendingBatchMessages(st, q, m.ID)
	trimBatchContext(st, &q)
	return q
}

func pendingBatchMessages(st State, q ai.Request, currentID string) ([]contextcleaner.Message, error) {
	pending := []contextcleaner.Message{}
	seen := map[string]bool{currentID: true}
	for _, id := range q.BatchIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		found := false
		for _, m := range st.Messages {
			if m.ID != id || m.Target != q.Target {
				continue
			}
			if m.Self || !contextAvailable(m) || skipOtherGroupMention(st, m) {
				return nil, fmt.Errorf("合并消息已撤回或不再允许回复")
			}
			v := contextMessage(st, m)
			// 识别结果仅保留在本次请求，原始归档和证据哈希不变。
			if q.Conversation != nil && (m.Kind == "image" || m.Kind == "audio") {
				for _, old := range q.Conversation.Pending {
					if old.ID == id {
						v.Text = old.Text
						break
					}
				}
			}
			pending = append(pending, v)
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("合并消息已删除，停止本轮回复")
		}
	}
	return pending, nil
}

func (s *Server) prepareBatchIncoming(ctx context.Context, q ai.Request) (ai.Request, error) {
	if q.Conversation != nil {
		conv := *q.Conversation
		conv.Pending = slices.Clone(conv.Pending)
		q.Conversation = &conv
		for i, m := range conv.Pending {
			if m.Kind != "image" && m.Kind != "audio" {
				continue
			}
			media := q
			media.InputID, media.InputKind, media.Prompt = m.ID, m.Kind, m.Text
			var err error
			media, err = s.prepareIncoming(ctx, media)
			if err != nil {
				return q, err
			}
			conv.Pending[i].Text = media.Prompt
		}
	}
	return s.prepareIncoming(ctx, q)
}

// 合并消息只出现一次；本批之后的新消息留给下一批，不混入旧请求的历史。
func trimBatchContext(st State, q *ai.Request) {
	if len(q.BatchIDs) == 0 || q.Conversation == nil {
		return
	}
	before := map[string]bool{}
	q.Conversation.RepliedAfterCurrent = false
	afterAnchor := false
	for _, m := range st.Messages {
		if m.Target != q.Target {
			continue
		}
		if m.ID == q.Conversation.Current.ID {
			afterAnchor = true
			continue
		}
		if afterAnchor && m.Self && m.Source == "ai" {
			body, err := ai.ReplyText(m.Content)
			_, silent := contextcleaner.Output(contextcleaner.Config{Enabled: true, Silence: true}, body, true)
			if err != nil || silent || contextcleaner.ReplyDecision(body) {
				continue // 误发的控制内容不算已回答，也不作为后续 assistant 示例。
			}
		}
		// 来信之后实际发出的本人回复必须可见，后续尚未处理的群友消息仍留给下一批。
		if (!afterAnchor || m.Self && contextAvailable(m)) && !slices.Contains(q.BatchIDs, m.ID) {
			before[m.ID] = true
			if afterAnchor && m.Self {
				q.Conversation.RepliedAfterCurrent = true
			}
		}
	}
	history := []contextcleaner.Message{}
	for _, m := range q.Context {
		if before[m.ID] {
			history = append(history, m)
		}
	}
	q.Context = history
	refs := []contextcleaner.Message{}
	for _, m := range q.Conversation.References {
		if !slices.Contains(q.BatchIDs, m.ID) {
			refs = append(refs, m)
		}
	}
	// 为合并中的引用消息补齐同会话原文，不能只给一个没有正文的引用编号。
	for _, m := range q.Conversation.Pending {
		if m.Quote == nil || !m.Quote.Verified || slices.Contains(q.BatchIDs, m.Quote.ID) || slices.ContainsFunc(refs, func(v contextcleaner.Message) bool { return v.ID == m.Quote.ID }) {
			continue
		}
		for _, original := range st.Messages {
			if original.Target == q.Target && original.ID == m.Quote.ID && contextAvailable(original) {
				refs = append(refs, contextMessage(st, original))
				break
			}
		}
	}
	q.Conversation.References = refs
}

// 清除同一批次只记录一次，唤醒等待协程；切换账号前在原账号下留痕。
func (s *Server) cancelGroupReplyBatch(b *groupReplyBatch, reason string) {
	s.replyBatchMu.Lock()
	if s.replyBatches[b.target] != b {
		s.replyBatchMu.Unlock()
		return
	}
	delete(s.replyBatches, b.target)
	ids := slices.Clone(b.ids)
	select {
	case b.wake <- struct{}{}:
	default:
	}
	s.replyBatchMu.Unlock()
	s.recordReplyDecision(ai.Request{AccountID: b.account, AccountEpoch: b.epoch, Target: b.target, BatchIDs: ids}, "", "INFO", "取消", reason)
}

// 生成只是草稿；吸收生成期间的来信后才允许发送，不重放微信副作用。
package dashboard

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

const maxReplyRefreshes = 2

// 作废草稿也消耗真实模型用量，不能只统计最后发出的那次。
func (s *Server) recordDiscardedReplyUsage(q ai.Request, result ai.Result) {
	_ = s.store.change(func(st *State) error {
		if st.Account.ID == q.AccountID && s.store.accountEpoch == q.AccountEpoch {
			addModelUsage(st, result)
		}
		return nil
	})
}

type replyOutdated struct{}

func (replyOutdated) Error() string {
	return "生成期间收到新一批来信，旧草稿尚未发送"
}

func activeReply(st State, target string) bool {
	return slices.ContainsFunc(st.Tasks, func(t Task) bool {
		return t.Target == target && t.Source == "wechat" && t.Kind == "chat" && (t.Status == "queued" || t.Status == "running")
	})
}

func (s *Server) replyReplacedByOperator(q ai.Request) bool {
	if q.Source != "wechat" || q.Conversation == nil {
		return false
	}
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
		return false
	}
	return slices.ContainsFunc(st.Messages, func(m Message) bool {
		return m.Target == q.Target && m.Self && (m.Source == "manual" || m.Source == "wechat") && contextAvailable(m) && q.EvidenceHashes[m.ID] == ""
	})
}

func (s *Server) replyHasPending(id string, q ai.Request) bool {
	if q.Source != "wechat" || q.Kind != "chat" || q.Conversation == nil {
		return false
	}
	known := func(source string) bool {
		return source == "" || source == q.Conversation.Current.ID || slices.Contains(q.BatchIDs, source)
	}
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
		return false // 账号边界由证据校验报错，不把旧任务并到新账号。
	}
	for _, t := range st.Tasks {
		if t.ID != id && t.Target == q.Target && t.Source == "wechat" && t.Kind == "chat" && t.Status == "queued" {
			if !known(t.ReplyTo) || slices.ContainsFunc(t.ReplyBatch, func(source string) bool { return !known(source) }) {
				return true
			}
		}
	}
	s.replyBatchMu.Lock()
	defer s.replyBatchMu.Unlock()
	b := s.replyBatches[q.Target]
	return b != nil && b.account == q.AccountID && b.epoch == q.AccountEpoch && slices.ContainsFunc(b.ids, func(source string) bool { return !known(source) })
}

func (s *Server) refreshPendingReply(ctx context.Context, id string, q ai.Request) (ai.Request, bool, error) {
	if !s.replyHasPending(id, q) {
		return q, false, nil
	}
	if reason := s.replyBlockedReason(s.store.snapshot(), q.Target); reason != "" {
		return q, false, fmt.Errorf("会话已暂停自动回复：%s", reason)
	}
	// 沿用这一批的 7 秒静默、15 秒最大等待；不逐条打断模型，也不抢走补完的半句话。
	for {
		s.replyBatchMu.Lock()
		b := s.replyBatches[q.Target]
		wait := time.Duration(0)
		var submitted <-chan struct{}
		if b != nil && b.account == q.AccountID && b.epoch == q.AccountEpoch {
			if b.submitting {
				submitted = b.submitted
			} else {
				wait = time.Until(b.deadline())
			}
		}
		s.replyBatchMu.Unlock()
		if submitted != nil {
			select {
			case <-ctx.Done():
				return q, false, ctx.Err()
			case <-submitted:
				continue
			}
		}
		if wait <= 0 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return q, false, ctx.Err()
		case <-timer.C:
		}
	}
	next, err := s.coalesceReplyInputs(id, q, true)
	if err != nil {
		return q, false, err
	}
	changed := next.Conversation.Current.ID != q.Conversation.Current.ID || !slices.Equal(next.BatchIDs, q.BatchIDs)
	return next, changed, nil
}

// 高频会话每个任务最多刷新两次；持续变化时回到合并等待，旧草稿不外发。
func (s *Server) deferBusyReply(id string, q ai.Request) {
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
		s.updateTask(id, func(t *Task) { t.Status, t.Error = "cancelled", "账号已切换，停止旧会话回复" })
		return
	}
	for _, source := range q.BatchIDs {
		for _, m := range st.Messages {
			if m.ID == source && m.Target == q.Target && !m.Self && contextAvailable(m) && !skipOtherGroupMention(st, m) {
				s.accumulateGroupReply(st, m, time.Now())
				break
			}
		}
	}
	s.updateTask(id, func(t *Task) {
		t.Status, t.Error = "cancelled", "会话持续收到补充，未发送草稿；全部来信已移交下一批合并"
		t.ReplyRetryable = false
	})
	s.replyTaskDecision(q, id, "INFO", "延后", fmt.Sprintf("本任务已刷新 %d 次，继续等待合并；没有发送旧回复", maxReplyRefreshes))
}

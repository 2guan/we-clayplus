// 主要功能：将自动回复持久化排队，按会话顺序占用全局生成槽位，不丢弃忙时来信。
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

// 普通回复沿用会话冷却：正在排队或执行的回复也占用本轮机会。
// beforeID 仅在出队时使用，此时只检查自己之前的任务。
func replyCooldownReason(st State, target, beforeID string, cooldown time.Duration) string {
	for _, t := range st.Tasks {
		if beforeID != "" && t.ID == beforeID {
			break
		}
		if t.Target != target || !wechatReplySource(t.Source) || t.Kind != "chat" {
			continue
		}
		if t.Status == "queued" || t.Status == "running" {
			return "当前会话仍有回复正在处理"
		}
		stamp, err := time.Parse(time.RFC3339, t.Created)
		if err == nil && time.Since(stamp) < cooldown {
			return "普通消息仍在回复冷却时间内"
		}
	}
	return ""
}

func queuedReplyGuaranteed(st State, q ai.Request) bool {
	if q.Conversation == nil || st.Account.ID == "" {
		return false
	}
	for _, c := range st.Contacts {
		if c.ID == q.Target && c.Kind == "group" {
			if replyDirectedAtSelf(st, q.Conversation.Current) != "" || groupReplyProbability(st, q.Target) == 100 {
				return true
			}
			for _, m := range q.Conversation.Pending {
				if replyDirectedAtSelf(st, m) != "" {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (s *Server) waitReplyTurn(ctx context.Context, id string, q ai.Request) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.taskMu.Lock()
		st := s.store.snapshot()
		if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
			s.taskMu.Unlock()
			return fmt.Errorf("账号已切换，停止排队回复")
		}
		blocked := len(s.taskCancel)-len(s.taskWaiting) >= int(s.number("routing.concurrency", 2))
		for _, t := range st.Tasks {
			if t.ID == id {
				break
			}
			if t.Target == q.Target && t.Source == "wechat" && t.Kind == "chat" && (t.Status == "queued" || t.Status == "running") {
				blocked = true
				break
			}
		}
		if !blocked {
			cooldownReason := ""
			if !queuedReplyGuaranteed(st, q) {
				cooldownReason = replyCooldownReason(st, q.Target, id, time.Duration(settingNumber(st, "reply.cooldown"))*time.Second)
			}
			delete(s.taskWaiting, id)
			s.taskMu.Unlock()
			if reason := s.replyBlockedReason(s.store.snapshot(), q.Target); reason != "" {
				return fmt.Errorf("会话已暂停自动回复，排队任务已取消：%s", reason)
			}
			if cooldownReason != "" {
				return fmt.Errorf("%s", cooldownReason)
			}
			return nil
		}
		wake := s.taskWake
		s.taskMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

func (s *Server) replyDecision(m Message, decision, reason string) {
	s.store.mu.RLock()
	account, epoch := s.store.state.Account.ID, s.store.accountEpoch
	st := s.store.state
	enabled := !s.cfg.DisableAI && pluginEnabled(st, "ai") && settingFlag(st, "reply.enabled") && replyTargetAllowed(st, m.Target) && slices.ContainsFunc(st.Contacts, func(c Contact) bool {
		return c.ID == m.Target && c.Mode == "auto" && (c.Kind == "group" || st.Settings["reply.private"] != "关闭")
	})
	s.store.mu.RUnlock()
	// 尚未启用智能回复的会话不产生常规判定日志；已启动任务的取消、失败仍单独留痕。
	if !enabled {
		return
	}
	q := ai.Request{AccountID: account, AccountEpoch: epoch, Target: m.Target, BatchIDs: []string{m.ID}}
	s.recordReplyDecision(q, "", "INFO", decision, reason)
}

// 仅记录关联标识与可说明的结果，不记录消息正文、模型推理或凭据。
func (s *Server) recordReplyDecision(q ai.Request, task, level, decision, reason string) {
	ids := slices.Clone(q.BatchIDs)
	if len(ids) == 0 && q.Conversation != nil {
		ids = []string{q.Conversation.Current.ID}
	}
	reason = strings.Join(strings.Fields(reason), " ")
	metadata, _ := json.Marshal(map[string]any{"message_ids": ids, "task": task})
	message := "自动回复" + decision + "；原因：" + reason + "\n" + string(metadata)
	written, err := s.store.appendLog(q.AccountID, q.AccountEpoch, level, "ai", q.Target, message)
	if err != nil {
		slog.Warn("自动回复判定日志写入失败")
		return
	}
	if written {
		severity := slog.LevelInfo
		if level == "ERROR" {
			severity = slog.LevelError
		} else if level == "WARN" {
			severity = slog.LevelWarn
		}
		slog.Log(context.Background(), severity, "自动回复"+decision, "reason", reason, "target", q.Target, "message_ids", ids, "task", task)
		s.notify()
	}
}

func (s *Server) replyTaskDecision(q ai.Request, id, level, decision, reason string) {
	if wechatReplySource(q.Source) && q.Kind == "chat" {
		s.recordReplyDecision(q, id, level, decision, reason)
	}
}

// 隐藏升级前产生的“未开启回复”例行日志，保留真实任务取消、沉默和失败记录。
var quietReplyLogPrefixes = []string{
	"自动回复跳过；原因：当前会话处于人工或暂停模式",
	"自动回复跳过；原因：AI 已在启动配置中停用",
	"自动回复跳过；原因：AI 插件已关闭",
	"自动回复跳过；原因：自动回复总开关已关闭",
	"自动回复跳过；原因：私聊自动回复触发已关闭",
	"自动回复跳过；原因：会话尚未进入通讯录，无法确认自动回复模式",
	"自动回复跳过；原因：未通过会话名单规则",
}

func quietReplyLog(r Record) bool {
	return r.Component == "ai" && slices.ContainsFunc(quietReplyLogPrefixes, func(prefix string) bool { return strings.HasPrefix(r.Message, prefix) })
}

// 出队时合并尚未开始生成的同群批次；不重跑正在生成或已经执行操作的任务。
func (s *Server) coalesceQueuedReplies(id string, q ai.Request) (ai.Request, error) {
	if q.Source != "wechat" || q.Conversation == nil || q.Conversation.Scene != "group" {
		return q, nil
	}
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	st := s.store.snapshot()
	if st.Account.ID != q.AccountID || st.AccountEpoch != q.AccountEpoch {
		return q, fmt.Errorf("账号已切换，停止合并排队回复")
	}
	ids := slices.Clone(q.BatchIDs)
	if len(ids) == 0 {
		ids = []string{q.Conversation.Current.ID}
	}
	children := []string{}
	for _, t := range st.Tasks {
		if t.ID == id || t.Target != q.Target || t.Source != "wechat" || t.Kind != "chat" || t.Status != "queued" || !s.taskWaiting[t.ID] {
			continue
		}
		children = append(children, t.ID)
		batch := t.ReplyBatch
		if len(batch) == 0 {
			batch = []string{t.ReplyTo}
		}
		for _, source := range batch {
			if source != "" && !slices.Contains(ids, source) {
				ids = append(ids, source)
			}
		}
	}
	if len(children) == 0 {
		return q, nil
	}
	valid := []string{}
	var latest Message
	for _, m := range st.Messages {
		if m.Target == q.Target && slices.Contains(ids, m.ID) && !m.Self && contextAvailable(m) && !skipOtherGroupMention(st, m) {
			valid = append(valid, m.ID)
			latest = m
		}
	}
	if len(valid) == 0 {
		return q, fmt.Errorf("待合并消息已撤回或不可用")
	}
	next := batchReplyRequest(st, latest, valid)
	err := s.store.change(func(v *State) error {
		if v.Account.ID != q.AccountID || s.store.accountEpoch != q.AccountEpoch {
			return fmt.Errorf("账号已切换，停止合并排队回复")
		}
		index := slices.IndexFunc(v.Tasks, func(t Task) bool { return t.ID == id && t.Status == "queued" })
		if index < 0 {
			return fmt.Errorf("当前排队任务已取消")
		}
		for _, child := range children {
			if !slices.ContainsFunc(v.Tasks, func(t Task) bool { return t.ID == child && t.Status == "queued" }) {
				return fmt.Errorf("排队状态已变化，请停止旧批次")
			}
		}
		v.Tasks[index].ReplyTo = latest.ID
		v.Tasks[index].ReplyBatch = slices.Clone(valid)
		v.Tasks[index].Input = latest.Content
		v.Tasks[index].Name = fmt.Sprintf("群聊合并回复（%d 条）", len(valid))
		for i := range v.Tasks {
			if slices.Contains(children, v.Tasks[i].ID) {
				v.Tasks[i].Status = "cancelled"
				v.Tasks[i].ReplyMergedInto = id
				v.Tasks[i].Error = "已合并至任务 " + id + "，不单独生成回复"
			}
		}
		return nil
	})
	if err != nil {
		return q, err
	}
	for _, child := range children {
		if cancel := s.taskCancel[child]; cancel != nil {
			cancel()
		}
	}
	s.replyTaskDecision(next, id, "INFO", "合并排队", "同群尚未生成的批次统一处理，避免依次追答旧消息")
	return next, nil
}

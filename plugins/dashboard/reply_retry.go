// 主要功能：从失败任务重新构造回复，沿用归档来源、模型配置与现有权限检查。
package dashboard

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

type replyModelFailure struct{ error }

func replyRequest(st State, m Message) ai.Request {
	conversation, history := buildReplyContext(st, m, time.Now())
	system, _ := st.Overrides[m.Target]["prompt"].(string)
	model, _ := st.Overrides[m.Target]["model"].(string)
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Conversation: &conversation, Kind: "chat", Source: "wechat", Model: model, Prompt: m.Content, System: system, Target: m.Target, Context: history}
	if m.Kind == "image" || m.Kind == "audio" {
		q.InputKind, q.InputID = m.Kind, m.ID
	}
	return q
}

func (s *Server) retryReply(id, target string) error {
	st := s.store.snapshot()
	index := slices.IndexFunc(st.Tasks, func(t Task) bool {
		return t.ID == id && t.Target == target && wechatReplySource(t.Source) && t.Kind == "chat"
	})
	if index < 0 || st.Tasks[index].Status != "failed" || !st.Tasks[index].ReplyRetryable {
		return fmt.Errorf("该任务不是可重试的模型失败")
	}
	t := st.Tasks[index]
	if t.Source == "wechat-proactive" {
		if !s.proactiveAllowed(target) {
			return fmt.Errorf("当前会话不允许AI主动发言")
		}
		q, err := proactiveRequest(st, target)
		if err != nil {
			return err
		}
		_, err = s.queueTaskRetry(q, nil, id)
		return err
	}
	if !s.replyAllowed(target) {
		return fmt.Errorf("请先恢复该会话的自动回复与微信连接")
	}
	for _, m := range st.Messages {
		if m.Target != target || m.ID != t.ReplyTo {
			continue
		}
		if m.Self || !contextAvailable(m) || !slices.Contains([]string{"text", "quote", "image", "audio"}, m.Kind) {
			return fmt.Errorf("原消息已撤回或不可用于回复")
		}
		_, err := s.queueTaskRetry(batchReplyRequest(st, m, t.ReplyBatch), nil, id)
		return err
	}
	return fmt.Errorf("原消息已删除，不能重新生成回复")
}

// 兼容升级前已诊断的“模型读取失败”任务；只从加密来源追踪恢复消息关联，不猜测文本。
func (s *Server) restoreReplyRetries() error {
	st := s.store.snapshot()
	restored := map[string]string{}
	for _, t := range st.Tasks {
		if t.Source != "wechat" || t.Kind != "chat" || t.Status != "failed" || t.ReplyTo != "" || t.Error != "模型响应读取失败或超过 32 MB" {
			continue
		}
		raw, err := s.store.getSecret("reply-trace:" + st.Account.ID + ":" + t.ID)
		var trace replyTrace
		if err == nil && json.Unmarshal([]byte(raw), &trace) == nil && trace.CurrentID != "" {
			restored[t.ID] = trace.CurrentID
		}
	}
	if len(restored) == 0 {
		return nil
	}
	return s.store.change(func(v *State) error {
		for i := range v.Tasks {
			if id := restored[v.Tasks[i].ID]; id != "" {
				v.Tasks[i].ReplyTo, v.Tasks[i].ReplyRetryable = id, true
			}
		}
		return nil
	})
}

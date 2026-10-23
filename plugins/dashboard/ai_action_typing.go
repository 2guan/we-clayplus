package dashboard

import (
	"context"
	"slices"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

// 输入状态仅跟随当前私聊回复。与模型推理并行，单个状态请求最多一秒，不阻塞正文生成。
func (s *Server) startActionTyping(ctx context.Context, q ai.Request) func() {
	st := s.store.snapshot()
	noop := func() {}
	if s.cfg.Mock || s.host == nil || q.Source != "wechat" || q.Kind != "chat" || isActionGroup(q.Target) {
		return noop
	}
	for _, op := range []string{"StartTyping", "StopTyping"} {
		if !slices.Contains(s.cfg.Capabilities["report"].Enabled, op) || behaviorAllowed(st, "report."+op, q.Target) != nil {
			return noop
		}
	}
	finish := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		callCtx, cancel := context.WithTimeout(ctx, time.Second)
		_, _ = s.host.Execute(callCtx, control.Command{Operation: "ai-typing", Target: q.Target, Content: "start"})
		cancel()
		select {
		case <-ctx.Done():
		case <-finish:
		}
		current := s.store.snapshot()
		if current.Account.ID != st.Account.ID || current.AccountEpoch != st.AccountEpoch {
			return
		}
		callCtx, cancel = context.WithTimeout(s.ctx, time.Second)
		defer cancel()
		_, _ = s.host.Execute(callCtx, control.Command{Operation: "ai-typing", Target: q.Target, Content: "stop"})
	}()
	return func() {
		close(finish)
		select {
		case <-done:
		case <-time.After(2200 * time.Millisecond):
		}
	}
}

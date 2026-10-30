package dashboard

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

type momentReplyDecisions struct {
	After int64           `json:"after"`
	Seen  map[string]bool `json:"seen"`
}

// 只消费功能启用后到达的回复；历史通知、普通评论和点赞均不触发自动回应。
func (s *Server) momentReplyWorker() {
	defer s.workers.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.checkMomentReplies(s.ctx); err != nil && s.ctx.Err() == nil {
			account, epoch := s.momentNotificationAccount()
			_, _ = s.store.appendLog(account, epoch, "WARN", "moments", "", "朋友圈回复检查失败："+err.Error())
		}
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) checkMomentReplies(ctx context.Context) error {
	s.store.mu.RLock()
	account, epoch, status := s.store.state.Account.ID, s.store.accountEpoch, s.store.state.Account.Status
	enabled, _ := s.store.state.Settings["moments.reply_enabled"].(bool)
	probability, _ := s.store.state.Settings["moments.comment_probability"].(float64)
	min, _ := s.store.state.Settings["moments.comment_interval_min"].(float64)
	max, _ := s.store.state.Settings["moments.comment_interval_max"].(float64)
	s.store.mu.RUnlock()
	if account == "" || status != "online" {
		return nil
	}
	job := momentRandomJob{ID: randomID(), Kind: "reply", Status: "running", Message: "收到对方回复，准备回应…", Account: account, Epoch: epoch, Probability: 100, IntervalMin: int(min), IntervalMax: int(max)}
	decisions := momentReplyDecisions{Seen: map[string]bool{}}
	s.momentRandomMu.Lock()
	if s.momentRandomCancel != nil {
		s.momentRandomMu.Unlock()
		return nil
	}
	err := s.momentRandomLoad(account, "reply-decisions", &decisions)
	if err == nil && (decisions.After == 0 || !enabled) {
		if decisions.After == 0 && enabled {
			decisions.After = s.momentReplyStarted.Unix()
		} else {
			decisions.After = time.Now().Unix()
		}
		err = s.momentRandomSave(job, "reply-decisions", decisions)
		s.momentRandomMu.Unlock()
		return err
	}
	s.momentRandomMu.Unlock()
	if err != nil {
		return err
	}
	syncCtx, cancel := context.WithTimeout(ctx, time.Minute)
	notifications, _, err := s.syncMomentNotifications(syncCtx)
	cancel()
	if err != nil {
		return err
	}
	s.momentRandomMu.Lock()
	defer s.momentRandomMu.Unlock()
	if s.momentRandomCancel != nil {
		return nil
	}
	if err = s.momentRandomValid(ctx, job); err != nil {
		return err
	}
	if decisions.Seen == nil {
		decisions.Seen = map[string]bool{}
	}
	sort.SliceStable(notifications.Items, func(i, j int) bool { return notifications.Items[i].Created < notifications.Items[j].Created })
	for _, n := range notifications.Items {
		if n.Kind != "comment" || !n.ReplyToSelf || n.ActorID == "" || n.ActorID == account || n.Created <= decisions.After || decisions.Seen[n.ID] {
			continue
		}
		decisions.Seen[n.ID] = true
		if err = s.momentRandomSave(job, "reply-decisions", decisions); err != nil {
			return err
		}
		if n.CommentID <= 0 {
			_, _ = s.store.appendLog(account, epoch, "WARN", "moments", n.ActorID, "朋友圈回复未发送：通知缺少当前 core 可用的评论编号，未猜测或改为普通留言")
			continue
		}
		if rand.IntN(100) >= int(probability) {
			_, _ = s.store.appendLog(account, epoch, "INFO", "moments", n.ActorID, fmt.Sprintf("朋友圈回复未发送：未命中随机评论概率 %g%%", probability))
			continue
		}
		now := time.Now()
		job.Started = now.Format(time.RFC3339Nano)
		job.Until = time.Unix(decisions.After, 0).Format(time.RFC3339Nano)
		job.Notification = &n
		if err = s.momentRandomSave(job, "job", job); err != nil {
			return err
		}
		s.launchMomentRandomLocked(job)
		return nil
	}
	return nil
}

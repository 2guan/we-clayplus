// 朋友圈随机互动仅由管理员点击启动；任务串行执行，未知写入结果不会自动重放。
package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

type momentRandomJob struct {
	Notification *MomentNotification `json:"-"`
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	Status       string              `json:"status"`
	Total        int                 `json:"total"`
	Checked      int                 `json:"checked"`
	Sent         int                 `json:"sent"`
	Skipped      int                 `json:"skipped"`
	Message      string              `json:"message"`
	Started      string              `json:"started"`
	Until        string              `json:"until"`
	Account      string              `json:"-"`
	Epoch        uint64              `json:"-"`
	Probability  int                 `json:"probability"`
	IntervalMin  int                 `json:"interval_min"`
	IntervalMax  int                 `json:"interval_max"`
}

func (s *Server) momentRandomLoad(account, key string, dst any) error {
	raw, err := s.store.getSecret("moments-random:" + account + ":" + key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), dst)
}
func (s *Server) momentRandomSave(job momentRandomJob, key string, v any) error {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.Account.ID != job.Account || s.store.accountEpoch != job.Epoch {
		return fmt.Errorf("账号已切换，随机互动停止")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.store.putSecret("moments-random:"+job.Account+":"+key, string(raw))
}
func (s *Server) momentRandomAPI(w http.ResponseWriter, r *http.Request, u User) {
	w.Header().Set("Cache-Control", "no-store")
	if requireAdmin(u) != nil {
		writeError(w, 403, "仅管理员可以执行随机互动")
		return
	}
	s.momentRandomMu.Lock()
	defer s.momentRandomMu.Unlock()
	account, epoch := s.momentNotificationAccount()
	if r.Method == http.MethodGet {
		var job momentRandomJob
		if s.momentRandomJob != nil && s.momentRandomJob.Account == account && s.momentRandomJob.Epoch == epoch {
			job = *s.momentRandomJob
		} else {
			if err := s.momentRandomLoad(account, "job", &job); err != nil {
				writeError(w, 500, "任务记录读取失败")
				return
			}
			if job.Status == "running" {
				job.Status, job.Message = "interrupted", "服务或账号会话已切换，任务停止；不会自动续发"
			}
		}
		writeJSON(w, 200, job)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Kind string `json:"kind"`
		Stop string `json:"stop"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.Stop != "" {
		if s.momentRandomCancel == nil || s.momentRandomJob.Account != account || s.momentRandomJob.ID != q.Stop {
			writeError(w, 409, "该任务已停止或不属于当前账号")
			return
		}
		s.momentRandomCancel()
		writeJSON(w, 202, *s.momentRandomJob)
		return
	}
	if q.Kind != "like" && q.Kind != "comment" {
		writeError(w, 400, "请选择随机点赞或随机评论")
		return
	}
	if s.momentRandomCancel != nil {
		writeError(w, 409, "已有随机互动任务执行中，请先停止或等待完成")
		return
	}
	st := s.store.snapshot()
	if account == "" || (!s.cfg.Mock && st.Account.Status != "online") {
		writeError(w, 409, "微信尚未登录")
		return
	}
	if st.Account.ID != account || st.AccountEpoch != epoch {
		writeError(w, 409, "账号已切换")
		return
	}
	n := func(key string) int { v, _ := st.Settings["moments."+key].(float64); return int(v) }
	now := time.Now()
	job := momentRandomJob{ID: randomID(), Kind: q.Kind, Status: "running", Message: "正在读取追溯范围内的朋友圈…", Started: now.Format(time.RFC3339Nano), Until: now.Add(-time.Duration(n("lookback_hours")) * time.Hour).Format(time.RFC3339Nano), Account: account, Epoch: epoch, Probability: n(q.Kind + "_probability"), IntervalMin: n("comment_interval_min"), IntervalMax: n("comment_interval_max")}
	if n("lookback_hours") < 1 || n("lookback_hours") > 8760 || job.Probability < 0 || job.Probability > 100 || job.IntervalMin < 1 || job.IntervalMax < job.IntervalMin {
		writeError(w, 400, "朋友圈随机互动配置无效")
		return
	}
	if err := s.momentRandomSave(job, "job", job); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.launchMomentRandomLocked(job)
	writeJSON(w, 202, job)
}
func (s *Server) launchMomentRandomLocked(job momentRandomJob) {
	ctx, cancel := context.WithCancel(s.ctx)
	copy := job
	s.momentRandomJob, s.momentRandomCancel = &copy, cancel
	s.workers.Add(1)
	go func() { defer s.workers.Done(); defer cancel(); s.runMomentRandom(ctx, job) }()
}
func (s *Server) momentRandomProgress(job momentRandomJob) error {
	s.momentRandomMu.Lock()
	defer s.momentRandomMu.Unlock()
	if err := s.momentRandomSave(job, "job", job); err != nil {
		return err
	}
	copy := job
	s.momentRandomJob = &copy
	return nil
}
func (s *Server) momentRandomValid(ctx context.Context, job momentRandomJob) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	account, epoch := s.momentNotificationAccount()
	if account != job.Account || epoch != job.Epoch {
		return fmt.Errorf("账号会话已更新，随机互动停止")
	}
	return nil
}
func (s *Server) momentRandomPage(ctx context.Context, cursor string) (MomentPage, error) {
	if s.cfg.Mock {
		return MomentPage{Items: s.store.snapshot().Moments}, nil
	}
	if s.host == nil {
		return MomentPage{}, fmt.Errorf("微信尚未就绪")
	}
	raw, err := s.host.Execute(ctx, control.Command{Operation: "moments-page", ID: cursor})
	if err != nil {
		return MomentPage{}, err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return MomentPage{}, err
	}
	var page MomentPage
	err = json.Unmarshal(data, &page)
	return page, err
}
func (s *Server) momentRandomCandidates(ctx context.Context, job momentRandomJob) ([]Moment, error) {
	from, _ := time.Parse(time.RFC3339Nano, job.Until)
	until, _ := time.Parse(time.RFC3339Nano, job.Started)
	items := []Moment{}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	for {
		if err := s.momentRandomValid(ctx, job); err != nil {
			return nil, err
		}
		readCtx, cancel := context.WithTimeout(ctx, time.Minute)
		page, err := s.momentRandomPage(readCtx, cursor)
		cancel()
		if err != nil {
			return nil, err
		}
		allOlder := len(page.Items) > 0
		for _, m := range page.Items {
			t, e := time.Parse(time.RFC3339, m.Time)
			if e != nil {
				allOlder = false
				continue
			}
			if !t.Before(from) {
				allOlder = false
			}
			if e == nil && !t.Before(from) && !t.After(until) && m.ID != "" && m.AuthorID != "" && m.AuthorID != job.Account && !seen[m.ID] {
				seen[m.ID] = true
				items = append(items, m)
			}
		}
		if allOlder || page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor || cursors[page.NextCursor] {
			return nil, fmt.Errorf("微信分页未推进，尚未完成读取；未执行互动")
		}
		cursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339, items[i].Time)
		b, _ := time.Parse(time.RFC3339, items[j].Time)
		return a.After(b)
	})
	return items, nil
}
func momentRandomAlready(m Moment, kind, account string) bool {
	if kind == "like" {
		if m.Liked {
			return true
		}
		for _, person := range m.LikeUsers {
			if person.ID == account {
				return true
			}
		}
		return false
	}
	if kind == "reply" {
		return false
	}
	for _, c := range m.Comments {
		if c.AuthorID == account {
			return true
		}
	}
	return false
}
func momentRandomWait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (s *Server) runMomentRandom(ctx context.Context, job momentRandomJob) {
	var runErr error
	defer func() {
		job.Status = "completed"
		job.Message = "处理完成"
		if runErr != nil {
			job.Status = "failed"
			job.Message = runErr.Error()
			if ctx.Err() != nil {
				job.Status = "cancelled"
				job.Message = "任务已停止；已提交的互动不会撤销，也不会自动续发"
			}
		}
		_ = s.momentRandomProgress(job)
		s.momentRandomMu.Lock()
		copy := job
		s.momentRandomJob = &copy
		s.momentRandomCancel = nil
		s.momentRandomMu.Unlock()
		_, _ = s.store.appendLog(job.Account, job.Epoch, "INFO", "moments", "", fmt.Sprintf("随机%s结束：成功 %d，跳过 %d；%s", map[string]string{"like": "点赞", "comment": "评论", "reply": "回复"}[job.Kind], job.Sent, job.Skipped, job.Message))
		s.notify()
	}()
	var items []Moment
	var err error
	if job.Notification != nil {
		items = []Moment{{ID: job.Notification.MomentID, AuthorID: job.Notification.ActorID}}
	} else {
		items, err = s.momentRandomCandidates(ctx, job)
	}
	if err != nil {
		runErr = err
		return
	}
	job.Total = len(items)
	ledger := map[string]string{}
	if err = s.momentRandomLoad(job.Account, "attempts", &ledger); err != nil {
		runErr = err
		return
	}
	nextComment, _ := time.Parse(time.RFC3339Nano, ledger["next_comment_at"])
	for _, m := range items {
		if err = s.momentRandomValid(ctx, job); err != nil {
			runErr = err
			return
		}
		job.Checked++
		job.Message = "正在处理 " + m.Author + " 的动态"
		if err = s.momentRandomProgress(job); err != nil {
			runErr = err
			return
		}
		key := job.Kind + ":" + m.ID
		if job.Notification != nil {
			key = "reply:" + job.Notification.ID
		}
		if momentRandomAlready(m, job.Kind, job.Account) || ledger[key] != "" || rand.IntN(100) >= job.Probability {
			job.Skipped++
			continue
		}
		if job.Kind != "like" {
			if err = momentRandomWait(ctx, time.Until(nextComment)); err != nil {
				runErr = err
				return
			}
		}
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		current, e := s.momentDraftSource(readCtx, s.store.snapshot(), m.ID)
		var text string
		if e == nil && !momentRandomAlready(current, job.Kind, job.Account) && job.Kind != "like" {
			var reply []Comment
			if job.Notification != nil {
				for _, c := range current.Comments {
					if c.ID == fmt.Sprint(job.Notification.CommentID) && c.AuthorID == job.Notification.ActorID {
						reply = append(reply, c)
						break
					}
				}
				if len(reply) == 0 {
					e = fmt.Errorf("对方回复已不可用，未发送普通留言代替")
				}
			}
			if e == nil {
				text, _, e = s.generateMomentDraft(readCtx, s.store.snapshot(), current, reply...)
			}
		}
		cancel()
		if e != nil {
			runErr = e
			return
		}
		if momentRandomAlready(current, job.Kind, job.Account) {
			job.Skipped++
			continue
		}
		if err = s.momentRandomValid(ctx, job); err != nil {
			runErr = err
			return
		}
		// 持久化提交意图先于实际写入；失败或中断后保留，避免下一次点击重复未知操作。
		s.momentInteractionMu.Lock()
		err = s.momentRandomValid(ctx, job)
		if err == nil {
			ledger[key] = "attempted"
			if job.Kind != "like" {
				ledger["next_comment_at"] = time.Now().Add(time.Duration(job.IntervalMin+rand.IntN(job.IntervalMax-job.IntervalMin+1)) * time.Second).Format(time.RFC3339Nano)
			}
			err = s.momentRandomSave(job, "attempts", ledger)
			if err == nil {
				err = s.sendMomentRandom(ctx, job, current, text)
			}
		}
		s.momentInteractionMu.Unlock()
		if err != nil {
			if strings.Contains(err.Error(), "已点赞，跳过") || strings.Contains(err.Error(), "本人已评论，跳过") {
				job.Skipped++
				continue
			}
			runErr = err
			return
		}
		ledger[key] = "completed"
		job.Sent++
		if job.Kind != "like" {
			nextComment = time.Now().Add(time.Duration(job.IntervalMin+rand.IntN(job.IntervalMax-job.IntervalMin+1)) * time.Second)
			ledger["next_comment_at"] = nextComment.Format(time.RFC3339Nano)
		} else {
			if err = momentRandomWait(ctx, time.Second); err != nil {
				runErr = err
				return
			}
		}
		if err = s.momentRandomSave(job, "attempts", ledger); err != nil {
			runErr = err
			return
		}
	}
}
func (s *Server) sendMomentRandom(ctx context.Context, job momentRandomJob, m Moment, text string) error {
	if s.cfg.Mock {
		return s.store.change(func(st *State) error {
			if st.Account.ID != job.Account || s.store.accountEpoch != job.Epoch {
				return fmt.Errorf("账号已切换")
			}
			for i := range st.Moments {
				v := &st.Moments[i]
				if v.ID == m.ID {
					if momentRandomAlready(*v, job.Kind, job.Account) {
						return fmt.Errorf("本人已评论，跳过")
					}
					if job.Kind == "like" {
						v.Liked = true
						v.Likes++
						v.LikeUsers = append(v.LikeUsers, MomentLike{ID: job.Account, Name: st.Account.Name})
					} else {
						comment := Comment{ID: randomID(), AuthorID: job.Account, Author: st.Account.Name, Content: text}
						if job.Notification != nil {
							comment.ReplyToID = job.Notification.ActorID
							comment.ReplyTo = job.Notification.Actor
						}
						v.Comments = append(v.Comments, comment)
					}
					return nil
				}
			}
			return fmt.Errorf("动态已不可用")
		})
	}
	paramsMap := map[string]any{"only_if_missing": true, "account": job.Account}
	if job.Notification != nil {
		paramsMap["reply_comment_id"] = job.Notification.CommentID
		paramsMap["reply_author_id"] = job.Notification.ActorID
	}
	params, _ := json.Marshal(paramsMap)
	_, err := s.host.Execute(ctx, control.Command{Operation: "moments-" + map[string]string{"like": "like", "comment": "comment", "reply": "comment"}[job.Kind], ID: m.ID, Target: m.AuthorID, Content: text, Params: params})
	return err
}

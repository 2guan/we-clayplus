// 文件名：business_jobs.go
// 主要功能：管理按账号隔离的单次/Cron 任务，先持久化执行占位再产生副作用，拒绝自动重发未知结果。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"slices"
	"strings"
	"time"
)

type businessJob struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Kind       string          `json:"kind"`
	At         string          `json:"at"`
	Expression string          `json:"expression"`
	Timezone   string          `json:"timezone"`
	Request    businessRequest `json:"request"`
	Next       string          `json:"next"`
	Last       string          `json:"last"`
	Revision   int             `json:"revision"`
}

func (s *Server) businessJobs(account string) ([]businessJob, error) {
	rows, e := s.store.db.Query("SELECT body FROM business_jobs WHERE account=? ORDER BY id", account)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []businessJob{}
	for rows.Next() {
		var raw []byte
		var j businessJob
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &j); e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func validateBusinessJob(j *businessJob, st State, now time.Time) error {
	if strings.TrimSpace(j.Name) == "" || len(j.Name) > 120 {
		return fmt.Errorf("任务名称应为 1 到 120 字节")
	}
	if j.Request.Plugin != "text" && (!isBusinessPlugin(j.Request.Plugin) || slices.Contains([]string{"cron", "auto_accept", "welcome"}, j.Request.Plugin)) {
		return fmt.Errorf("此插件不支持定时任务")
	}
	if !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == j.Request.Target }) {
		return fmt.Errorf("任务目标不存在")
	}
	if j.Request.Media != "" || len(j.Request.Query) > 8000 || j.Request.Days < 0 || j.Request.Days > 365 {
		return fmt.Errorf("任务参数无效，媒体请使用已保存素材")
	}
	if j.Request.Plugin == "text" && strings.TrimSpace(j.Request.Query) == "" {
		return fmt.Errorf("请输入发送文字")
	}
	loc, e := time.LoadLocation(j.Timezone)
	if e != nil {
		return fmt.Errorf("时区无效")
	}
	j.Request.Send = true
	switch j.Kind {
	case "once":
		at, e := time.Parse(time.RFC3339, j.At)
		if e != nil || !at.After(now) {
			return fmt.Errorf("单次任务必须设置未来的时间")
		}
		j.Next = at.UTC().Format(time.RFC3339)
	case "cron":
		cron, e := toolbox.ParseSchedule(j.Expression)
		if e != nil {
			return e
		}
		next, e := cron.Next(now.In(loc))
		if e != nil {
			return e
		}
		j.Next = next.UTC().Format(time.RFC3339)
	default:
		return fmt.Errorf("请选择单次或周期任务")
	}
	return nil
}
func (s *Server) saveBusinessJob(j businessJob) (businessJob, error) {
	s.businessMu.Lock()
	defer s.businessMu.Unlock()
	st := s.store.snapshot()
	if e := validateBusinessJob(&j, st, time.Now()); e != nil {
		return j, e
	}
	jobs, e := s.businessJobs(st.Account.ID)
	if e != nil {
		return j, e
	}
	if j.ID == "" {
		if len(jobs) >= 100 {
			return j, fmt.Errorf("每个账号最多 100 项任务")
		}
		j.ID = randomID()
		j.Revision = 1
	} else {
		found := false
		for _, old := range jobs {
			if old.ID == j.ID {
				found = true
				if j.Revision != old.Revision {
					return j, fmt.Errorf("任务已变化，请重新读取")
				}
				j.Revision++
				j.Last = old.Last
			}
		}
		if !found {
			return j, fmt.Errorf("任务不存在")
		}
	}
	raw, _ := json.Marshal(j)
	_, e = s.store.db.Exec("INSERT INTO business_jobs VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", j.ID, st.Account.ID, raw)
	s.notify()
	return j, e
}
func (s *Server) deleteBusinessJob(id string, revision int) error {
	s.businessMu.Lock()
	defer s.businessMu.Unlock()
	account := s.store.snapshot().Account.ID
	jobs, e := s.businessJobs(account)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if j.ID == id {
			if j.Revision != revision {
				return fmt.Errorf("任务已变化，请重新读取")
			}
			_, e = s.store.db.Exec("DELETE FROM business_jobs WHERE id=? AND account=?", id, account)
			return e
		}
	}
	return fmt.Errorf("任务不存在")
}
func (s *Server) scheduler() {
	defer s.workers.Done()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-tick.C:
			s.tickBusiness(now)
		}
	}
}
func (s *Server) tickBusiness(now time.Time) {
	s.businessMu.Lock()
	st := s.store.snapshot()
	if !businessEnabled(st, "cron") {
		s.businessMu.Unlock()
		return
	}
	jobs, e := s.businessJobs(st.Account.ID)
	if e != nil {
		s.businessMu.Unlock()
		return
	}
	due := []businessJob{}
	for _, j := range jobs {
		next, err := time.Parse(time.RFC3339, j.Next)
		if !j.Enabled || err != nil || next.After(now) {
			continue
		}
		old := j
		j.Last = now.UTC().Format(time.RFC3339)
		j.Revision++
		if j.Kind == "once" {
			j.Enabled = false
		} else {
			schedule, e := toolbox.ParseSchedule(j.Expression)
			loc, err := time.LoadLocation(j.Timezone)
			if e != nil || err != nil {
				j.Enabled = false
			} else {
				next, e = schedule.Next(now.In(loc))
				if e != nil {
					j.Enabled = false
				} else {
					j.Next = next.UTC().Format(time.RFC3339)
				}
			}
		}
		raw, _ := json.Marshal(j)
		if _, e = s.store.db.Exec("UPDATE business_jobs SET body=? WHERE id=? AND account=?", raw, j.ID, st.Account.ID); e != nil {
			continue
		} // 错过超过一分钟的触发不补发，避免停机后突发消息。
		when, _ := time.Parse(time.RFC3339, old.Next)
		if now.Sub(when) > time.Minute {
			id, _ := s.beginBusiness(st.Account.ID, businessRequest{Plugin: "cron", Target: j.Request.Target}, "skipped")
			s.finishBusiness(id, "skipped", "错过执行窗口，不补发")
			continue
		}
		due = append(due, old)
	}
	s.businessMu.Unlock()
	for _, j := range due {
		j := j
		if !s.launchBusiness(func() {
			if err := s.runBusinessJob(s.ctx, st.Account.ID, st.AccountEpoch, j, "schedule:"+j.ID+":"+j.Next); err != nil {
				id, e := s.beginBusiness(st.Account.ID, businessRequest{Plugin: "cron", Target: j.Request.Target}, "failed")
				if e == nil {
					s.finishBusiness(id, "failed", j.Name+" · "+err.Error())
				}
			}
		}) {
			id, e := s.beginBusiness(st.Account.ID, businessRequest{Plugin: "cron", Target: j.Request.Target}, "skipped")
			if e == nil {
				s.finishBusiness(id, "skipped", "事件队列已满或服务关闭")
			}
		}
	}
}
func (s *Server) runBusinessJob(ctx context.Context, account string, epoch uint64, j businessJob, key string) error {
	st := s.store.snapshot()
	if st.Account.ID != account || st.AccountEpoch != epoch {
		return fmt.Errorf("账号已切换，本次跳过")
	}
	// 调度占位后如果用户删除或编辑任务，取消本次执行；手动执行是独立明确操作。
	if strings.HasPrefix(key, "schedule:") {
		jobs, e := s.businessJobs(account)
		if e != nil {
			return e
		}
		valid := false
		for _, current := range jobs {
			if current.ID == j.ID && current.Revision == j.Revision+1 {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("任务已修改或删除，本次跳过")
		}
	}
	if !businessAllowed(st, "cron", j.Request.Target) {
		return fmt.Errorf("定时任务未启用或目标不在生效范围")
	}
	ok, e := s.claimBusiness(account, key)
	if e != nil {
		return e
	}
	if !ok {
		return fmt.Errorf("该次任务已经执行，不重复发送")
	}
	delivery := j.Request
	delivery.jobID = j.ID
	delivery.jobRevision = j.Revision
	delivery.Send = true
	if strings.HasPrefix(key, "schedule:") {
		delivery.jobRevision++
	}
	if j.Request.Plugin != "text" {
		_, e = s.executeBusinessFor(ctx, account, epoch, delivery, "定时任务 · "+j.Name)
		return e
	}
	id, e := s.beginBusiness(account, businessRequest{Plugin: "cron", Target: j.Request.Target}, "running")
	if e != nil {
		return e
	}
	delivery.Plugin = "cron"
	e = s.deliverBusinessFor(ctx, account, epoch, delivery, businessResult{Text: j.Request.Query})
	if e != nil {
		s.finishBusiness(id, "unknown", "发送结果未确认，不自动重发")
	} else {
		s.finishBusiness(id, "sent", "定时文字已发送")
	}
	return e
}

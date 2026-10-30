// 文件名：business_runtime.go
// 主要功能：持久化插件执行记录与去重凭据，分发受限命令、欢迎事件和好友验证。
// 作者：2guan
package dashboard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type businessRun struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Target string `json:"target"`
	Time   string `json:"time"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func (s *Server) initBusiness() error {
	_, e := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS business_previews(id TEXT PRIMARY KEY,account TEXT NOT NULL,time TEXT NOT NULL,used INTEGER NOT NULL,body BLOB NOT NULL);CREATE TABLE IF NOT EXISTS business_runs(id TEXT PRIMARY KEY,account TEXT NOT NULL,plugin TEXT NOT NULL,target TEXT NOT NULL,time TEXT NOT NULL,status TEXT NOT NULL,detail TEXT NOT NULL);CREATE TABLE IF NOT EXISTS business_claims(account TEXT NOT NULL,id TEXT NOT NULL,time TEXT NOT NULL,PRIMARY KEY(account,id));CREATE TABLE IF NOT EXISTS business_jobs(id TEXT PRIMARY KEY,account TEXT NOT NULL,body BLOB NOT NULL);CREATE TABLE IF NOT EXISTS business_assets(id TEXT PRIMARY KEY,account TEXT NOT NULL,name TEXT NOT NULL,media TEXT NOT NULL);`)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("UPDATE business_runs SET status='interrupted',detail='服务中断，结果可能未知；不会自动重发' WHERE status='running'")
	return e
}
func (s *Server) claimBusiness(account, key string) (bool, error) {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	res, e := s.store.db.Exec("INSERT OR IGNORE INTO business_claims(account,id,time) VALUES(?,?,?)", account, sum, time.Now().Format(time.RFC3339))
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}
func (s *Server) beginBusiness(account string, q businessRequest, status string) (string, error) {
	id := randomID()
	_, e := s.store.db.Exec("INSERT INTO business_runs VALUES(?,?,?,?,?,?,?)", id, account, q.Plugin, q.Target, time.Now().Format(time.RFC3339), status, "")
	return id, e
}
func (s *Server) finishBusiness(id, status, detail string) {
	_, e := s.store.db.Exec("UPDATE business_runs SET status=?,detail=? WHERE id=?", status, detail, id)
	if e != nil {
		return
	} // 记录只保存运行摘要，不保存申请票据、媒体或模型输入。
	_, _ = s.store.db.Exec("DELETE FROM business_runs WHERE id IN (SELECT id FROM business_runs ORDER BY time DESC LIMIT -1 OFFSET 2000)")
	_, _ = s.store.db.Exec("DELETE FROM business_claims WHERE time < ?", time.Now().AddDate(0, 0, -30).Format(time.RFC3339))
	s.notify()
}
func (s *Server) executeBusiness(ctx context.Context, q businessRequest, source string) (businessResult, error) {
	st := s.store.snapshot()
	return s.executeBusinessFor(ctx, st.Account.ID, st.AccountEpoch, q, source)
}

// executeBusinessFor keeps the identity captured by the event or scheduler. A queued
// request must never adopt whichever account happens to be active when it runs.
func (s *Server) executeBusinessFor(ctx context.Context, account string, epoch uint64, q businessRequest, source string) (businessResult, error) {
	empty := businessResult{}
	st := s.store.snapshot()
	if st.Account.ID != account || st.AccountEpoch != epoch {
		return empty, fmt.Errorf("账号已切换，请重新执行")
	}
	if !isBusinessPlugin(q.Plugin) {
		return empty, fmt.Errorf("插件不存在")
	}
	if q.Send && !businessAllowed(st, q.Plugin, q.Target) {
		return empty, fmt.Errorf("插件未启用或目标不在生效范围，请先配置名单")
	}
	select {
	case s.businessSlots <- struct{}{}:
		defer func() { <-s.businessSlots }()
	default:
		return empty, fmt.Errorf("插件任务繁忙，请稍后重试")
	}
	id, e := s.beginBusiness(account, q, "running")
	if e != nil {
		return empty, e
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	if active := s.store.snapshot(); active.Account.ID != account || active.AccountEpoch != epoch {
		s.finishBusiness(id, "skipped", source+" · 账号已切换")
		return empty, fmt.Errorf("账号已切换，请重新执行")
	}
	r, e := s.buildBusinessFor(ctx, q, st)
	if active := s.store.snapshot(); active.Account.ID != account || active.AccountEpoch != epoch {
		e = fmt.Errorf("账号已切换，请重新预览")
		r = businessResult{}
	}
	status, detail := "previewed", "预览完成，未发送"
	if e == nil && len(r.data) > 0 && r.Media == "" {
		r.Media, e = s.saveMedia(r.data, r.MIME)
	}
	if e == nil && len(r.Images) > 0 {
		e = s.saveBusinessImages(&r)
	}
	if e == nil && !q.Send {
		r.Preview, e = s.saveBusinessPreview(account, q, r)
	}
	attempted := false
	if e == nil && q.Send {
		attempted = true
		e = s.deliverBusinessFor(ctx, account, epoch, q, r)
		status = "sent"
		detail = "发送请求已完成"
	}
	if e != nil {
		status = "failed"
		detail = e.Error()
		if attempted {
			status = "unknown"
			detail = "未确认发送成功，请检查运行状态；不自动重试"
			if len(r.Images) > 0 {
				detail += "：" + e.Error()
			}
		} else if q.Send && (q.Plugin == "music" || q.Plugin == "news") && ctx.Err() == nil {
			// 搜索失败也回应原会话；已尝试发卡片时不再补发，避免未知结果重复发送。
			label := map[string]string{"music": "点歌失败", "news": "新闻获取失败"}[q.Plugin]
			feedback := businessResult{Text: label + "：" + e.Error() + "。稍后可以重试。"}
			if notifyErr := s.deliverBusinessFor(ctx, account, epoch, q, feedback); notifyErr != nil {
				status = "unknown"
				detail += "；失败提示也未确认发送成功，不自动重发"
			}
		}
	}
	if r.MusicSource != "" {
		detail += " · 音源：" + r.MusicSource
	}
	s.finishBusiness(id, status, source+" · "+detail)
	return r, e
}
func (s *Server) businessMediaPath(media string) string {
	return filepath.Join(s.cfg.DataDir, "media", filepath.Base(media))
}

// 同时最多 64 个等待事件，超过队列容量时由调用方记录跳过，不堆积无限协程。
func (s *Server) launchBusiness(fn func()) bool {
	if s.ctx.Err() != nil {
		return false
	}
	select {
	case s.businessPending <- struct{}{}:
	default:
		return false
	}
	s.workers.Add(1)
	go func() { defer s.workers.Done(); defer func() { <-s.businessPending }(); fn() }()
	return true
}

func (s *Server) businessCooldown(account, id, target string) bool {
	seconds := businessNumber(s.store.snapshot(), id, "cooldown")
	if seconds <= 0 {
		return true
	}
	var n int
	e := s.store.db.QueryRow("SELECT count(*) FROM business_runs WHERE account=? AND plugin=? AND target=? AND status!='previewed' AND time>=?", account, id, target, time.Now().Add(-time.Duration(seconds)*time.Second).Format(time.RFC3339)).Scan(&n)
	return e == nil && n == 0
}

var joinPatterns = []*regexp.Regexp{regexp.MustCompile(`^["“]([^"”]+)["”].*邀请["“]([^"”]+)["”]加入了群聊`), regexp.MustCompile(`^["“]([^"”]+)["”]通过.*加入了群聊`)}

var businessMentionText = regexp.MustCompile(`@[^@\r\n\x{2005}]+\x{2005}`)

func businessCommandText(m Message) string {
	text := m.Content
	if len(m.Mentions) > 0 || m.MentionAll {
		// 微信提醒可放在句首或句尾，昵称中也可能有空格；提醒授权仍单独核验 ID。
		text = businessMentionText.ReplaceAllString(text, " ")
	}
	return toolbox.CommandText(text)
}

func joinName(text string) string {
	for i, re := range joinPatterns {
		m := re.FindStringSubmatch(text)
		if len(m) > 1 {
			if i == 0 {
				return m[2]
			}
			return m[1]
		}
	}
	return ""
}

// Record 已完成原始消息去重；再用持久化事件键防止重启重放产生副作用。
func (s *Server) dispatchBusiness(m Message) bool {
	if m.Self || m.Source != "wechat" {
		return false
	}
	st := s.store.snapshot()
	// 海龟汤开局由专用路由判断真实 @；未满足条件的开局不落入通用插件或普通 AI。
	if m.Kind == "text" || m.Kind == "quote" {
		command, _ := soupCommand(soupText(st, m))
		// 展示文字仅用于拦住普通 AI 兜底，不能作为真实 @ 的授权依据。
		displayCommand, _ := soupCommand(toolbox.CommandText(m.Content))
		if (command == "start" || displayCommand == "start") && businessAllowed(st, "turtlesoup", m.Target) {
			if !soupStartAllowed(st, m) {
				s.replyDecision(m, "跳过", "群内开启海龟汤要求真实 @ 本人，本条未满足；不由普通聊天兜底")
			}
			return true
		}
	}
	id, arg := "", ""
	if m.Kind == "text" || m.Kind == "quote" {
		id, arg = toolbox.Match(businessCommandText(m))
	} else if m.Kind == "system" {
		arg = joinName(m.Content)
		if arg != "" {
			id = "welcome"
		}
	}
	if id == "" {
		return false
	}
	if !businessAllowed(st, id, m.Target) {
		return false
	}
	if (m.Kind == "text" || m.Kind == "quote") && memoryGroup(st, m.Target) && st.Settings[id+".group_mention_required"] != false && (st.Account.ID == "" || !slices.Contains(m.Mentions, st.Account.ID)) {
		s.replyDecision(m, "跳过", "业务插件 "+id+" 群聊触发要求真实 @ 本人；本条未满足，不执行也不由普通聊天兜底")
		return true
	}
	if id == "welcome" && !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == m.Target && c.Kind == "group" }) {
		return false
	}
	s.businessMu.Lock()
	defer s.businessMu.Unlock()
	if id != "news" && !s.businessCooldown(st.Account.ID, id, m.Target) {
		s.replyDecision(m, "跳过", "业务插件 "+id+" 处于冷却时间，本次不发送，也不转交普通聊天")
		return true
	}
	claimed, e := s.claimBusiness(st.Account.ID, "message:"+id+":"+m.Target+":"+m.ID)
	if e != nil || !claimed {
		reason := "业务插件 " + id + " 已处理过此消息，跳过重复触发"
		if e != nil {
			reason = "业务插件 " + id + " 触发登记失败：" + e.Error()
		}
		s.replyDecision(m, "跳过", reason)
		return true
	}
	q := businessCommand(id, m.Target, arg)
	q.Send = true
	if id == "news" {
		q = normalizeNewsRequest(st, q)
		allowed, err := s.claimNewsCooldown(st.Account.ID, q, time.Now())
		if err != nil || !allowed {
			marker, e := s.beginBusiness(st.Account.ID, q, "skipped")
			if e == nil {
				detail := fmt.Sprintf("微信触发 · 冷却中（%s）；同类请求间隔 %d 秒，新闻、图卦和 RSS 分别计时", map[string]string{"today": "今日新闻", "diagram": "今日图卦", "rss": "RSS 新闻"}[q.Mode], businessNumber(st, id, "cooldown"))
				if err != nil {
					s.finishBusiness(marker, "failed", "冷却状态检查失败，本次未发送")
				} else {
					s.finishBusiness(marker, "skipped", detail)
				}
			}
			return true
		}
	}
	// 先同步写占位记录用于冷却，再异步处理，避免短时间事件同时穿过限制。
	marker, e := s.beginBusiness(st.Account.ID, q, "accepted")
	if e != nil {
		return true
	}
	if !s.launchBusiness(func() {
		if id == "welcome" {
			timer := time.NewTimer(time.Duration(businessNumber(st, id, "delay_seconds")) * time.Second)
			defer timer.Stop()
			select {
			case <-s.ctx.Done():
				return
			case <-timer.C:
			}
		}
		if active := s.store.snapshot(); active.Account.ID != st.Account.ID || active.AccountEpoch != st.AccountEpoch {
			s.finishBusiness(marker, "skipped", "账号发生变化")
			return
		}
		_, err := s.executeBusinessFor(s.ctx, st.Account.ID, st.AccountEpoch, q, "微信触发")
		if err != nil {
			s.finishBusiness(marker, "failed", "自动执行失败，请查看相邻记录")
		} else {
			s.finishBusiness(marker, "completed", "自动执行完成")
		}
	}) {
		s.finishBusiness(marker, "skipped", "事件队列已满或服务关闭")
	}
	return true
}

// 同一规则用于网页预览和真实事件，延迟结束后也必须重新检查。
func friendMatchReason(st State, q FriendRequest) string {
	if !businessAllowed(st, "auto_accept", q.Applicant) {
		return "插件未启用或申请人不在生效范围"
	}
	if key := businessText(st, "auto_accept", "keyword"); key != "" && !strings.Contains(q.Comment, key) {
		return "附言关键词不匹配"
	}
	if q.Scene < 0 || q.Scene > 1000 {
		return "来源场景无效"
	}
	if scenes := businessText(st, "auto_accept", "scenes"); scenes != "" {
		for _, v := range strings.Split(scenes, ",") {
			if strings.TrimSpace(v) == strconv.Itoa(q.Scene) {
				return ""
			}
		}
		return "来源场景不在允许列表"
	}
	return ""
}

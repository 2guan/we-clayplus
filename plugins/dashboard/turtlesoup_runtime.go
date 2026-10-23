// 海龟汤按会话串行处理，网络调用不持有全局锁；消息先归档再异步执行。
package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/we-clay/we-clay/plugins/turtlesoup"
)

type soupNoPuzzle struct{}

func (soupNoPuzzle) Error() string {
	return "模型出题未成功，固定题库也没有可用的对应主题；请重试或换一碗"
}

func soupText(st State, m Message) string {
	text := strings.TrimSpace(m.Content)
	if slices.Contains(m.Mentions, st.Account.ID) {
		text = businessCommandText(m)
	}
	return strings.TrimRightFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("。！!？?", r) })
}
func soupCommand(text string) (string, string) {
	if text == "来碗海龟汤" {
		return "start", ""
	}
	if strings.HasPrefix(text, "来碗海龟汤 ") || strings.HasPrefix(text, "来碗海龟汤，") || strings.HasPrefix(text, "来碗海龟汤：") {
		return "start", strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, "来碗海龟汤"), "，： "))
	}
	commands := map[string]string{"提示": "hint", "看汤面": "surface", "看汤底": "reveal", "换一碗": "change", "确认换汤": "confirm-change", "不玩了": "end", "继续": "resume"}
	if command := commands[text]; command != "" {
		return command, ""
	}
	return "question", ""
}
func (s *Server) soupCandidate(m Message) bool {
	st := s.store.snapshot()
	if m.Self || m.Source != "wechat" || !slices.Contains([]string{"text", "quote"}, m.Kind) || !businessAllowed(st, "turtlesoup", m.Target) || strings.HasPrefix(m.Status, "revoked") {
		return false
	}
	if m.SenderID == "" || m.SenderID == m.Target && memoryGroup(st, m.Target) {
		return false
	}
	command, _ := soupCommand(soupText(st, m))
	if command == "start" {
		if _, e := s.livePlay(st.Account.ID, m.Target); e == nil {
			return false
		}
		return m.Rich == nil && soupStartAllowed(st, m)
	} // 引用触发词不会开新局。
	_, err := s.liveSoup(st.Account.ID, m.Target)
	if err != nil {
		var n int
		if s.store.db.QueryRow("SELECT count(*) FROM soup_turns WHERE account=? AND target=? AND status IN ('queued','running')", st.Account.ID, m.Target).Scan(&n) != nil || n == 0 {
			return false
		}
	}
	if command != "question" {
		return true
	}
	if !memoryGroup(st, m.Target) || settingText(st, "turtlesoup.group_mode") != "direct" {
		return true
	}
	if slices.Contains(m.Mentions, st.Account.ID) || strings.HasPrefix(strings.TrimSpace(m.Content), "汤问") {
		return true
	}
	if m.Rich != nil && !m.Rich.QuoteUnavailable {
		for _, old := range st.Messages {
			if old.Target == m.Target && old.ID == m.Rich.QuoteID && old.Game != "" {
				return true
			}
		}
	}
	return false
}

func soupStartAllowed(st State, m Message) bool {
	return !memoryGroup(st, m.Target) || st.Settings["turtlesoup.group_start_mention"] == false || st.Account.ID != "" && slices.Contains(m.Mentions, st.Account.ID)
}
func (s *Server) enqueueSoup(m Message) error {
	s.soupMu.Lock()
	defer s.soupMu.Unlock()
	st := s.store.snapshot()
	command, theme := soupCommand(soupText(st, m))
	t := soupTurn{Epoch: st.AccountEpoch, ID: randomID(), Message: m, Command: command, Theme: theme, Status: "queued", Updated: time.Now().UTC().Format(time.RFC3339Nano)}
	if len([]rune(theme)) > 300 || len([]rune(m.Content)) > 3000 {
		t.Status = "failed"
		t.Error = "海龟汤主题最多 300 字，问题最多 3000 字；请缩短后重新发送"
	}
	if g, e := s.liveSoup(st.Account.ID, m.Target); e == nil {
		t.GameID = g.ID
	}
	var pending int
	if err := s.store.db.QueryRow("SELECT count(*) FROM soup_turns WHERE status IN ('queued','running')").Scan(&pending); err != nil {
		return err
	}
	if pending >= 64 {
		t.Status = "failed"
		t.Error = "海龟汤队列已满，请稍后在工作台重试"
		t.Retryable = true
	}
	raw, err := s.store.sealMemory(st.Account.ID, "soup-turn:"+t.ID, t)
	if err != nil {
		return err
	}
	_, err = s.store.db.Exec("INSERT OR IGNORE INTO soup_turns(id,account,target,message_id,status,updated,body) VALUES(?,?,?,?,?,?,?)", t.ID, st.Account.ID, m.Target, m.ID, t.Status, t.Updated, raw)
	if err != nil {
		return err
	}
	if t.Status == "queued" {
		s.startSoupWorkerLocked(st.Account.ID, st.AccountEpoch, m.Target)
	}
	s.notify()
	return nil
}
func (s *Server) startSoupWorkerLocked(account string, epoch uint64, target string) {
	if s.soupWorkers == nil {
		s.soupWorkers = map[string]bool{}
	}
	key := account + "\x00" + target
	if s.soupWorkers[key] || s.ctx.Err() != nil {
		return
	}
	s.soupWorkers[key] = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		for {
			s.soupMu.Lock()
			st := s.store.snapshot()
			var id string
			err := s.store.db.QueryRow("SELECT id FROM soup_turns WHERE account=? AND target=? AND status='queued' ORDER BY seq LIMIT 1", account, target).Scan(&id)
			if err != nil || s.ctx.Err() != nil || st.Account.ID != account || st.AccountEpoch != epoch {
				delete(s.soupWorkers, key)
				if err == nil && s.ctx.Err() == nil && st.Account.ID == account && st.AccountEpoch != epoch {
					s.startSoupWorkerLocked(account, st.AccountEpoch, target)
				}
				s.soupMu.Unlock()
				return
			}
			t, e := s.soupTurn(account, id)
			if e == nil && t.Epoch != epoch {
				t.Status = "failed"
				t.Retryable = true
				t.Error = "账号会话已更新，旧回合不会自动执行；可手动重试"
				_ = s.saveSoupTurn(account, t)
				s.soupMu.Unlock()
				continue
			}
			if e == nil {
				t.Status = "running"
				t.Retryable = false
				e = s.saveSoupTurn(account, t)
			}
			s.soupMu.Unlock()
			if e != nil {
				s.soupMu.Lock()
				delete(s.soupWorkers, key)
				s.soupMu.Unlock()
				return
			}
			s.runSoupTurn(account, epoch, t)
		}
	}()
}
func (s *Server) setSoupMessage(account, target, id, game string) error {
	return s.store.change(func(st *State) error {
		if st.Account.ID != account {
			return fmt.Errorf("账号已切换")
		}
		for i, m := range st.Messages {
			if m.Target == target && m.ID == id {
				st.Messages[i].Game = game
				return s.store.recordMemoryArchiveLocked(account, st.Messages[i], false)
			}
		}
		return fmt.Errorf("来源消息已不存在")
	})
}
func (s *Server) runSoupTurn(account string, epoch uint64, t soupTurn) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	err := s.processSoupTurn(ctx, account, epoch, &t)
	if err != nil {
		if t.Status == "sending" {
			t.Status = "unknown"
			t.Retryable = false
			t.Error = "未确认微信发送结果，请核对会话；不会自动重发"
		} else {
			t.Status = "failed"
			t.Retryable = true
			t.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				t.Error = "任务已中断，可重试当前回合"
			}
		}
	}
	if e := s.saveSoupTurn(account, t); e != nil {
		return
	}
	var unavailable soupNoPuzzle
	if errors.As(err, &unavailable) && !t.NoticeAttempted && s.soupTurnAllowed(account, epoch, t) == nil {
		t.NoticeAttempted = true
		if s.saveSoupTurn(account, t) == nil {
			_ = s.send(ctx, actionRequest{Target: t.Message.Target, Content: "这个主题暂时没出成功，固定题库也没有对应题目。可以说「换一碗」后换题，或在海龟汤工作台重试。"}, "plugin:turtlesoup")
		}
	}
	_ = s.store.change(func(st *State) error {
		if st.Account.ID == account {
			level := "INFO"
			msg := "海龟汤回合完成"
			if err != nil {
				level = "WARN"
				msg = "海龟汤回合未完成，可在插件工作台查看"
			}
			addLog(st, level, "turtlesoup", msg)
			st.Logs[len(st.Logs)-1].Target = t.Message.Target
		}
		return nil
	})
}
func (s *Server) soupTurnAllowed(account string, epoch uint64, t soupTurn) error {
	st := s.store.snapshot()
	if st.Account.ID != account || st.AccountEpoch != epoch || st.Account.Status != "online" || !businessAllowed(st, "turtlesoup", t.Message.Target) {
		return fmt.Errorf("账号或插件生效范围已变化")
	}
	if command, _ := soupCommand(soupText(st, t.Message)); command == "start" && !soupStartAllowed(st, t.Message) {
		return fmt.Errorf("群聊开局需要真实 @ 当前微信账号")
	}
	for _, m := range st.Messages {
		if m.ID == t.Message.ID && m.Target == t.Message.Target && m.Content == t.Message.Content && !strings.HasPrefix(m.Status, "revoked") {
			return nil
		}
	}
	return fmt.Errorf("原始消息已删除或撤回")
}
func (s *Server) soupSelf() string {
	st := s.store.snapshot()
	name := settingText(st, "turtlesoup.self_name")
	if name == "" {
		name = st.Account.Name
	}
	if name == "" {
		name = "我"
	}
	return name
}
func (s *Server) soupOpening(g soupGame) string {
	return "🍲 " + s.soupSelf() + "给你端来一碗海龟汤。\n《" + g.Puzzle.Title + "》 · " + g.Puzzle.Theme + " · " + g.Puzzle.Difficulty + "\n\n【汤面】\n" + g.Puzzle.Surface + "\n\n直接提问或猜答案。发送「提示」「看汤面」「看汤底」「换一碗」「不玩了」。群聊揭晓和换题由开局者操作。"
}
func (s *Server) soupReply(ctx context.Context, account string, epoch uint64, t *soupTurn, text string) error {
	if err := s.soupTurnAllowed(account, epoch, *t); err != nil {
		return err
	}
	if t.PreparedRevision > 0 {
		g, err := s.soupGame(account, t.GameID)
		if err != nil || g.Revision != t.PreparedRevision {
			return fmt.Errorf("对局已变化，旧回复未发送")
		}
	}
	t.Output = text
	t.Status = "sending"
	t.Retryable = false
	if err := s.saveSoupTurn(account, *t); err != nil {
		return err
	}
	q := actionRequest{Target: t.Message.Target, Content: text}
	if strings.HasSuffix(q.Target, "@chatroom") && t.Message.SenderID != "" {
		name := strings.Join(strings.Fields(t.Message.Sender), " ")
		if name == "" {
			name = "玩家"
		}
		q.Content = "@" + name + "\u2005" + q.Content
		q.Mentions = []string{t.Message.SenderID}
	}
	if err := s.send(ctx, q, "plugin:turtlesoup"); err != nil {
		return err
	}
	t.Status = "completed"
	return nil
}
func (s *Server) soupEffectReply(ctx context.Context, account string, epoch uint64, g *soupGame, t *soupTurn, text string) error {
	if err := s.prepareSoupEffect(account, g, t, text); err != nil {
		return err
	}
	return s.soupReply(ctx, account, epoch, t, text)
}
func (s *Server) processSoupTurn(ctx context.Context, account string, epoch uint64, t *soupTurn) error {
	if err := s.soupTurnAllowed(account, epoch, *t); err != nil {
		return err
	}
	if t.Output != "" {
		return s.soupReply(ctx, account, epoch, t, t.Output)
	}
	g, err := s.liveSoup(account, t.Message.Target)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if t.GameID != "" && g.ID != "" && t.GameID != g.ID {
		return fmt.Errorf("原对局已结束，不能将旧问题带入新局")
	}
	if t.Command == "start" && g.ID != "" && g.Puzzle.Surface != "" && g.Status != "generating" && g.Status != "failed" {
		t.GameID = g.ID
		return s.soupReply(ctx, account, epoch, t, s.soupOpening(g))
	}
	if g.ID == "" && t.Command != "start" {
		return s.soupReply(ctx, account, epoch, t, "本局已结束，发送「来碗海龟汤」开始新游戏。")
	}
	if g.ID != "" {
		t.GameID = g.ID
		if t.Theme == "" {
			t.Theme = g.Theme
		}
		if memoryGroup(s.store.snapshot(), g.Target) && slices.Contains([]string{"reveal", "end", "change", "confirm-change"}, t.Command) && t.Message.SenderID != g.Owner {
			return s.soupReply(ctx, account, epoch, t, "这项操作请由开局者发起，其他玩家可以继续提问或获取提示。")
		}
		if g.Status == "playing" {
			stamp, _ := time.Parse(time.RFC3339Nano, g.Updated)
			minutes := s.number("turtlesoup.idle_minutes", 30)
			if minutes > 0 && time.Since(stamp) > time.Duration(minutes)*time.Minute {
				g.Status = "paused"
				if err = s.saveSoupGame(account, &g); err != nil {
					return err
				}
			}
		}
	}
	if t.Command == "change" {
		g.ChangePending = true
		return s.soupEffectReply(ctx, account, epoch, &g, t, "换题会结束当前这碗汤，回复「确认换汤」继续；不确认则保留当前进度。")
	}
	if t.Command == "confirm-change" && !g.ChangePending {
		return s.soupReply(ctx, account, epoch, t, "还没有换题请求。想换题请先说「换一碗」。")
	}
	if t.Command == "start" || t.Command == "confirm-change" {
		if g.ID != "" && g.Puzzle.Surface == "" && t.Command == "start" {
			if memoryGroup(s.store.snapshot(), g.Target) && t.Message.SenderID != g.Owner {
				return s.soupReply(ctx, account, epoch, t, "请由开局者重新发起出题。")
			}
			if t.Theme != "" {
				g.Theme = t.Theme
			}
			g.Status = "generating"
			if err = s.saveSoupGame(account, &g); err != nil {
				return err
			}
		}
		if t.Command == "confirm-change" {
			g.Status = "ended"
			if err = s.saveSoupGame(account, &g); err != nil {
				return err
			}
			g = soupGame{}
		}
		if g.ID == "" {
			g = soupGame{ID: randomID(), Target: t.Message.Target, Owner: t.Message.SenderID, Theme: t.Theme, Status: "generating", Created: time.Now().UTC().Format(time.RFC3339Nano)}
			if err = s.reserveSoupGame(account, &g); err != nil {
				return err
			}
		}
		t.GameID = g.ID
		t.Command = "start"
		if err = s.saveSoupTurn(account, *t); err != nil {
			return err
		}
		text, modelErr := s.soupModel(ctx, account, t, g)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = s.soupTurnAllowed(account, epoch, *t); err != nil {
			return err
		}
		latest, e := s.soupGame(account, g.ID)
		if e != nil {
			return e
		}
		if latest.Revision != g.Revision {
			return fmt.Errorf("对局在出题期间已变化")
		}
		p := turtlesoup.Puzzle{}
		g.Source = "model"
		if modelErr == nil {
			modelErr = turtlesoup.Decode(text, &p)
		}
		if modelErr != nil {
			g.Source = "bank"
			p, err = s.soupFallback(account, g.Target, g.Theme)
			if err != nil {
				g.Status = "failed"
				_ = s.saveSoupGame(account, &g)
				return soupNoPuzzle{}
			}
		}
		if p.ID == "" {
			p.ID = randomID()
		}
		g.Puzzle = p
		g.Status = "playing"
		prefix := ""
		if modelErr != nil {
			prefix = "模型出题暂未成功，这次从固定题库为你选了一题。\n\n"
		}
		return s.soupEffectReply(ctx, account, epoch, &g, t, prefix+s.soupOpening(g))
	}
	if g.Status == "failed" || g.Status == "generating" {
		return s.soupReply(ctx, account, epoch, t, "这碗汤还没出成功。可以在插件工作台重试，或说「换一碗」。")
	}
	if g.Status == "paused" && t.Command == "question" {
		return s.soupReply(ctx, account, epoch, t, "这局已暂停，发送「继续」恢复；进度和题目都还在。")
	}
	switch t.Command {
	case "surface":
		return s.soupReply(ctx, account, epoch, t, s.soupOpening(g))
	case "resume":
		if g.Puzzle.Surface == "" {
			return s.soupReply(ctx, account, epoch, t, "题目尚未生成，请由开局者发送「来碗海龟汤」重新出题。")
		}
		g.Status = "playing"
		return s.soupEffectReply(ctx, account, epoch, &g, t, "继续这碗汤，进度已保留。\n\n"+g.Puzzle.Surface)
	case "hint":
		limit := min(len(g.Puzzle.Hints), int(s.number("turtlesoup.max_hints", 3)))
		if g.Hints >= limit {
			return s.soupReply(ctx, account, epoch, t, "这碗汤的提示已经用完了，可以继续猜，或由开局者说「看汤底」。")
		}
		text := fmt.Sprintf("提示 %d：%s", g.Hints+1, g.Puzzle.Hints[g.Hints])
		g.Hints++
		return s.soupEffectReply(ctx, account, epoch, &g, t, text)
	case "reveal", "end":
		text := "这局结束了，汤底没有公开。想继续玩可以说「来碗海龟汤」。"
		g.Status = "ended"
		if t.Command == "reveal" {
			g.Status = "revealed"
			text = "【汤底】\n" + g.Puzzle.Bottom + "\n\n想再来一碗，可以说「来碗海龟汤」。"
		}
		return s.soupEffectReply(ctx, account, epoch, &g, t, text)
	}
	text, err := s.soupModel(ctx, account, t, g)
	if err != nil {
		return err
	}
	if err = s.soupTurnAllowed(account, epoch, *t); err != nil {
		return err
	}
	var v turtlesoup.Verdict
	if err = turtlesoup.Decode(text, &v); err != nil {
		return err
	}
	if err = turtlesoup.ValidateVerdict(v, g.Puzzle); err != nil {
		return err
	}
	t.Verdict = &v
	if v.Intent == "unrelated" {
		if err = s.setSoupMessage(account, t.Message.Target, t.Message.ID, ""); err != nil {
			return err
		}
		m := t.Message
		m.Game = ""
		t.Status = "completed"
		t.Output = "非游戏消息，交回普通对话"
		_ = s.rememberExplicit(m)
		if !s.dispatchBusiness(m) {
			s.autoReply(m)
		}
		return nil
	}
	if v.Intent == "guess" {
		for _, id := range v.Facts {
			if !slices.Contains(g.Known, id) {
				g.Known = append(g.Known, id)
			}
		}
	}
	answer := turtlesoup.Answer(v)
	g.Questions++
	if turtlesoup.Solved(v, g.Puzzle) {
		g.Status = "solved"
		answer = "猜对了！\n\n【汤底】\n" + g.Puzzle.Bottom + "\n\n发送「来碗海龟汤」再玩一局。"
	} else if g.Questions%5 == 0 {
		answer = s.soupSelf() + "的判断是：" + answer
	}
	return s.soupEffectReply(ctx, account, epoch, &g, t, answer)
}

func (s *Server) soupFallback(account, target, theme string) (turtlesoup.Puzzle, error) {
	bank, err := s.soupPuzzles(account)
	if err != nil {
		return turtlesoup.Puzzle{}, err
	}
	// 仅剥离难度修饰；不能用一个泛化关键词替代玩家指定的完整题材。
	topic := strings.TrimSpace(theme)
	for _, suffix := range []string{"，难一点", ",难一点", " 难一点", "，简单一点", " 简单一点", "，中等", "，困难", "，简单"} {
		topic = strings.TrimSpace(strings.TrimSuffix(topic, suffix))
	}
	for _, p := range bank {
		if !p.Enabled || topic != "" && !strings.EqualFold(topic, p.Theme) {
			continue
		}
		var played int
		if err = s.store.db.QueryRow("SELECT count(*) FROM soup_played WHERE account=? AND target=? AND puzzle_id=?", account, target, p.ID).Scan(&played); err != nil {
			return turtlesoup.Puzzle{}, err
		}
		if played == 0 {
			return p, nil
		}
	}
	return turtlesoup.Puzzle{}, fmt.Errorf("没有未玩过且主题匹配的题目")
}

package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/we-clay/we-clay/plugins/playroom"
	"regexp"
	"slices"
	"strings"
	"time"
)

var playReportRefs = regexp.MustCompile(`\[来源:([^\]\s]+)\]`)

var fortuneNumber = regexp.MustCompile(`抽签编号\s*[：:]?\s*\d+|\s*·\s*第\d+签`)

// 发送时也清理旧的每日缓存和待发送内容，保留同日同签的结果。
func fortuneReply(text string) string {
	labels := strings.NewReplacer("【互动插件 · fortune】", "", "（仅供娱乐）", "", "(仅供娱乐)", "", "【仅供娱乐·虚构签文】", "", "（北京时间）", "", "(北京时间)", "")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(fortuneNumber.ReplaceAllString(labels.Replace(raw), ""))
		if line == "" && strings.TrimSpace(raw) != "" || line == "签名" || strings.Contains(line, "签文为娱乐向文字游戏") || strings.Contains(line, "不构成任何吉凶判断") || strings.Contains(line, "签只是个陪聊的话头") || strings.Contains(line, "小提醒") && (strings.HasSuffix(line, "：") || strings.HasSuffix(line, ":")) {
			continue
		}
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func fortuneDrawReply(draw int, text string) string {
	text = fortuneReply(text)
	if draw < 1 || draw > 100 || text == "" {
		return text
	}
	first, _, _ := strings.Cut(text, "\n")
	_, dateErr := time.Parse("2006-01-02", first)
	if dateErr == nil || strings.HasPrefix(first, "日期") || strings.HasPrefix(first, "▍") {
		return fmt.Sprintf("第%d签\n%s", draw, text)
	}
	return fmt.Sprintf("第%d签 · %s", draw, text)
}

func reportSourceIDs(text string) []string {
	var ids []string
	for _, m := range playReportRefs.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(ids, m[1]) {
			ids = append(ids, m[1])
		}
	}
	return ids
}
func (s *Server) playSourceMessage(account, target, id string) (Message, error) {
	var raw []byte
	var hash string
	var m Message
	e := s.store.db.QueryRow("SELECT event_hash,body FROM memory_archive_events WHERE account=? AND target=? AND message_id=? ORDER BY seq DESC LIMIT 1", account, target, id).Scan(&hash, &raw)
	if e == nil {
		e = s.store.openMemory(account, "archive:"+target+":"+id+":"+hash, raw, &m)
	}
	if e != nil {
		return m, e
	}
	s.store.mu.Lock()
	deleted, e := s.store.memoryDeletedLocked(account, target, id)
	if e == nil && !deleted {
		deleted, e = s.store.memoryRevokedLocked(account, target, id)
	}
	s.store.mu.Unlock()
	if e != nil {
		return m, e
	}
	if deleted || !contextAvailable(m) || strings.HasPrefix(m.Game, "private:") {
		return m, fmt.Errorf("来源已撤回、删除或不可用")
	}
	return m, nil
}
func (s *Server) playReportMessages(account string, t playTurn) ([]Message, error) {
	if len(t.Sources) > 0 {
		return t.Sources, nil
	}
	now := time.Now().In(time.FixedZone("北京时间", 8*3600))
	if created, e := time.Parse(time.RFC3339Nano, t.Created); e == nil {
		now = created.In(now.Location())
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if strings.Contains(t.Text, "周报") || strings.Contains(t.Message.Content, "周报") {
		start = start.AddDate(0, 0, -6)
	}
	var upper int64
	if e := s.store.db.QueryRow("SELECT COALESCE(MAX(seq),0) FROM memory_archive_events WHERE account=? AND target=?", account, t.Target).Scan(&upper); e != nil {
		return nil, e
	}
	out := []Message{}
	limit := int(s.number("group_digest.max_messages", 1000))
	scanned := 0
	cursor := int64(0)
	for cursor < upper {
		batch, e := s.store.memoryArchiveBatchLocked(account, t.Target, cursor, upper)
		if e != nil {
			return nil, e
		}
		if len(batch) == 0 {
			break
		}
		for _, event := range batch {
			m := event.Message
			cursor = event.Seq
			scanned++
			if scanned > 200000 {
				return nil, fmt.Errorf("会话归档超过本次扫描上限，请缩小范围或提高服务容量")
			}
			stamp, err := time.Parse(time.RFC3339, m.Time)
			if err != nil || stamp.Before(start) || stamp.After(now) || m.ID == t.Message.ID || m.Kind != "text" && m.Kind != "quote" || !contextAvailable(m) || strings.HasPrefix(m.Source, "plugin:playroom:") || strings.HasPrefix(m.Game, "private:") {
				continue
			}
			out = append(out, m)
			if len(out) > limit {
				return nil, fmt.Errorf("本次资料超过%d条上限，未静默删减；请提高插件消息上限或缩小范围", limit)
			}
		}
	}
	valid := out[:0]
	for _, m := range out {
		latest, e := s.playSourceMessage(account, t.Target, m.ID)
		if e != nil {
			continue
		}
		if replyEvidenceHash(latest) == replyEvidenceHash(m) {
			valid = append(valid, m)
		}
	}
	return valid, nil
}
func (s *Server) validatePlaySources(account string, t playTurn) error {
	for offset := 0; offset < len(t.Sources); offset += 400 {
		batch := t.Sources[offset:min(offset+400, len(t.Sources))]
		wanted := map[string]string{}
		marks := make([]string, len(batch))
		args := []any{account, t.Target}
		for i, m := range batch {
			wanted[m.ID] = replyEvidenceHash(m)
			marks[i] = "?"
			args = append(args, m.ID)
		}
		query := `SELECT e.message_id,e.event_hash,e.body FROM memory_archive_events e WHERE e.account=? AND e.target=? AND e.message_id IN (` + strings.Join(marks, ",") + `) AND NOT EXISTS (SELECT 1 FROM memory_archive_events n WHERE n.account=e.account AND n.target=e.target AND n.message_id=e.message_id AND n.seq>e.seq) AND NOT EXISTS (SELECT 1 FROM memory_revocations r WHERE r.account=e.account AND r.target=e.target AND r.message_id=e.message_id) AND NOT EXISTS (SELECT 1 FROM memory_deleted_sources d WHERE d.account=e.account AND d.target=e.target AND d.message_id=e.message_id)`
		rows, e := s.store.db.Query(query, args...)
		if e != nil {
			return e
		}
		valid := 0
		for rows.Next() {
			var id, hash string
			var raw []byte
			var m Message
			if e = rows.Scan(&id, &hash, &raw); e != nil {
				break
			}
			if e = s.store.openMemory(account, "archive:"+t.Target+":"+id+":"+hash, raw, &m); e != nil {
				break
			}
			if contextAvailable(m) && wanted[id] == replyEvidenceHash(m) {
				valid++
			}
		}
		if err := rows.Err(); e == nil {
			e = err
		}
		rows.Close()
		if e != nil {
			return e
		}
		if valid != len(wanted) {
			return fmt.Errorf("来源资料发生撤回、删除或内容变化，请重新生成")
		}
	}
	return nil
}
func (s *Server) playUtility(ctx context.Context, account string, epoch uint64, t *playTurn) error {
	st := s.store.snapshot()
	actor := playActor(st, t.Message)
	g := playroom.Game{Kind: t.Plugin, Theme: t.Text}
	out := []playroom.Delivery{}
	switch t.Plugin {
	case "fortune":
		stamp := time.Now()
		if created, e := time.Parse(time.RFC3339Nano, t.Created); e == nil {
			stamp = created
		}
		day := stamp.In(time.FixedZone("北京时间", 8*3600)).Format("2006-01-02")
		var text string
		var b []byte
		draw := 0
		e := s.store.db.QueryRow("SELECT body FROM play_daily WHERE account=? AND target=? AND actor=? AND day=?", account, t.Target, actor, day).Scan(&b)
		if e == nil {
			key := "play-daily:" + t.Target + ":" + actor + ":" + day
			var original string
			var cached playDailyText
			e = s.store.openMemory(account, key, b, &cached)
			if e == nil {
				text = cached.Text
				original = text
				draw = cached.Draw
			} else {
				// 旧缓存是带程序日期的字符串；新缓存用对象区分，避免误删模型原文的日期。
				e = s.store.openMemory(account, key, b, &text)
				if e == nil {
					original = text
					text = fortuneReply(text)
					if first, rest, ok := strings.Cut(text, "\n"); ok && first == day {
						text = strings.TrimSpace(rest)
					}
				}
			}
			if e == nil && (draw < 1 || draw > 100) {
				draw, e = s.cachedFortuneDraw(account, t.Target, actor, day, original)
				if e == nil && draw > 0 {
					b, e = s.store.sealMemory(account, key, playDailyText{Text: text, Draw: draw})
					if e == nil {
						_, e = s.store.db.Exec("UPDATE play_daily SET body=? WHERE account=? AND target=? AND actor=? AND day=?", b, account, t.Target, actor, day)
					}
				}
			}
		} else if errors.Is(e, sql.ErrNoRows) {
			n := t.Draw
			if n == 0 {
				n = playroom.Roll(100)
			}
			draw = n
			p, err := s.playModel(ctx, account, t, g, "generate", fmt.Sprintf("抽签编号%d；北京时间日期%s。同一天同一玩家在同一会话只抽一次。", n, day))
			if err != nil {
				return err
			}
			text = p.Title + "\n" + p.Text
			b, e = s.store.sealMemory(account, "play-daily:"+t.Target+":"+actor+":"+day, playDailyText{Text: text, Draw: draw})
			if e == nil {
				_, e = s.store.db.Exec("INSERT INTO play_daily VALUES(?,?,?,?,?)", account, t.Target, actor, day, b)
			}
		}
		if e != nil {
			return e
		}
		out = []playroom.Delivery{playroom.Public(fortuneDrawReply(draw, text))}
	case "group_digest":
		if !memoryGroup(st, t.Target) {
			return fmt.Errorf("日报仅处理当前群聊")
		}
		sources, e := s.playReportMessages(account, *t)
		if e != nil {
			return e
		}
		if len(sources) == 0 {
			out = []playroom.Delivery{playroom.Public("所选北京时间范围内没有可总结的有效文字消息。")}
			break
		}
		t.Sources = sources
		if e = s.savePlayTurn(account, *t); e != nil {
			return e
		}
		var b strings.Builder
		fmt.Fprintf(&b, "资料范围：本群%s，消息%d条。引用严格使用[来源:消息ID]。\n", map[bool]string{true: "最近七个北京时间自然日", false: "北京时间今天"}[strings.Contains(t.Message.Content, "周报")], len(sources))
		for _, m := range sources {
			label := ""
			if m.Game != "" {
				label = "〔游戏过程，不能当现实事实〕"
			}
			text := strings.ReplaceAll(m.Content, "\n", "\n  ")
			fmt.Fprintf(&b, "[%s] %s %s%s：%s\n", m.ID, m.Time, m.Sender, label, text)
		}
		p, e := s.playModel(ctx, account, t, g, "report", b.String())
		if e != nil {
			return e
		}
		if e = s.validatePlaySources(account, *t); e != nil {
			return e
		}
		out = []playroom.Delivery{playroom.Public(p.Title + fmt.Sprintf("\n范围内有效资料：%d条\n", len(sources)) + p.Text)}
	case "group_memes":
		if !memoryGroup(st, t.Target) {
			return fmt.Errorf("群梗只处理当前群聊")
		}
		text := soupText(st, t.Message)
		if strings.HasPrefix(text, "查看群梗") || strings.HasPrefix(text, "随机群梗") {
			memes, e := s.playMemes(account, t.Target)
			if e != nil {
				return e
			}
			var usable []playMeme
			for _, m := range memes {
				if m.Status != "confirmed" {
					continue
				}
				source, e := s.playSourceMessage(account, t.Target, m.SourceID)
				if e == nil && replyEvidenceHash(source) == m.SourceHash {
					usable = append(usable, m)
				}
			}
			if len(usable) == 0 {
				out = []playroom.Delivery{playroom.Public("当前群还没有已确认且来源仍有效的梗。引用消息后说「收藏这个梗」可提交审核。")}
				break
			}
			if strings.HasPrefix(text, "随机群梗") {
				usable = []playMeme{usable[playroom.Roll(len(usable))-1]}
			}
			var b strings.Builder
			for i, m := range usable {
				if i >= 10 {
					break
				}
				fmt.Fprintf(&b, "%s\n原文：%s\n说明：%s\n[来源:%s]\n\n", m.ID, m.Text, m.Explanation, m.SourceID)
			}
			out = []playroom.Delivery{playroom.Public(b.String())}
			break
		}
		if t.Message.Rich == nil || t.Message.Rich.QuoteID == "" || t.Message.Rich.QuoteUnavailable {
			out = []playroom.Delivery{playroom.Public("请引用本群的一条有效消息，再发送「收藏这个梗」。")}
			break
		}
		source, e := s.playSourceMessage(account, t.Target, t.Message.Rich.QuoteID)
		if e != nil {
			return e
		}
		if source.Game != "" || strings.HasPrefix(source.Source, "plugin:") {
			return fmt.Errorf("请选择本群真实参与者的公开消息，不把生成内容再次收藏为事实")
		}
		var prior string
		e = s.store.db.QueryRow("SELECT id FROM play_memes WHERE account=? AND target=? AND source_id=?", account, t.Target, source.ID).Scan(&prior)
		if e == nil {
			out = []playroom.Delivery{playroom.Public("这条来源已经提交过，拒绝或删除后也不会自动重复提交。")}
			break
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		t.Sources = []Message{source}
		if e = s.savePlayTurn(account, *t); e != nil {
			return e
		}
		p, e := s.playModel(ctx, account, t, g, "report", "唯一来源："+source.Sender+"："+source.Content)
		if e != nil {
			return e
		}
		if e = s.validatePlaySources(account, *t); e != nil {
			return e
		}
		m := playMeme{ID: randomID(), Target: t.Target, SourceID: source.ID, SourceHash: replyEvidenceHash(source), Text: source.Content, Explanation: p.Title + "：" + p.Text, Status: "pending", Created: time.Now().Format(time.RFC3339)}
		if e = s.savePlayMeme(account, m); e != nil {
			return e
		}
		out = []playroom.Delivery{playroom.Public("已生成带来源的群梗候选，等待后台确认后才会用于查询和回顾。")}
	}
	if e := s.playAllowed(account, epoch, *t); e != nil {
		return e
	}
	if e := s.preparePlay(account, nil, t, out); e != nil {
		return e
	}
	return s.deliverPlay(ctx, account, epoch, t)
}

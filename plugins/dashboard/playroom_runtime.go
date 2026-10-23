package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/we-clay/we-clay/plugins/playroom"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type playRoute struct {
	Plugin, GameID, Target, Text string
	Private, Blocked             bool
}

func playSource(source string) bool { return strings.HasPrefix(source, "playroom-") }
func playActor(st State, m Message) string {
	if !memoryGroup(st, m.Target) && m.SenderID == "" {
		return m.Target
	}
	return m.SenderID
}
func playFriend(st State, id string) bool {
	return slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == id && c.Kind == "friend" })
}
func (s *Server) routePlay(m Message) (playRoute, bool) {
	st := s.store.snapshot()
	empty := playRoute{}
	if m.Self || m.Source != "wechat" || !slices.Contains([]string{"text", "quote"}, m.Kind) || strings.HasPrefix(m.Status, "revoked") {
		return empty, false
	}
	actor := playActor(st, m)
	if actor == "" || !memoryGroup(st, m.Target) && actor != m.Target || memoryGroup(st, m.Target) && actor == m.Target {
		return empty, false
	}
	text := soupText(st, m)
	group := memoryGroup(st, m.Target)
	if !group {
		parts := strings.Fields(text)
		if len(parts) >= 2 && (parts[0] == "确认入局" || parts[0] == "确认收件" || parts[0] == "游戏" || parts[0] == "加入") {
			g, e := s.playByCode(st.Account.ID, parts[1])
			if e == nil {
				member := g.Index(actor) >= 0
				canJoin := parts[0] == "加入" && g.Kind == "chemistry" && !memoryGroup(st, g.Target) && g.Status == "lobby"
				if !member && !canJoin {
					return empty, false
				}
				command := "确认入局"
				if parts[0] == "确认收件" && len(parts) == 3 {
					command = "确认收件 " + parts[2]
				}
				if parts[0] == "游戏" {
					command = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, parts[0]), " "+parts[1]))
					if len(parts) < 3 {
						command = "进度"
					}
				} else if canJoin {
					command = "加入"
				}
				return playRoute{g.Kind, g.ID, g.Target, command, true, !businessAllowed(st, g.Kind, g.Target)}, true
			}
		}
	}
	kind, theme := playroom.Match(text)
	if kind == "" {
		display := toolbox.CommandText(m.Content)
		kind, theme = playroom.Match(display)
	}
	if kind != "" && businessAllowed(st, kind, m.Target) {
		blocked := m.Rich != nil && kind != "group_memes" || group && st.Settings[kind+".group_mention_required"] != false && (st.Account.ID == "" || !slices.Contains(m.Mentions, st.Account.ID))
		return playRoute{kind, "", m.Target, theme, false, blocked}, true
	}
	if g, e := s.livePlay(st.Account.ID, m.Target); e == nil && businessAllowed(st, g.Kind, g.Target) {
		idx := g.Index(actor)
		if idx < 0 && text != "加入" {
			return empty, false
		}
		if group && !playroom.IsCommand(text) {
			return empty, false
		}
		if !group && slices.Contains([]string{"romance", "radio"}, g.Kind) {
			return playRoute{g.Kind, g.ID, g.Target, text, false, false}, true
		}
		if playroom.IsCommand(text) {
			return playRoute{g.Kind, g.ID, g.Target, text, !group && g.Kind == "chemistry", false}, true
		}
	}
	return empty, false
}
func (s *Server) enqueuePlay(m Message, route playRoute) error {
	if route.Blocked {
		return nil
	}
	s.playMu.Lock()
	defer s.playMu.Unlock()
	st := s.store.snapshot()
	t := playTurn{Created: time.Now().UTC().Format(time.RFC3339Nano), Draw: playroom.Roll(100), Start: route.GameID == "", ID: randomID(), GameID: route.GameID, Plugin: route.Plugin, Target: route.Target, Epoch: st.AccountEpoch, Message: m, Text: route.Text, Private: route.Private, Status: "queued"}
	if len([]rune(t.Text)) > 4000 {
		t.Status = "failed"
		t.Error = "本轮最多4000字，请缩短后重发"
	}
	var n int
	if e := s.store.db.QueryRow("SELECT count(*) FROM play_turns WHERE status IN ('queued','running')").Scan(&n); e != nil {
		return e
	}
	if n >= 128 {
		t.Status = "failed"
		t.Error = "互动队列已满，可稍后重试"
		t.Retryable = true
	}
	b, e := s.store.sealMemory(st.Account.ID, "play-turn:"+t.ID, t)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("INSERT OR IGNORE INTO play_turns(account,id,game_id,target,source_target,message_id,status,body) VALUES(?,?,?,?,?,?,?,?)", st.Account.ID, t.ID, t.GameID, t.Target, m.Target, m.ID, t.Status, b)
	if e == nil && t.Status == "queued" {
		s.startPlayWorkerLocked(st.Account.ID, st.AccountEpoch, t.Target)
	}
	s.notify()
	return e
}
func (s *Server) startPlayWorkerLocked(account string, epoch uint64, target string) {
	if s.playWorkers == nil {
		s.playWorkers = map[string]bool{}
	}
	key := account + "\x00" + target
	if s.playWorkers[key] || s.ctx.Err() != nil {
		return
	}
	s.playWorkers[key] = true
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer func() {
			s.playMu.Lock()
			delete(s.playWorkers, key)
			var first string
			_ = s.store.db.QueryRow("SELECT status FROM play_turns WHERE account=? AND target=? AND (status='queued' OR status IN ('failed','unknown') AND game_id<>'') ORDER BY seq LIMIT 1", account, target).Scan(&first)
			if first == "queued" && s.ctx.Err() == nil {
				s.startPlayWorkerLocked(account, s.store.snapshot().AccountEpoch, target)
			}
			s.playMu.Unlock()
		}()
		for {
			s.playMu.Lock()
			var id, status string
			e := s.store.db.QueryRow("SELECT id,status FROM play_turns WHERE account=? AND target=? AND (status='queued' OR status IN ('failed','unknown') AND game_id<>'') ORDER BY seq LIMIT 1", account, target).Scan(&id, &status)
			if e != nil || s.ctx.Err() != nil || status != "queued" {
				s.playMu.Unlock()
				return
			}
			t, e := s.playTurn(account, id)
			if e == nil {
				t.Status = "running"
				t.Retryable = false
				e = s.savePlayTurn(account, t)
			}
			s.playMu.Unlock()
			if e != nil {
				return
			}
			if !s.runPlayTurn(account, epoch, t) {
				return
			}
		}
	}()
}
func (s *Server) playAllowed(account string, epoch uint64, t playTurn) error {
	st := s.store.snapshot()
	if st.Account.ID != account || st.AccountEpoch != epoch || t.Epoch != epoch || st.Account.Status != "online" || !businessAllowed(st, t.Plugin, t.Target) {
		return fmt.Errorf("账号会话或插件生效范围已变化")
	}
	if t.Private && !playFriend(st, playActor(st, t.Message)) {
		return fmt.Errorf("秘密操作需要有效好友私聊")
	}

	found := false
	for _, m := range st.Messages {
		if m.ID == t.Message.ID && m.Target == t.Message.Target && m.Content == t.Message.Content && !strings.HasPrefix(m.Status, "revoked") && m.Status != "deleted" {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("来源消息已删除、撤回或变化")
	}
	if t.GameID != "" {
		g, e := s.playGame(account, t.GameID)
		if e != nil {
			return e
		}
		if g.Status == "paused" && !slices.Contains([]string{"继续游戏", "结束游戏", "进度", "怎么玩"}, t.Text) {
			return fmt.Errorf("对局已暂停")
		}
		if t.PreparedRevision > 0 && g.Revision != t.PreparedRevision {
			return fmt.Errorf("对局已变化，旧回复停止发送")
		}
	}
	return nil
}
func (s *Server) runPlayTurn(account string, epoch uint64, t playTurn) bool {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	e := s.processPlay(ctx, account, epoch, &t)
	if e != nil {
		if latest, err := s.playTurn(account, t.ID); err == nil && latest.Status == "cancelled" {
			return false
		}
		if t.GameID != "" {
			if g, err := s.playGame(account, t.GameID); err == nil && g.Status == "ended" && (t.PreparedRevision == 0 || g.Revision != t.PreparedRevision) {
				t.Status = "cancelled"
				t.Error = "管理员已结束本局，未确认的发送请核对微信"
				t.Retryable = false
				_ = s.savePlayTurn(account, t)
				return false
			}
		}
		t.Error = e.Error()
		if t.Status == "sending" {
			t.Status = "unknown"
			t.Retryable = false
			t.Error = "发送结果未确认，请人工核对微信，不能直接重发"
		} else {
			t.Status = "failed"
			t.Retryable = true
		}
		_ = s.savePlayTurn(account, t)
		_ = s.store.change(func(st *State) error {
			if st.Account.ID == account {
				addLog(st, "WARN", "playroom", "互动插件回合失败，可在插件工作台查看并重试")
			}
			return nil
		})
		return false
	}
	t.Status = "completed"
	t.Retryable = false
	t.Error = ""
	return s.savePlayTurn(account, t) == nil
}
func (s *Server) deliverPlay(ctx context.Context, account string, epoch uint64, t *playTurn) error {
	for t.Cursor < len(t.Deliveries) {
		if e := s.validatePlaySources(account, *t); e != nil {
			return e
		}
		if e := s.playAllowed(account, epoch, *t); e != nil {
			return e
		}
		d := &t.Deliveries[t.Cursor]
		if d.Sent {
			t.Cursor++
			continue
		}
		target := d.Target
		if target == "" {
			target = t.Target
		}
		if d.Private && !playFriend(s.store.snapshot(), target) {
			return fmt.Errorf("接收秘密信息的玩家已不是好友，发放已停止")
		}
		source := "plugin:playroom:" + t.Plugin
		if d.Private {
			source = "plugin:playroom-private:" + t.Plugin
		}
		text := compactPlayReply(d.Text, s.store.snapshot().Account.Name)
		if t.Plugin == "fortune" {
			text = fortuneReply(text)
		}
		if text == "" {
			return fmt.Errorf("回复清理后没有可发送的正文")
		}
		if t.GameID != "" {
			g, e := s.playGame(account, t.GameID)
			if e != nil {
				return e
			}
			if d.Private {
				header := "【" + playName(t.Plugin) + " · " + g.Code + "】"
				if !strings.HasPrefix(text, header) {
					text = header + "\n" + text
				}
				if !t.Private && !strings.Contains(text, "游戏 "+g.Code) {
					text += "\n操作：游戏 " + g.Code + " 操作内容"
				}
			}
		}
		t.Status = "sending"
		if e := s.savePlayTurn(account, *t); e != nil {
			return e
		}
		if e := s.send(ctx, actionRequest{Target: target, Content: text}, source); e != nil {
			return e
		}
		d.Sent = true
		t.Cursor++
		t.Status = "prepared"
		if e := s.savePlayTurn(account, *t); e != nil {
			return e
		}
	}
	return nil
}
func playName(kind string) string { d, _ := playroom.Find(kind); return d.Name }

var playSeats = regexp.MustCompile(`(?:^|\s)([0-9]{1,2})人(?:局)?`)

func (s *Server) processPlay(ctx context.Context, account string, epoch uint64, t *playTurn) error {
	if e := s.playAllowed(account, epoch, *t); e != nil {
		return e
	}
	if len(t.Deliveries) > 0 {
		return s.deliverPlay(ctx, account, epoch, t)
	}
	st := s.store.snapshot()
	actor := playActor(st, t.Message)
	in := playroom.Input{Actor: actor, Name: t.Message.Sender, Text: t.Text, Private: t.Private, Friend: playFriend(st, actor)}
	if slices.Contains([]string{"fortune", "group_digest", "group_memes"}, t.Plugin) {
		return s.playUtility(ctx, account, epoch, t)
	}
	var g playroom.Game
	var e error
	if t.GameID != "" {
		g, e = s.playGame(account, t.GameID)
	} else {
		g, e = s.livePlay(account, t.Target)
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	respond := func(text string) error {
		out := []playroom.Delivery{playroom.Public(text)}
		if t.Private {
			out = []playroom.Delivery{playroom.Secret(actor, text)}
		}
		if e := s.preparePlay(account, nil, t, out); e != nil {
			return e
		}
		return s.deliverPlay(ctx, account, epoch, t)
	}
	if t.GameID == "" && g.ID != "" {
		return respond("此会话已有「" + playName(g.Kind) + "」，请先结束；发送进度查看。")
	}
	if g.ID == "" {
		if old, e := s.liveSoup(account, t.Target); e == nil && old.ID != "" {
			return respond("本会话正在玩海龟汤，请先结束再开始其他游戏。")
		}
		d, _ := playroom.Find(t.Plugin)
		if d.GroupOnly && !memoryGroup(st, t.Target) {
			return respond("这个玩法需要在群里发起。")
		}
		if slices.Contains([]string{"romance", "radio"}, t.Plugin) && memoryGroup(st, t.Target) {
			return respond("这个玩法仅在私聊中开启。")
		}
		id := randomID()
		g = playroom.Game{ID: id, Code: id[:8], Kind: t.Plugin, Target: t.Target, Owner: actor, Theme: t.Text, Status: "lobby", Players: []playroom.Player{{ID: actor, Name: t.Message.Sender, Alive: true, HP: 10, Ready: !d.Secret}}, Created: time.Now().UTC().Format(time.RFC3339Nano)}
		if match := playSeats.FindStringSubmatch(t.Text); len(match) > 1 {
			g.Seats, _ = strconv.Atoi(match[1])
			if g.Seats < d.Min || g.Seats > d.Max {
				return respond(fmt.Sprintf("人数范围是%d—%d人。", d.Min, d.Max))
			}
		}
		if !memoryGroup(st, t.Target) && d.Max == 1 {
			g.Players[0].Ready = true
		}
		if e = s.reservePlayGame(account, &g); e != nil {
			return respond(e.Error())
		}
		t.GameID = g.ID
		if e = s.savePlayTurn(account, *t); e != nil {
			return e
		}
		if d.Max == 1 || !memoryGroup(st, t.Target) && slices.Contains([]string{"dungeon", "trpg"}, t.Plugin) {
			g.Players[0].Ready = true
			in.Text = "开始游戏"
		} else {
			intro := g.Instructions()
			if g.Kind == "chemistry" && !memoryGroup(st, g.Target) {
				intro = "私聊默契局已建立。把邀请码「" + g.Code + "」分享给自愿参加的好友，请对方私聊「加入 " + g.Code + "」，再「确认入局 " + g.Code + "」。\n" + intro
			}
			if e = s.preparePlay(account, &g, t, []playroom.Delivery{playroom.Public(intro)}); e != nil {
				return e
			}
			return s.deliverPlay(ctx, account, epoch, t)
		}
	}
	t.GameID = g.ID
	if e = s.savePlayTurn(account, *t); e != nil {
		return e
	}
	if t.Text == "重试本轮" {
		return respond("请在插件工作台重试失败回合；未知发送须先核对，避免重复发放身份。")
	}
	if t.Start && g.Status == "lobby" {
		d, _ := playroom.Find(g.Kind)
		if d.Max == 1 || !memoryGroup(st, g.Target) && (g.Kind == "dungeon" || g.Kind == "trpg") {
			in.Text = "开始游戏"
		}
	}
	if in.Text == "开始游戏" && g.Status == "lobby" {
		d, _ := playroom.Find(g.Kind)
		if actor != g.Owner {
			return respond("请由房主开始游戏。")
		}
		if len(g.Players) < d.Min || g.Seats > 0 && len(g.Players) != g.Seats {
			return respond("报名人数尚未满足配置。")
		}
		for _, p := range g.Players {
			if !p.Ready {
				return respond(p.Name + "尚未完成好友私聊确认。")
			}
		}
		if slices.Contains([]string{"undercover", "murder", "chemistry", "personality", "dungeon", "trpg", "romance", "radio"}, g.Kind) {
			pack, e := s.playModel(ctx, account, t, g, "generate", "")
			if e != nil {
				return e
			}
			g.Pack = pack
		}
	}
	if slices.Contains([]string{"romance", "radio"}, g.Kind) && !slices.Contains([]string{"结束游戏", "暂停游戏", "继续游戏", "进度", "怎么玩"}, in.Text) {
		if g.Status == "paused" {
			return respond("剧情已暂停，请继续游戏。")
		}
		if in.Text != "开始游戏" {
			pack, e := s.playModel(ctx, account, t, g, "story", in.Text)
			if e != nil {
				return e
			}
			g.Pack = pack
			g.History = append(g.History, playroom.Line{Speaker: actor, Text: in.Text})
		}
		g.Status = "playing"
		g.Phase = "story"
		g.Round++
		g.History = append(g.History, playroom.Line{Speaker: "主持", Text: g.Pack.Text})
		if len(g.History) > 200 {
			g.History = g.History[len(g.History)-200:]
		} // 只保留游戏历史，模型预算仍按模型中心设置。
		text := g.Pack.Text
		for i, c := range g.Pack.Choices {
			text += fmt.Sprintf("\n%d. %s", i+1, c)
		}
		if e = s.preparePlay(account, &g, t, []playroom.Delivery{playroom.Public(text)}); e != nil {
			return e
		}
		return s.deliverPlay(ctx, account, epoch, t)
	}
	next, out, e := playroom.Apply(g, in)
	if e != nil {
		return respond(e.Error())
	}
	if e = s.playAllowed(account, epoch, *t); e != nil {
		return e
	}
	if e = s.preparePlay(account, &next, t, out); e != nil {
		return e
	}
	return s.deliverPlay(ctx, account, epoch, t)
}

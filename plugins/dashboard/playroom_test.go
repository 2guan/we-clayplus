// 仅使用虚构账号、模拟微信 Host 和本地 HTTP 模型，不操作真实微信。
package dashboard

import (
	"context"
	"fmt"
	"github.com/we-clay/we-clay/plugins/playroom"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func playFixture(t *testing.T, kind string, group bool) (*Server, Message, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		p := playMockPack(kind, 4)
		if kind == "group_digest" {
			p.Text = "大家讨论了周末出游。[来源:fixture-public-source]"
		}
		soupJSON(w, p)
	})
	m.ID = randomID()
	m.Content = ""
	if group {
		m.Target = "fixture-play-group@chatroom"
		m.SenderID = "fixture-play-p1"
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["reply.enabled"] = false
		st.Settings[kind+".scope"] = "whitelist"
		st.Settings[kind+".targets"] = m.Target
		for i, p := range st.Plugins {
			if p.ID == kind {
				st.Plugins[i].Enabled = true
			}
		}
		if group {
			st.Contacts = append(st.Contacts, Contact{ID: m.Target, Name: "虚构游戏群", Kind: "group"})
		}
		for i := 1; i <= 12; i++ {
			st.Contacts = append(st.Contacts, Contact{ID: fmt.Sprint("fixture-play-p", i), Name: fmt.Sprint("玩家", i), Kind: "friend"})
		}
		return nil
	}))
	return s, m, calls
}
func recordPlay(t *testing.T, s *Server, m Message, text string) playTurn {
	t.Helper()
	m.ID = randomID()
	m.Content = text
	if memoryGroup(s.store.snapshot(), m.Target) {
		m.Mentions = []string{s.store.snapshot().Account.ID}
		m.Content = "@本人\u2005" + text
	}
	replyRegressionCheck(t, s.Record(m))
	var turn playTurn
	waitBusiness(t, func() bool {
		turns, e := s.playTurns(s.store.snapshot().Account.ID, "")
		if e != nil {
			return false
		}
		for _, x := range turns {
			if x.Message.ID == m.ID {
				turn = x
				return slices.Contains([]string{"completed", "failed", "unknown"}, x.Status)
			}
		}
		return false
	})
	return turn
}
func waitPlayIdle(t *testing.T, s *Server, target string) {
	t.Helper()
	waitBusiness(t, func() bool {
		s.playMu.Lock()
		defer s.playMu.Unlock()
		return !s.playWorkers[s.store.snapshot().Account.ID+"\x00"+target]
	})
}
func TestPlayroomThirteenRegisteredDisabledAndAdminOnly(t *testing.T) {
	s := newServer(t, false)
	st := s.store.snapshot()
	if len(playroom.Definitions) != 13 {
		t.Fatal("定义数")
	}
	for _, d := range playroom.Definitions {
		if pluginEnabled(st, d.ID) {
			t.Fatal("真实模式默认启用", d.ID)
		}
		if !isBusinessPlugin(d.ID) || businessUsage(d.ID) == nil {
			t.Fatal("未注册", d.ID)
		}
	}
	if call(t, s, "/api/playroom", nil, nil, "").Code != 401 {
		t.Fatal("匿名读取秘密")
	}
	admin := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-play-admin", "password": "fixture-password-123"}, nil, "").Result().Cookies()[0]
	if call(t, s, "/api/playroom", map[string]any{"operation": "simulate", "target": "fake", "text": "开狼人杀"}, admin, "").Code != 400 {
		t.Fatal("真实模式接受了模拟")
	}
}
func TestPlayroomGroupMentionFriendHandshakeAndPrivateIsolation(t *testing.T) {
	s, m, calls := playFixture(t, "undercover", true)
	bad := m
	bad.Content = "玩谁是卧底 美食"
	bad.ID = randomID()
	replyRegressionCheck(t, s.Record(bad))
	if games, _ := s.playGames(s.store.snapshot().Account.ID, "", ""); len(games) > 0 {
		t.Fatal("无真实@开局")
	}
	turn := recordPlay(t, s, m, "玩谁是卧底 美食")
	if turn.Status != "completed" {
		t.Fatal(turn.Error)
	}
	g, e := s.livePlay(s.store.snapshot().Account.ID, m.Target)
	replyRegressionCheck(t, e)
	for i := 2; i <= 4; i++ {
		p := m
		p.SenderID = fmt.Sprint("fixture-play-p", i)
		p.Sender = fmt.Sprint("玩家", i)
		if tr := recordPlay(t, s, p, "加入"); tr.Status != "completed" {
			t.Fatal(tr.Error)
		}
	}
	recordPlay(t, s, m, "开始游戏")
	g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
	if g.Status != "lobby" || calls.Load() != 0 {
		t.Fatal("未确认却开始出题")
	}
	for i := 1; i <= 4; i++ {
		p := m
		p.Target = fmt.Sprint("fixture-play-p", i)
		p.SenderID = p.Target
		p.Sender = fmt.Sprint("玩家", i)
		tr := confirmPlay(t, s, p, g.Code)
		if tr.Status != "completed" {
			t.Fatal(tr.Error)
		}
	}
	tr := recordPlay(t, s, m, "开始游戏")
	if tr.Status != "completed" {
		t.Fatal(tr.Error)
	}
	g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
	if g.Status != "playing" || calls.Load() != 1 {
		t.Fatal("未正确开局", tr.Error)
	}
	for _, msg := range s.store.snapshot().Messages {
		if msg.Self {
			header := "【" + playName(g.Kind) + " · " + g.Code + "】"
			if strings.Count(msg.Content, header) > 1 || strings.Contains(msg.Content, "【互动插件 ·") {
				t.Fatal("回复含重复插件标题")
			}
			if strings.HasPrefix(msg.Game, "private:") {
				if !strings.HasPrefix(msg.Content, header) {
					t.Fatal("私密消息丢失对局标识")
				}
				if strings.Contains(msg.Content, "你的词语：") && !strings.Contains(msg.Content, "操作：游戏 "+g.Code) {
					t.Fatal("身份发放丢失操作提示")
				}
				if strings.Contains(msg.Content, "等待房主开局") && strings.Contains(msg.Content, "操作内容") {
					t.Fatal("私聊回执重复附带操作格式")
				}
			}
		}
		if strings.HasPrefix(msg.Game, "private:") {
			if contextAvailable(msg) || memoryMessageAvailable(msg) {
				t.Fatal("秘密消息进入普通上下文或记忆")
			}
		}
		if msg.Target == m.Target && (strings.Contains(msg.Content, "苹果") || strings.Contains(msg.Content, "梨")) {
			t.Fatal("群内泄词", msg.Content)
		}
	}
	for _, task := range s.store.snapshot().Tasks {
		if playSource(task.Source) && (strings.Contains(task.Input, "苹果") || strings.Contains(task.Result, "苹果")) {
			t.Fatal("任务列表泄词")
		}
	}
}
func TestPlayroomSoloPluginsAndDailyCache(t *testing.T) {
	for _, kind := range []string{"romance", "radio", "dungeon", "trpg", "fortune", "personality"} {
		t.Run(kind, func(t *testing.T) {
			s, m, calls := playFixture(t, kind, false)
			d, _ := playroom.Find(kind)
			tr := recordPlay(t, s, m, d.Trigger+" 演示主题")
			if tr.Status != "completed" {
				t.Fatal(tr.Error)
			}
			if calls.Load() != 1 {
				t.Fatal("模型未调用", calls.Load())
			}
			for _, msg := range s.store.snapshot().Messages {
				if msg.Self && (strings.Contains(msg.Content, "【互动插件 ·") || strings.HasPrefix(msg.Content, "【"+playName(kind)+" ·")) {
					t.Fatal("公开回复仍带统一插件前缀", kind)
				}
			}
			if kind == "fortune" {
				pack := playMockPack(kind, 1)
				for _, msg := range s.store.snapshot().Messages {
					if msg.Self && strings.Contains(msg.Game, "fortune") && msg.Content != fmt.Sprintf("第%d签 · %s\n%s", tr.Draw, pack.Title, pack.Text) {
						t.Fatal("签号或模型签文格式不正确")
					}
				}
				second := recordPlay(t, s, m, "抽个今日签")
				if second.Status != "completed" || calls.Load() != 1 || second.Deliveries[0].Text != tr.Deliveries[0].Text {
					t.Fatal("抽签未缓存", second.Error)
				}
				return
			}
			g, e := s.livePlay(s.store.snapshot().Account.ID, m.Target)
			replyRegressionCheck(t, e)
			if g.Status != "playing" {
				t.Fatal(g.Status)
			}
			if kind == "romance" || kind == "radio" {
				second := recordPlay(t, s, m, "今天有点累")
				if second.Status != "completed" || calls.Load() != 2 {
					t.Fatal(second.Error)
				}
				g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
				if len(g.History) < 3 {
					t.Fatal("未存剧情历史")
				}
			}
			if kind == "personality" {
				for i := 0; i < 5; i++ {
					if tr := recordPlay(t, s, m, "答 D"); tr.Status != "completed" {
						t.Fatal(tr.Error)
					}
				}
				g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
				if g.Status != "ended" {
					t.Fatal("问卷未完成")
				}
			}
		})
	}
}
func TestPlayroomReportSourceAndMemeReview(t *testing.T) {
	for _, kind := range []string{"group_digest", "group_memes"} {
		t.Run(kind, func(t *testing.T) {
			s, m, calls := playFixture(t, kind, true)
			source := m
			source.ID = "fixture-public-source"
			source.Content = "周末一起去公园，周六九点集合。"
			source.Mentions = nil
			replyRegressionCheck(t, s.Record(source))
			if kind == "group_digest" {
				tr := recordPlay(t, s, m, "生成今天的群日报")
				if tr.Status != "completed" || len(tr.Sources) != 1 {
					t.Fatal(tr.Error, len(tr.Sources))
				}
				if !strings.Contains(tr.Deliveries[0].Text, "[来源:fixture-public-source]") {
					t.Fatal("没有来源")
				}
				return
			}
			m.Kind = "quote"
			m.Rich = &RichMessage{QuoteID: source.ID} // 引用命令允许收藏，但不能用引用触发其他游戏。
			tr := recordPlay(t, s, m, "收藏这个梗")
			if tr.Status != "completed" {
				t.Fatal(tr.Error)
			}
			memes, e := s.playMemes(s.store.snapshot().Account.ID, m.Target)
			replyRegressionCheck(t, e)
			if len(memes) != 1 || memes[0].Status != "pending" {
				t.Fatal("候选缺失", tr)
			}
			m.Kind = "text"
			m.Rich = nil
			tr = recordPlay(t, s, m, "查看群梗")
			if !strings.Contains(tr.Deliveries[0].Text, "还没有已确认") {
				t.Fatal("候选提前公开")
			}
			admin := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-play-admin", "password": "fixture-password-123"}, nil, "").Result().Cookies()[0]
			resp := call(t, s, "/api/playroom", map[string]any{"operation": "meme-confirm", "id": memes[0].ID}, admin, "")
			if resp.Code != 200 {
				t.Fatal(resp.Body.String())
			}
			tr = recordPlay(t, s, m, "随机群梗")
			if !strings.Contains(tr.Deliveries[0].Text, source.Content) {
				t.Fatal("确认后不可用")
			}
			if calls.Load() != 1 {
				t.Fatal("重复查梗调用模型")
			}
			source.Status = "revoked_by_other"
			replyRegressionCheck(t, s.recordMemoryArchive(s.store.snapshot().Account.ID, source))
			tr = recordPlay(t, s, m, "查看群梗")
			if strings.Contains(tr.Deliveries[0].Text, source.Content) {
				t.Fatal("撤回来源仍公开")
			}
		})
	}
}
func TestPlayroomUnknownDeliveryAndRecovery(t *testing.T) {
	s, m, _ := playFixture(t, "undercover", true)
	recordPlay(t, s, m, "玩谁是卧底")
	g, _ := s.livePlay(s.store.snapshot().Account.ID, m.Target)
	for i := 2; i <= 4; i++ {
		p := m
		p.SenderID = fmt.Sprint("fixture-play-p", i)
		recordPlay(t, s, p, "加入")
	}
	for i := 1; i <= 4; i++ {
		p := m
		p.Target = fmt.Sprint("fixture-play-p", i)
		p.SenderID = p.Target
		confirmPlay(t, s, p, g.Code)
	}
	var sends atomic.Int32
	s.host = replyRegressionHost(func(context.Context, control.Command) (any, error) {
		if sends.Add(1) == 2 {
			return nil, fmt.Errorf("fixture-unknown-send")
		}
		return map[string]string{"id": randomID()}, nil
	})
	tr := recordPlay(t, s, m, "开始游戏")
	if tr.Status != "unknown" || tr.Retryable || tr.Cursor != 1 {
		t.Fatal("未知发送被当作可重试", tr.Status, tr.Cursor)
	}
	waitPlayIdle(t, s, m.Target)
	g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
	g.Status = "paused"
	replyRegressionCheck(t, s.controlPlayGame(s.store.snapshot().Account.ID, &g))
	g.Status = "playing"
	replyRegressionCheck(t, s.controlPlayGame(s.store.snapshot().Account.ID, &g))
	before := sends.Load()
	replyRegressionCheck(t, s.initPlayroom())
	if sends.Load() != before {
		t.Fatal("恢复自动重发")
	}
	got, _ := s.playTurn(s.store.snapshot().Account.ID, tr.ID)
	if got.Status != "unknown" {
		t.Fatal(got.Status)
	}
	admin := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-play-admin", "password": "fixture-password-123"}, nil, "").Result().Cookies()[0]
	if call(t, s, "/api/playroom", map[string]any{"operation": "retry", "id": tr.ID}, admin, "").Code != 400 {
		t.Fatal("未知发送允许直接重试")
	}
	resp := call(t, s, "/api/playroom", map[string]any{"operation": "confirm-sent", "id": tr.ID}, admin, "")
	if resp.Code != 200 {
		t.Fatal(resp.Body.String())
	}
	waitBusiness(t, func() bool { x, _ := s.playTurn(s.store.snapshot().Account.ID, tr.ID); return x.Status == "completed" })
	if sends.Load() != int32(len(tr.Deliveries)) {
		t.Fatal("重发了已经确认的身份", sends.Load(), len(tr.Deliveries))
	}
}
func TestPlayroomFailureRetriesWholePipelineAndKeepsSecrets(t *testing.T) {
	s, m, _ := playFixture(t, "radio", false)
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "fixture unavailable", 503)
			return
		}
		soupJSON(w, playMockPack("radio", 1))
	}))
	defer remote.Close()
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Models[0].Endpoint = remote.URL; return nil }))
	tr := recordPlay(t, s, m, "打开深夜电台 失眠")
	if tr.Status != "failed" || !tr.Retryable || calls.Load() != 2 {
		t.Fatal(tr.Status, tr.Error, calls.Load())
	}
	waitPlayIdle(t, s, m.Target)
	admin := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-play-admin", "password": "fixture-password-123"}, nil, "").Result().Cookies()[0]
	resp := call(t, s, "/api/playroom", map[string]any{"operation": "retry", "id": tr.ID}, admin, "")
	if resp.Code != 200 {
		t.Fatal(resp.Body.String())
	}
	waitBusiness(t, func() bool { x, _ := s.playTurn(s.store.snapshot().Account.ID, tr.ID); return x.Status == "completed" })
	if calls.Load() != 3 {
		t.Fatal("重试没有恢复生成", calls.Load())
	}
}
func TestPlayroomAccountSwitchWhileGenerating(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		soupJSON(w, playMockPack("radio", 1))
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["reply.enabled"] = false
		st.Settings["radio.scope"] = "whitelist"
		st.Settings["radio.targets"] = m.Target
		for i, p := range st.Plugins {
			if p.ID == "radio" {
				st.Plugins[i].Enabled = true
			}
		}
		return nil
	}))
	m.ID = randomID()
	m.Content = "打开深夜电台"
	replyRegressionCheck(t, s.Record(m))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("未开始")
	}
	old := s.store.snapshot().Account.ID
	replyRegressionCheck(t, s.store.switchAccount(Account{ID: "fixture-play-new-account", Status: "online"}))
	close(release)
	waitBusiness(t, func() bool { ts, _ := s.playTurns(old, ""); return len(ts) > 0 && ts[0].Status == "failed" })
	for _, msg := range s.store.snapshot().Messages {
		if strings.HasPrefix(msg.Source, "plugin:playroom") {
			t.Fatal("切换账号后仍发送")
		}
	}
}

func TestPlayroomOtherGroupGamesAndPublicAdventure(t *testing.T) {
	for _, kind := range []string{"werewolf", "avalon", "murder", "chemistry", "dungeon", "trpg"} {
		t.Run(kind, func(t *testing.T) {
			s, m, _ := playFixture(t, kind, true)
			d, _ := playroom.Find(kind)
			n := d.Min
			tr := recordPlay(t, s, m, d.Trigger)
			if tr.Status != "completed" {
				t.Fatal(tr.Error)
			}
			g, e := s.livePlay(s.store.snapshot().Account.ID, m.Target)
			replyRegressionCheck(t, e)
			for i := 2; i <= n; i++ {
				p := m
				p.SenderID = fmt.Sprint("fixture-play-p", i)
				recordPlay(t, s, p, "加入")
			}
			if d.Secret {
				for i := 1; i <= n; i++ {
					p := m
					p.Target = fmt.Sprint("fixture-play-p", i)
					p.SenderID = p.Target
					confirmPlay(t, s, p, g.Code)
				}
			}
			if kind == "murder" { // 该本地模型固定生成四人剧本。
				p := m
				p.SenderID = "fixture-play-p4"
				recordPlay(t, s, p, "加入")
				p.Target = p.SenderID
				confirmPlay(t, s, p, g.Code)
			}
			tr = recordPlay(t, s, m, "开始游戏")
			if tr.Status != "completed" {
				t.Fatal(tr.Error)
			}
			g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
			if g.Status != "playing" {
				t.Fatal(g.Kind, g.Status, tr.Deliveries)
			}
		})
	}
}
func TestPlayroomPrivateChemistryFanoutAndControlResume(t *testing.T) {
	s, m, _ := playFixture(t, "chemistry", false)
	recordPlay(t, s, m, "测默契 朋友")
	g, e := s.livePlay(s.store.snapshot().Account.ID, m.Target)
	replyRegressionCheck(t, e)
	other := m
	other.Target = "fixture-play-p2"
	other.SenderID = other.Target
	recordPlay(t, s, other, "加入 "+g.Code)
	confirmPlay(t, s, m, g.Code)
	confirmPlay(t, s, other, g.Code)
	tr := recordPlay(t, s, m, "开始游戏")
	if tr.Status != "completed" {
		t.Fatal(tr.Error)
	}
	seen := map[string]bool{}
	for _, d := range tr.Deliveries {
		if !d.Private {
			t.Fatal("私聊默契输出未通过秘密消息隔离")
		}
		seen[d.Target] = true
	}
	if !seen[m.Target] || !seen[other.Target] {
		t.Fatal("未向每个玩家发送问题", seen)
	}
	g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
	waitPlayIdle(t, s, m.Target)
	before := g.Revision
	g.Status = "paused"
	replyRegressionCheck(t, s.controlPlayGame(s.store.snapshot().Account.ID, &g))
	if g.Revision != before+1 {
		t.Fatal("暂停版本未变更")
	}
	g.Status = "playing"
	replyRegressionCheck(t, s.controlPlayGame(s.store.snapshot().Account.ID, &g))
	for i := 0; i < 5; i++ {
		recordPlay(t, s, m, "游戏 "+g.Code+" 答 A")
		tr = recordPlay(t, s, other, "游戏 "+g.Code+" 答 A")
		if tr.Status != "completed" {
			t.Fatal(tr.Error)
		}
	}
	g, _ = s.playGame(s.store.snapshot().Account.ID, g.ID)
	if g.Status != "ended" {
		t.Fatal("默契测试未结束")
	}
}

func confirmPlay(t *testing.T, s *Server, m Message, code string) playTurn {
	t.Helper()
	first := recordPlay(t, s, m, "确认入局 "+code)
	if first.Status != "completed" {
		t.Fatal(first.Error)
	}
	g, e := s.playByCode(s.store.snapshot().Account.ID, code)
	replyRegressionCheck(t, e)
	i := g.Index(playActor(s.store.snapshot(), m))
	if i < 0 || g.Players[i].Challenge == "" {
		t.Fatal("未发出收件码")
	}
	if g.Players[i].Ready {
		t.Fatal("未经收件码确认即就绪")
	}
	return recordPlay(t, s, m, "确认收件 "+code+" "+g.Players[i].Challenge)
}
func TestPlayroomSoupConcurrentReservation(t *testing.T) {
	s := newServer(t, true)
	account := s.store.snapshot().Account.ID
	var wins atomic.Int32
	done := make(chan struct{}, 2)
	go func() {
		g := playroom.Game{ID: randomID(), Code: "fixture-code", Kind: "radio", Target: "fixture-exclusive-target", Status: "lobby"}
		if s.reservePlayGame(account, &g) == nil {
			wins.Add(1)
		}
		done <- struct{}{}
	}()
	go func() {
		g := soupGame{ID: randomID(), Target: "fixture-exclusive-target", Status: "generating"}
		if s.reserveSoupGame(account, &g) == nil {
			wins.Add(1)
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	if wins.Load() != 1 {
		t.Fatal("海龟汤与互动局同时占用同一会话")
	}
}

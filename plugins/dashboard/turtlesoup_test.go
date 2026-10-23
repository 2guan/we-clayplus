// 使用虚构消息和本地模型服务验证游戏；不连接真实微信。
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/plugins/turtlesoup"
	"github.com/we-clay/we-clay/sdk/control"
)

func TestSoupAdminAPIAndAccountSwitch(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	s, m, sends := soupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		soupJSON(w, turtlesoup.Defaults[0])
	})
	if call(t, s, "/api/turtlesoup", nil, nil, "").Code != 401 {
		t.Fatal("匿名读取了汤底")
	}
	resp := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-soup-admin", "password": "fixture-password-123"}, nil, "")
	admin := resp.Result().Cookies()[0]
	resp = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-soup-operator", Password: "fixture-password-123", Role: "operator", Enabled: true}, admin, "")
	if resp.Code != 200 {
		t.Fatal(resp.Body.String())
	}
	resp = call(t, s, "/api/auth/login", map[string]string{"name": "fixture-soup-operator", "password": "fixture-password-123"}, nil, "")
	operator := resp.Result().Cookies()[0]
	for _, body := range []any{nil, map[string]string{"operation": "end", "id": "fake"}} {
		if call(t, s, "/api/turtlesoup", body, operator, "").Code != 403 {
			t.Fatal("非管理员操作了游戏或读取了汤底")
		}
	}
	resp = call(t, s, "/api/turtlesoup", map[string]any{"operation": "simulate", "target": m.Target, "text": "来碗海龟汤"}, admin, "")
	if resp.Code != 400 {
		t.Fatal("真实模式接受了模拟消息")
	}
	replyRegressionCheck(t, s.Record(m))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("模型未开始")
	}
	old := s.store.snapshot().Account.ID
	replyRegressionCheck(t, s.store.switchAccount(Account{ID: "fixture-soup-new-account", Name: "新账号", Status: "online"}))
	close(release)
	waitBusiness(t, func() bool {
		turns, e := s.soupTurns(old, m.Target)
		return e == nil && len(turns) > 0 && turns[0].Status == "failed"
	})
	if sends.Load() != 0 {
		t.Fatal("账号切换后发送旧账号结果")
	}
	g, e := s.liveSoup(old, m.Target)
	replyRegressionCheck(t, e)
	if g.Puzzle.Bottom != "" {
		t.Fatal("旧模型结果仍被应用")
	}
	if games, e := s.soupGames(s.store.snapshot().Account.ID, ""); e != nil || len(games) != 0 {
		t.Fatal("新账号读取了旧对局")
	}
}

func soupFixture(t *testing.T, handler http.HandlerFunc) (*Server, Message, *atomic.Int32) {
	t.Helper()
	s, m, sends := retryFixture(t, handler)
	m.ID = randomID()
	m.Content = "来碗海龟汤 职场"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.Name = "测试主持"
		st.Settings["reply.enabled"] = false
		st.Settings["turtlesoup.scope"] = "whitelist"
		st.Settings["turtlesoup.targets"] = m.Target
		st.Settings["turtlesoup.self_name"] = "小汤"
		for i, p := range st.Plugins {
			if p.ID == "turtlesoup" {
				st.Plugins[i].Enabled = true
			}
		}
		return nil
	}))
	return s, m, sends
}
func soupJSON(w http.ResponseWriter, value any) {
	raw, _ := json.Marshal(value)
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}})
}
func waitSoupTurn(t *testing.T, s *Server, m Message) soupTurn {
	t.Helper()
	var turn soupTurn
	waitBusiness(t, func() bool {
		all, e := s.soupTurns(s.store.snapshot().Account.ID, m.Target)
		if e != nil {
			return false
		}
		for _, x := range all {
			if x.Message.ID == m.ID {
				turn = x
				return slices.Contains([]string{"completed", "failed", "unknown"}, x.Status)
			}
		}
		return false
	})
	return turn
}
func soupRecord(t *testing.T, s *Server, m Message, text string) soupTurn {
	t.Helper()
	m.ID = randomID()
	m.Content = text
	replyRegressionCheck(t, s.Record(m))
	return waitSoupTurn(t, s, m)
}

func TestSoupModelFirstIdentityPrivacyAndGroupControl(t *testing.T) {
	var calls atomic.Int32
	s, m, sends := soupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls.Add(1)
		if strings.Contains(body.Messages[0].Content, "题目作者") {
			if !strings.Contains(body.Messages[len(body.Messages)-1].Content, "职场") {
				t.Error("未传主题")
			}
			soupJSON(w, turtlesoup.Defaults[0])
			return
		}
		if !strings.Contains(body.Messages[0].Content, "测试主持") || !strings.Contains(body.Messages[0].Content, "小汤") {
			t.Error("未传可信身份")
		}
		if strings.Contains(body.Messages[len(body.Messages)-1].Content, "fixture-soup-solution") {
			soupJSON(w, turtlesoup.Verdict{Intent: "guess", Answer: "yes", Solved: true, Facts: turtlesoup.Defaults[0].Required})
			return
		}
		soupJSON(w, turtlesoup.Verdict{Intent: "question", Answer: "no", Facts: []string{}})
	})
	replyRegressionCheck(t, s.Record(m))
	first := waitSoupTurn(t, s, m)
	if first.Status != "completed" || sends.Load() != 1 || calls.Load() != 1 || !strings.Contains(first.Output, "小汤") {
		t.Fatal(first, sends.Load(), calls.Load())
	}
	time.Sleep(200 * time.Millisecond)
	if sends.Load() != 1 {
		t.Fatal("主持消息触发了自己的游戏", sends.Load())
	}
	all, _ := s.soupTurns(s.store.snapshot().Account.ID, m.Target)
	if len(all) != 1 {
		t.Fatal("主持消息被作为玩家回合入队", len(all))
	}
	g, e := s.liveSoup(s.store.snapshot().Account.ID, m.Target)
	replyRegressionCheck(t, e)
	if g.Source != "model" {
		t.Fatal("没有优先模型出题")
	}
	question := soupRecord(t, s, m, "她是迟到了吗？")
	if question.Status != "completed" || question.Output != "不是。" {
		t.Fatal(question)
	}
	before := sends.Load()
	duplicate := m
	replyRegressionCheck(t, s.Record(duplicate))
	time.Sleep(30 * time.Millisecond)
	if sends.Load() != before {
		t.Fatal("事件重放重复回复")
	}
	st := s.store.snapshot()
	for _, msg := range st.Messages {
		if msg.ID == m.ID || msg.Source == "plugin:turtlesoup" {
			if msg.Game == "" || !contextAvailable(msg) || contextMessage(st, msg).Game != "turtlesoup" || memoryScanEligible(st, msg) {
				t.Fatal("游戏上下文标记缺失或进入了长期记忆", msg.ID)
			}
		}
	}
	for _, task := range st.Tasks {
		if soupSource(task.Source) && (strings.Contains(task.Input, g.Puzzle.Bottom) || strings.Contains(task.Result, g.Puzzle.Bottom)) {
			t.Fatal("公开任务泄露汤底")
		}
	}
	var raw []byte
	replyRegressionCheck(t, s.store.db.QueryRow("SELECT body FROM soup_games WHERE id=?", g.ID).Scan(&raw))
	if strings.Contains(string(raw), g.Puzzle.Bottom) {
		t.Fatal("汤底未加密")
	}
	if got := soupRecord(t, s, m, "提示"); got.Status != "completed" || !strings.Contains(got.Output, "提示 1") {
		t.Fatal(got)
	}
	if got := soupRecord(t, s, m, "看汤底"); !strings.Contains(got.Output, g.Puzzle.Bottom) {
		t.Fatal(got)
	}
	// 同一群中成员稳定 ID 不同，即使昵称相同，也不能替开局者揭晓。
	group := m
	group.Target = "fixture-soup-group@chatroom"
	group.SenderID = "fixture-opener"
	group.Mentions = []string{s.store.snapshot().Account.ID}
	group.ID = randomID()
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = append(st.Contacts, Contact{ID: group.Target, Kind: "group"})
		st.Settings["turtlesoup.scope"] = "whitelist"
		st.Settings["turtlesoup.targets"] = m.Target + "," + group.Target
		return nil
	}))
	replyRegressionCheck(t, s.Record(group))
	_ = waitSoupTurn(t, s, group)
	other := group
	other.SenderID = "fixture-another-player"
	got := soupRecord(t, s, other, "看汤底")
	if !strings.Contains(got.Output, "开局者") || strings.Contains(got.Output, g.Puzzle.Bottom) {
		t.Fatal("非开局者泄题", got)
	}
	got = soupRecord(t, s, other, "她是迟到了吗？")
	if got.Status != "completed" {
		t.Fatal("其他成员无法参加", got)
	}
	got = soupRecord(t, s, group, "换一碗")
	if !strings.Contains(got.Output, "确认换汤") {
		t.Fatal("未确认便换题", got)
	}
	got = soupRecord(t, s, group, "fixture-soup-solution：她是保洁员，误解了值班时间，老板合并了清洁时段。")
	if got.Status != "completed" || !strings.Contains(got.Output, "猜对了") || !strings.Contains(got.Output, g.Puzzle.Bottom) {
		t.Fatal("完整推理未通关", got)
	}
}

func TestSoupWholePipelineFallbackAndTopicIsolation(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := soupFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) })
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		a := st.Models[0]
		b := a
		b.ID = "fixture-soup-second"
		b.Model = "second"
		st.Models = append(st.Models, b)
		st.Pipeline = append(st.Pipeline, b.ID)
		st.Settings["routing.fallback"] = true
		return nil
	}))
	replyRegressionCheck(t, s.Record(m))
	turn := waitSoupTurn(t, s, m)
	if turn.Status != "completed" || calls.Load() != 4 || !strings.Contains(turn.Output, "固定题库") {
		t.Fatal(turn, calls.Load())
	}
	g, e := s.liveSoup(s.store.snapshot().Account.ID, m.Target)
	replyRegressionCheck(t, e)
	if g.Source != "bank" || g.Puzzle.Theme != "职场" {
		t.Fatal(g)
	}
	if _, e = s.soupFallback(s.store.snapshot().Account.ID, m.Target, "职场"); e == nil {
		t.Fatal("重复固定题")
	}
	if _, e = s.soupFallback(s.store.snapshot().Account.ID, "another-conversation", "职场"); e != nil {
		t.Fatal("跨会话错误消耗题库", e)
	}
	soupRecord(t, s, m, "不玩了")
	turn = soupRecord(t, s, m, "来碗海龟汤 太空考古")
	if turn.Status != "failed" || !turn.Retryable || strings.Contains(turn.Output, "【汤面】") {
		t.Fatal("悄悄替换了主题", turn)
	}
}

func TestSoupMalformedVerdictRetryAndNoLeak(t *testing.T) {
	var bad atomic.Bool
	bad.Store(true)
	s, m, sends := soupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []struct{ Content string } }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Messages[0].Content, "题目作者") {
			soupJSON(w, turtlesoup.Defaults[0])
			return
		}
		if bad.Load() {
			soupJSON(w, map[string]any{"intent": "guess", "answer": "yes", "solved": true, "matched_fact_ids": []string{"f1"}, "text": "不应该发送的汤底"})
		} else {
			soupJSON(w, turtlesoup.Verdict{Intent: "question", Answer: "no", Facts: []string{}})
		}
	})
	replyRegressionCheck(t, s.Record(m))
	_ = waitSoupTurn(t, s, m)
	turn := soupRecord(t, s, m, "我猜是她上班晚了")
	if turn.Status != "failed" || !turn.Retryable || sends.Load() != 1 {
		t.Fatal("无效裁定被发送", turn, sends.Load())
	}
	bad.Store(false)
	adminResp := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-soup-admin", "password": "fixture-password-123"}, nil, "")
	admin := adminResp.Result().Cookies()[0]
	resp := call(t, s, "/api/turtlesoup", map[string]string{"operation": "retry", "id": turn.ID}, admin, "")
	if resp.Code != 200 {
		t.Fatal(resp.Body.String())
	}
	waitBusiness(t, func() bool {
		next, e := s.soupTurn(s.store.snapshot().Account.ID, turn.ID)
		return e == nil && next.Status == "completed"
	})
	if sends.Load() != 2 {
		t.Fatal("重试未恰好发送一次", sends.Load())
	}
	resp = call(t, s, "/api/turtlesoup", map[string]string{"operation": "retry", "id": turn.ID}, admin, "")
	if resp.Code != 400 {
		t.Fatal("允许重复重试已完成回合")
	}
}

func TestSoupPreparedRecoveryDoesNotRepeatHint(t *testing.T) {
	s := newServer(t, true)
	account := s.store.snapshot().Account.ID
	m := Message{ID: "fixture-soup-recovery", Target: "mock-contact-0", SenderID: "fixture-player", Sender: "玩家", Content: "提示", Kind: "text", Source: "wechat", Status: "sent", Game: "turtlesoup", Time: time.Now().Format(time.RFC3339)}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = append(st.Messages, m); return nil }))
	g := soupGame{ID: randomID(), Target: m.Target, Owner: m.SenderID, Status: "playing", Puzzle: turtlesoup.Defaults[0], Hints: 1}
	replyRegressionCheck(t, s.saveSoupGame(account, &g))
	turn := soupTurn{ID: randomID(), GameID: g.ID, Message: m, Command: "hint", Status: "running"}
	raw, e := s.store.sealMemory(account, "soup-turn:"+turn.ID, turn)
	replyRegressionCheck(t, e)
	_, e = s.store.db.Exec("INSERT INTO soup_turns(id,account,target,message_id,status,updated,body) VALUES(?,?,?,?,?,?,?)", turn.ID, account, m.Target, m.ID, turn.Status, "", raw)
	replyRegressionCheck(t, e)
	replyRegressionCheck(t, s.prepareSoupEffect(account, &g, &turn, "提示 1：固定提示"))
	replyRegressionCheck(t, s.initSoup())
	turn, e = s.soupTurn(account, turn.ID)
	replyRegressionCheck(t, e)
	if !turn.Retryable || turn.Status != "failed" {
		t.Fatal(turn)
	}
	turn.Status = "running"
	replyRegressionCheck(t, s.saveSoupTurn(account, turn))
	s.runSoupTurn(account, s.store.snapshot().AccountEpoch, turn)
	g, e = s.soupGame(account, g.ID)
	replyRegressionCheck(t, e)
	if g.Hints != 1 {
		t.Fatal("重试重复扣提示")
	}
	turn, e = s.soupTurn(account, turn.ID)
	replyRegressionCheck(t, e)
	if turn.Status != "completed" || turn.Output != "提示 1：固定提示" {
		t.Fatal(turn)
	}
	turn.Status = "sending"
	replyRegressionCheck(t, s.saveSoupTurn(account, turn))
	replyRegressionCheck(t, s.initSoup())
	turn, e = s.soupTurn(account, turn.ID)
	replyRegressionCheck(t, e)
	if turn.Status != "unknown" || turn.Retryable {
		t.Fatal("未知发送结果允许盲目重发", turn)
	}
}

func TestSoupBudgetAndTriggerBoundaries(t *testing.T) {
	s := newServer(t, true)
	st := s.store.snapshot()
	m := Message{ID: "fixture-trigger", Target: "mock-contact-1", SenderID: "mock-contact-1", Kind: "text", Content: "他说来碗海龟汤是什么意思", Source: "wechat"}
	if s.soupCandidate(m) {
		t.Fatal("普通聊天被当成开局")
	}
	m.Content = "来碗海龟汤！"
	if !s.soupCandidate(m) {
		t.Fatal("标点未支持")
	}
	m.Kind = "quote"
	m.Rich = &RichMessage{}
	if s.soupCandidate(m) {
		t.Fatal("引用触发开局")
	}
	q := ai.Request{Kind: "chat", Source: "turtlesoup-judge", Prompt: "固定汤底及当前问题", System: "主持规则", Target: m.Target, Context: []contextcleaner.Message{{ID: "old", Role: "user", Session: m.Target, Text: strings.Repeat("旧", 400)}, {ID: "new", Role: "user", Session: m.Target, Text: "新问题"}}}
	_, c, e := safetyConfig(st, q.Source, q.Target)
	replyRegressionCheck(t, e)
	actual, _, e := materializeSoup(st, q, Model{Context: 1, ContextTokens: 1200}, 100, c, safetyReport{})
	replyRegressionCheck(t, e)
	if !strings.Contains(actual.Prompt, "固定汤底") || strings.Contains(actual.Prompt, "旧旧旧") || !strings.Contains(actual.Prompt, "新问题") {
		t.Fatal("固定资料或模型上下文设置未生效", actual.Prompt)
	}
	_, _, e = materializeSoup(st, q, Model{ContextTokens: 100}, 100, c, safetyReport{})
	if e == nil {
		t.Fatal("固定资料容量不足未停止")
	}
}

// 编译时确保既有 Host 的契约不被游戏扩展改变。
var _ control.Host = replyRegressionHost(func(context.Context, control.Command) (any, error) { return nil, fmt.Errorf("offline") })

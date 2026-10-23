package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestSoupGroupOpeningRequiresRealMention(t *testing.T) {
	s := newServer(t, true)
	st := s.store.snapshot()
	group := "fixture-mention-group@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Contacts = append(st.Contacts, Contact{ID: group, Kind: "group", Mode: "auto"})
		st.Settings["turtlesoup.scope"] = "whitelist"
		st.Settings["turtlesoup.targets"] = group + ",mock-contact-1"
		st.Settings["reply.enabled"] = true
		st.Settings["reply.probability"] = float64(100)
		return nil
	}))
	m := Message{ID: "fixture-no-at-start", Target: group, SenderID: "fixture-group-player", Sender: "玩家", Source: "wechat", Status: "sent", Kind: "text", Content: "来碗海龟汤", Time: time.Now().Format(time.RFC3339)}
	if s.soupCandidate(m) {
		t.Fatal("群聊未 @ 就开局")
	}
	before := len(s.store.snapshot().Tasks)
	replyRegressionCheck(t, s.Record(m))
	if turns, err := s.soupTurns(st.Account.ID, group); err != nil || len(turns) != 0 {
		t.Fatal("未 @ 的开局进入游戏队列")
	}
	if len(s.store.snapshot().Tasks) != before {
		t.Fatal("未 @ 的开局绕入普通 AI")
	}
	for _, test := range []Message{
		{Content: "@小禾\u2005来碗海龟汤"},
		{Content: "@其他人\u2005来碗海龟汤", Mentions: []string{"fixture-other-member"}},
		{Content: "来碗海龟汤", MentionAll: true},
	} {
		next := m
		next.ID = randomID()
		next.Content = test.Content
		next.Mentions = test.Mentions
		next.MentionAll = test.MentionAll
		if s.soupCandidate(next) {
			t.Fatal("文字 @、@ 他人或 @ 全体冒充本人提醒")
		}
		replyRegressionCheck(t, s.Record(next))
		if len(s.store.snapshot().Tasks) != before {
			t.Fatal("不合法的 @ 开局进入了普通 AI")
		}
	}
	opening := m
	opening.ID = "fixture-real-at-start"
	opening.Mentions = []string{st.Account.ID}
	opening.Content = "@小禾\u2005来碗海龟汤 职场"
	if !s.soupCandidate(opening) {
		t.Fatal("真实 @ 未触发开局")
	}
	replyRegressionCheck(t, s.Record(opening))
	turn := waitSoupTurn(t, s, opening)
	if turn.Status != "completed" {
		t.Fatal(turn)
	}
	question := m
	question.ID = "fixture-no-at-question"
	question.Content = "她迟到了吗？"
	if !s.soupCandidate(question) {
		t.Fatal("开局后的自然提问被错误要求 @")
	}
	if err := s.soupTurnAllowed(st.Account.ID, st.AccountEpoch, soupTurn{Message: m, Command: "start"}); err == nil || !strings.Contains(err.Error(), "需要真实 @") {
		t.Fatal("旧开局回合可绕过新的 @ 开关", err)
	}
	private := m
	private.Target = "mock-contact-1"
	private.SenderID = private.Target
	if !s.soupCandidate(private) {
		t.Fatal("私聊被错误要求 @")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["turtlesoup.group_start_mention"] = false; return nil }))
	if !s.soupCandidate(m) {
		t.Fatal("关闭开关后仍然强制 @")
	}
}

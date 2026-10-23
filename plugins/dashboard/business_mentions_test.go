package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestBusinessGroupCommandsRequireActualSelfMention(t *testing.T) {
	for _, tc := range []struct {
		name, plugin, text                                  string
		mention, other, all, disabled, private, quote, want bool
	}{
		{name: "music-no-mention", plugin: "music", text: "点歌，完美的一天"},
		{name: "music-typed-name", plugin: "music", text: "@测试本人\u2005点歌 晴天"},
		{name: "music-mention-other", plugin: "music", text: "@其他人\u2005点歌 晴天", other: true},
		{name: "music-mention-all", plugin: "music", text: "点歌 晴天", all: true},
		{name: "music-actual-mention", plugin: "music", text: "@测试本人\u2005点歌，完美的一天", mention: true, want: true},
		{name: "music-trailing-mention", plugin: "music", text: "点歌，完美的一天 @测试本人\u2005", mention: true, want: true},
		{name: "music-both-mentions", plugin: "music", text: "@其他人\u2005@测试本人\u2005点歌 晴天", mention: true, other: true, want: true},
		{name: "music-quote-reply", plugin: "music", text: "点歌 孙燕姿 完美的第一天", mention: true, quote: true, want: true},
		{name: "music-config-off", plugin: "music", text: "点歌 晴天", disabled: true, want: true},
		{name: "music-private", plugin: "music", text: "点歌 晴天", private: true, want: true},
		{name: "statistics-no-mention", plugin: "statistics", text: "消息统计"},
		{name: "statistics-actual-mention", plugin: "statistics", text: "@测试本人\u2005消息统计", mention: true, want: true},
		{name: "news-no-mention", plugin: "news", text: "今日新闻"},
		{name: "meal-no-mention", plugin: "what_to_eat", text: "吃什么"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t, true)
			target, kind := "fixture-business@chatroom", "group"
			if tc.private {
				target, kind = "fixture-business-peer", "friend"
			}
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Account.ID, st.Account.Name, st.Account.Status = "fixture-business-self", "测试本人", "online"
				st.Contacts = []Contact{{ID: target, Kind: kind, Mode: "auto"}}
				st.Messages = nil
				st.Tasks = nil
				st.Settings[tc.plugin+".group_mention_required"] = !tc.disabled
				st.Settings["reply.enabled"], st.Settings["reply.probability"] = true, float64(100)
				return nil
			}))
			m := Message{ID: "fixture-business-current", Target: target, SenderID: "fixture-member", Sender: "测试成员", Source: "wechat", Kind: "text", Content: tc.text, Status: "sent", Time: time.Now().Format(time.RFC3339), MentionAll: tc.all}
			if tc.mention {
				m.Mentions = append(m.Mentions, "fixture-business-self")
			}
			if tc.other {
				m.Mentions = append(m.Mentions, "fixture-other")
			}
			if tc.quote {
				m.Kind = "quote"
				m.Rich = &RichMessage{QuoteID: "fixture-original", Quote: "原文"}
			}
			replyRegressionCheck(t, s.Record(m))
			if tc.want {
				waitBusiness(t, func() bool {
					for _, v := range s.store.snapshot().Messages {
						if v.Self && v.Source == "plugin:"+tc.plugin && v.Status == "sent" {
							return true
						}
					}
					return false
				})
			} else {
				var count int
				replyRegressionCheck(t, s.store.db.QueryRow("SELECT count(*) FROM business_runs").Scan(&count))
				if count != 0 || len(s.store.snapshot().Tasks) != 0 || s.hasGroupReplyBatch(s.store.snapshot(), target) {
					t.Fatal("未真正 @ 本人却执行插件或由普通 AI 兜底")
				}
				found := false
				for _, l := range s.store.snapshot().Logs {
					found = found || strings.Contains(l.Message, "群聊触发要求真实 @ 本人")
				}
				if !found {
					t.Fatal("没有记录未满足 @ 要求的原因")
				}
			}
		})
	}
}

func TestGroupMentionSettingDefaultAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir, false)
	replyRegressionCheck(t, err)
	for _, id := range []string{"music", "news", "statistics", "wordcloud", "what_to_eat", "undercover", "fortune"} {
		if store.snapshot().Settings[id+".group_mention_required"] != true {
			t.Fatal("群聊 @ 要求未默认开启", id)
		}
	}
	replyRegressionCheck(t, store.change(func(st *State) error { st.Settings["music.group_mention_required"] = false; return nil }))
	replyRegressionCheck(t, store.db.Close())
	store, err = openStore(dir, false)
	replyRegressionCheck(t, err)
	defer store.db.Close()
	if store.snapshot().Settings["music.group_mention_required"] != false {
		t.Fatal("重启覆盖了已关闭的 @ 要求")
	}
}

func TestPlayroomGroupMentionCanBeConfigured(t *testing.T) {
	s := newServer(t, true)
	target := "fixture-config-game@chatroom"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "fixture-game-self"
		st.Contacts = []Contact{{ID: target, Kind: "group", Mode: "auto"}}
		return nil
	}))
	m := Message{ID: "fixture-game-start", Target: target, SenderID: "fixture-player", Kind: "text", Source: "wechat", Content: "玩谁是卧底", Status: "sent"}
	route, ok := s.routePlay(m)
	if !ok || !route.Blocked {
		t.Fatal("默认没有要求 @")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["undercover.group_mention_required"] = false; return nil }))
	route, ok = s.routePlay(m)
	if !ok || route.Blocked {
		t.Fatal("关闭 @ 要求后仍无法开局")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Settings["undercover.group_mention_required"] = true; return nil }))
	m.Content = "@甲\u2005@乙\u2005玩谁是卧底"
	m.Mentions = []string{"fixture-other", "fixture-game-self"}
	route, ok = s.routePlay(m)
	if !ok || route.Blocked {
		t.Fatal("多人 @ 中包含本人却未开局")
	}
}

func TestBusinessCommandMentionsDoNotContaminateQuery(t *testing.T) {
	for _, content := range []string{"@昵称 有空格\u2005点歌，完美的一天", "点歌，完美的一天 @昵称 有空格\u2005", "@甲\u2005点歌，完美的一天 @乙\u2005"} {
		m := Message{Content: content, Mentions: []string{"fixture-self"}}
		if got := businessCommandText(m); got != "点歌，完美的一天" {
			t.Errorf("提醒文字污染了查询：%q", got)
		}
	}
}

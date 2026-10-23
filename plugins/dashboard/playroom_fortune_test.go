package dashboard

import (
	"github.com/we-clay/we-clay/plugins/playroom"
	"strings"
	"testing"
	"time"
)

func TestFortuneReplyCleansLegacyDailyCache(t *testing.T) {
	s, m, calls := playFixture(t, "fortune", false)
	now := time.Now().In(time.FixedZone("北京时间", 8*3600))
	day := now.Format("2006-01-02")
	modelDate := now.Format("日期：2006年1月2日")
	legacy := "【互动插件 · fortune】\n" + day + " · 第49签（仅供娱乐）\n签名\n【仅供娱乐·虚构签文】\n林间有风\n" + modelDate + "\n抽签编号：49\n今日（北京时间）\n关键词：从容\n幸运物：绿叶\n\n▍主持小提醒：\n签文为娱乐向文字游戏，不构成任何吉凶判断、医疗、投资、法律或其他决策依据。日子是自己过的，签只是个陪聊的话头。"
	want := "林间有风\n" + modelDate + "\n今日\n关键词：从容\n幸运物：绿叶"
	if got := fortuneReply(legacy); got != day+"\n"+want || fortuneReply(got) != got {
		t.Fatalf("签文清理错误：%q", got)
	}
	legacy += "\n主持：" + s.store.snapshot().Account.Name
	account := s.store.snapshot().Account.ID
	actor := playActor(s.store.snapshot(), m)
	b, e := s.store.sealMemory(account, "play-daily:"+m.Target+":"+actor+":"+day, legacy)
	replyRegressionCheck(t, e)
	_, e = s.store.db.Exec("INSERT INTO play_daily VALUES(?,?,?,?,?)", account, m.Target, actor, day, b)
	replyRegressionCheck(t, e)
	origin := playTurn{ID: randomID(), Created: now.Format(time.RFC3339Nano), Plugin: "fortune", Target: m.Target, Message: m, TaskID: "fixture-fortune-generation", Draw: 49, Status: "completed", Deliveries: []playroom.Delivery{playroom.Public(legacy)}}
	b, e = s.store.sealMemory(account, "play-turn:"+origin.ID, origin)
	replyRegressionCheck(t, e)
	_, e = s.store.db.Exec("INSERT INTO play_turns(account,id,game_id,target,source_target,message_id,status,body) VALUES(?,?,?,?,?,?,?,?)", account, origin.ID, "", m.Target, m.Target, m.ID, origin.Status, b)
	replyRegressionCheck(t, e)
	turn := recordPlay(t, s, m, "抽个今日签")
	if turn.Status != "completed" || calls.Load() != 0 {
		t.Fatal("未复用每日缓存", turn.Error, calls.Load())
	}
	found := false
	for _, msg := range s.store.snapshot().Messages {
		if msg.Self && msg.Target == m.Target && msg.Content == "第49签 · "+want {
			found = true
			if !strings.Contains(msg.Game, "fortune") || memoryMessageAvailable(msg) {
				t.Fatal("签文丢失游戏标记或进入长期记忆")
			}
		}
	}
	if !found {
		t.Fatal("实际发送内容未清理")
	}
	var cached playDailyText
	replyRegressionCheck(t, s.store.db.QueryRow("SELECT body FROM play_daily WHERE account=? AND target=? AND actor=? AND day=?", account, m.Target, actor, day).Scan(&b))
	replyRegressionCheck(t, s.store.openMemory(account, "play-daily:"+m.Target+":"+actor+":"+day, b, &cached))
	if cached.Draw != 49 || cached.Text != fortuneReply(legacy)[len(day)+1:] {
		t.Fatal("未持久化恢复的签号")
	}
	// 新格式即使以模型写的 ISO 日期开头，也不能再把它误当成程序前缀。
	modelText := day + "\n模型签文正文"
	b, e = s.store.sealMemory(account, "play-daily:"+m.Target+":"+actor+":"+day, playDailyText{Text: modelText, Draw: 42})
	replyRegressionCheck(t, e)
	_, e = s.store.db.Exec("UPDATE play_daily SET body=? WHERE account=? AND target=? AND actor=? AND day=?", b, account, m.Target, actor, day)
	replyRegressionCheck(t, e)
	if next := recordPlay(t, s, m, "抽个今日签"); next.Status != "completed" || calls.Load() != 0 {
		t.Fatal("新缓存读取失败", next.Error)
	}
	found = false
	for _, msg := range s.store.snapshot().Messages {
		if msg.Self && msg.Content == "第42签\n"+modelText {
			found = true
		}
	}
	if !found {
		t.Fatal("误删了模型日期")
	}
}

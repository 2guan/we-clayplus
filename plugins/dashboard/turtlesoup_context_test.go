package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

// 捕获实际发给模型的普通聊天请求，只读取微信公开记录，不加载对局内部汤底。
func TestSoupPublicDialogueInNormalModelContext(t *testing.T) {
	captured := make(chan string, 1)
	s, current, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		captured <- string(raw)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"刚才主持回答的是不是，这是游戏中的判断。"}}]}`)
	})
	question := current
	question.ID = "fixture-game-question"
	question.Content = "她是因为迟到被批评吗？"
	question.Game = "turtlesoup"
	question.Time = time.Now().Add(-3 * time.Second).Format(time.RFC3339)
	host := question
	host.ID = "fixture-game-host"
	host.Self = true
	host.Source = "plugin:turtlesoup"
	host.Content = "不是。"
	host.Game = ""
	host.Time = time.Now().Add(-2 * time.Second).Format(time.RFC3339)
	ordinary := question
	ordinary.ID = "fixture-normal-before"
	ordinary.Game = ""
	ordinary.Content = "今天聊得很开心。"
	current.Content = "刚才那个问题主持怎么回答的？"
	current.Kind = "quote"
	current.Rich = &RichMessage{QuoteID: question.ID, Quote: question.Content}
	revoked := question
	revoked.ID = "fixture-revoked-game"
	revoked.Status = "revoked_by_other"
	revoked.Content = "fixture-revoked-should-not-appear"
	other := question
	other.ID = "fixture-other-game"
	other.Target = "fixture-other-conversation"
	other.Content = "fixture-cross-conversation-should-not-appear"
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Messages = []Message{ordinary, question, host, revoked, other, current}
		return nil
	}))
	game := soupGame{ID: "fixture-hidden-game", Target: current.Target, Status: "ended"}
	game.Puzzle.Bottom = "fixture-unrevealed-private-bottom"
	replyRegressionCheck(t, s.saveSoupGame(s.store.snapshot().Account.ID, &game))
	job := retrySubmit(t, s, current)
	if job.Status != "completed" || sends.Load() != 1 {
		t.Fatal(job, sends.Load())
	}
	raw := <-captured
	var body struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, m := range body.Messages {
		text += m.Content + "\n"
	}
	for _, want := range []string{question.Content, host.Content, ordinary.Content, "海龟汤游戏·虚构", "不代表参与者的现实情况"} {
		if !strings.Contains(text, want) {
			t.Fatal("实际模型请求缺少游戏过程或边界", want)
		}
	}
	if strings.Count(text, question.Content) != 1 {
		t.Fatal("引用的游戏问题重复注入", text)
	}
	for _, forbidden := range []string{game.Puzzle.Bottom, revoked.Content, other.Content} {
		if strings.Contains(text, forbidden) {
			t.Fatal("注入了未公开、撤回或跨会话内容", forbidden)
		}
	}
	if memoryScanEligible(s.store.snapshot(), question) {
		t.Fatal("游戏问题被当作长期记忆来源")
	}
	if contextMessage(s.store.snapshot(), ordinary).Game != "" {
		t.Fatal("普通消息被标为游戏")
	}
	// 已公开的汤底与问答一样可读，但始终带游戏标签。
	revealed := host
	revealed.ID = "fixture-revealed-bottom"
	revealed.Content = "【汤底】fixture-public-game-solution"
	revealed.Game = "turtlesoup"
	st := s.store.snapshot()
	st.Messages = append(st.Messages[:len(st.Messages):len(st.Messages)], revealed)
	conv, history := buildReplyContext(st, current, time.Now())
	request := ai.Request{Prompt: current.Content, Conversation: &conv, Context: history}
	joined := ""
	for _, turn := range ai.ConversationMessages(request) {
		joined += turn.Text
	}
	if !strings.Contains(joined, revealed.Content) || !strings.Contains(joined, "海龟汤游戏·虚构") {
		t.Fatal("已公开汤底未带标记进入上下文")
	}
}

func TestSoupGameTagIsPartOfReplyEvidence(t *testing.T) {
	m := Message{ID: "fixture-game-evidence", Target: "fixture-context", SenderID: "fixture-player", Content: "普通记录", Kind: "text"}
	before := replyEvidenceHash(m)
	m.Game = "turtlesoup"
	if before == replyEvidenceHash(m) {
		t.Fatal("游戏身份变化未使旧回复证据失效")
	}
}

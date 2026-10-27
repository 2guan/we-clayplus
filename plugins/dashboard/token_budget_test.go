package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

func TestLargeConversationReachesProvider(t *testing.T) {
	st := initialState(true)
	st.Settings["memory.messages"] = float64(1000)
	st.Settings["memory.tokens"] = float64(0)
	st.Settings["context_cleaner.max_input"] = float64(0)
	q := ai.Request{Kind: "chat", Source: "wechat", Target: "fixture-session", Prompt: "接着写下一章", System: replyInstructions(st, "接着写下一章", "text"), Conversation: &ai.Conversation{
		AccountID: "fixture-self", SelfName: "本人", Session: "fixture-session", Scene: "group", Current: contextcleaner.Message{ID: "current", SenderID: "fixture-player", Role: "user"},
	}}
	for i := 0; i < 900; i++ {
		role, sender := "user", "fixture-player"
		if i%2 == 1 {
			role, sender = "assistant", "fixture-self"
		}
		q.Context = append(q.Context, contextcleaner.Message{ID: fmt.Sprint(i), Role: role, SenderID: sender, Text: fmt.Sprintf("fixture-story-turn-%03d", i)})
	}
	prepared, _, err := materializeSafety(st, q, Model{}, 128)
	if err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) < 900 {
			t.Errorf("实际 HTTP 请求只有 %d 条消息", len(body.Messages))
		}
		all := ""
		for _, m := range body.Messages {
			all += m.Content
		}
		for i := 0; i < 900; i++ {
			if !strings.Contains(all, fmt.Sprintf("fixture-story-turn-%03d", i)) {
				t.Errorf("HTTP 请求缺失历史 %d", i)
			}
		}
		if !strings.Contains(body.Messages[len(body.Messages)-1].Content, q.Prompt) {
			t.Error("当前接龙请求没有放在末尾")
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"下一章正文"}}]}`)
	}))
	defer remote.Close()
	result, err := ai.Execute(context.Background(), ai.Config{Endpoint: remote.URL, Provider: "openai", Model: "fixture", Key: "fixture-key"}, prepared, false)
	if err != nil || result.Text != "下一章正文" {
		t.Fatal("实际模型传输链路失败", err)
	}
}

func TestTokenBudgetSettings(t *testing.T) {
	for _, n := range []int{0, 9999999, 10000000} {
		for group, key := range map[string]string{"memory": "tokens", "context_cleaner": "max_input"} {
			if err := validateSettings(group, map[string]any{key: float64(n)}); (err == nil) != (n <= 9999999) {
				t.Fatalf("%s.%s=%d 校验错误：%v", group, key, n, err)
			}
		}
	}
	for _, n := range []int{-2, -1, 0, 9999999, 10000000} {
		if err := validateModel(Model{ContextTokens: n}); (err == nil) != (n >= -1 && n <= 9999999) {
			t.Fatalf("模型预算 %d 校验错误：%v", n, err)
		}
	}
}

func TestLargeAndUnlimitedTokenBudgets(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		global, model, capacity int
		textSize                int
		wantHistory             bool
	}{
		{"大预算超过旧两百万上限", 9999999, 0, 0, 2000100, true},
		{"全局无限制超过可填写上限", 0, 0, 0, 10000100, true},
		{"模型无限制覆盖有限全局", 256, -1, 0, 2000100, true},
		{"模型大预算覆盖有限全局", 256, 9999999, 0, 2000100, true},
		{"有限模型仍裁剪", 0, 1024, 0, 4096, false},
		{"独立请求容量仍生效", 0, -1, 2048, 4096, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := initialState(true)
			st.Settings["memory.tokens"] = float64(tc.global)
			st.Settings["context_cleaner.max_input"] = float64(tc.capacity)
			q := ai.Request{Source: "wechat", Target: "fixture-session", Prompt: "本轮问题", Conversation: &ai.Conversation{
				Session: "fixture-session", Scene: "private", Current: contextcleaner.Message{ID: "current", Text: "本轮问题", Role: "user"},
			}, Context: []contextcleaner.Message{{ID: "history", Role: "user", Text: strings.Repeat("x", tc.textSize)}}}
			out, _, err := materializeSafety(st, q, Model{ContextTokens: tc.model}, 128)
			if err != nil {
				t.Fatal(err)
			}
			if (len(out.Context) == 1) != tc.wantHistory {
				t.Fatalf("历史保留错误：%d", len(out.Context))
			}
			if tc.wantHistory && out.Context[0].Text != q.Context[0].Text {
				t.Fatal("历史内容被截断")
			}
			if tc.model == -1 && contextTokenBudget(st, Model{ContextTokens: tc.model}) != 0 {
				t.Fatal("连续工具调用未继承模型无限制设置")
			}
		})
	}
}

func TestUnlimitedDocumentsAndGameHistory(t *testing.T) {
	st := initialState(true)
	st.Settings["memory.tokens"] = float64(0)
	st.Settings["context_cleaner.max_input"] = float64(0)
	text := strings.Repeat("x", 2000100)
	message := contextcleaner.Message{ID: "fixture-large", Role: "user", Text: text}
	q := ai.Request{Source: "analysis", Kind: "insight", Prompt: "分析", Documents: []contextcleaner.Message{message}}
	out, _, err := materializeSafety(st, q, Model{}, 128)
	if err != nil || !strings.Contains(out.Prompt, text) {
		t.Fatal("无限制分析仍受旧容量限制", err)
	}
	q = ai.Request{Source: "plugin:turtlesoup", Prompt: "问题", Context: []contextcleaner.Message{message}}
	_, c, err := safetyConfig(st, q.Source, q.Target)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err = materializeSoup(st, q, Model{ContextTokens: -1}, 128, c, safetyReport{})
	if err != nil || !strings.Contains(out.Prompt, text) {
		t.Fatal("无限制游戏历史仍被裁剪", err)
	}
}

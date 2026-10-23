package dashboard

import "testing"

func TestCompactPlayReplyPreservesContentAndSources(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"【互动插件 · radio】\n主持：主持甲\n今天想聊些什么？\n\n**主持人：主持甲**", "今天想聊些什么？"},
		{"林间有风\n主播: 主持甲", "林间有风"},
		{"主持：虚构角色乙\n主持甲说，慢慢来。[来源:fixture-source]", "主持：虚构角色乙\n主持甲说，慢慢来。[来源:fixture-source]"},
		{"【谁是卧底 · fixture-code】\n投票 2\n第2/5题", "【谁是卧底 · fixture-code】\n投票 2\n第2/5题"},
	} {
		if got := compactPlayReply(tc.input, "主持甲"); got != tc.want {
			t.Fatalf("回复内容发生非预期变化：%q", got)
		}
	}
}

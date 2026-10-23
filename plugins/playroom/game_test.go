package playroom

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func testGame(kind string, n int) Game {
	g := Game{ID: "test-game", Code: "testcode", Kind: kind, Owner: "p1", Target: "fixture-group", Status: "lobby", LastGuard: -1}
	for i := 1; i <= n; i++ {
		g.Players = append(g.Players, Player{ID: fmt.Sprint("p", i), Name: fmt.Sprint("玩家", i), Ready: true, Alive: true, HP: 10})
	}
	return g
}
func move(t *testing.T, g *Game, i int, private bool, text string) []Delivery {
	t.Helper()
	next, out, e := Apply(*g, Input{Actor: g.Players[i].ID, Name: g.Players[i].Name, Text: text, Private: private, Friend: true})
	if e != nil {
		t.Fatalf("%s %s: %v", g.Kind, text, e)
	}
	*g = next
	return out
}
func TestUndercoverCompleteAndNoPublicWordLeak(t *testing.T) {
	g := testGame("undercover", 4)
	g.Pack = Pack{Title: "美食", Words: []string{"苹果", "梨"}}
	out := move(t, &g, 0, false, "开始游戏")
	for _, d := range out {
		if !d.Private && (strings.Contains(d.Text, "苹果") || strings.Contains(d.Text, "梨")) {
			t.Fatal("公开泄词")
		}
	}
	wolf := -1
	for i, p := range g.Players {
		if p.Role == "卧底" {
			wolf = i
		}
	}
	for i := range g.Players {
		move(t, &g, i, false, "发言 这是一种常见食物")
	}
	for i := range g.Players {
		v := wolf
		if i == wolf {
			v = (wolf + 1) % 4
		}
		move(t, &g, i, false, fmt.Sprintf("投票 %d", v+1))
	}
	if g.Phase != "guess" {
		t.Fatal(g.Phase)
	}
	move(t, &g, wolf, true, "猜词 苹果")
	if g.Status != "ended" {
		t.Fatal("未结束")
	}
}
func TestWerewolfAllSeatsNightAndHunter(t *testing.T) {
	for seats := 6; seats <= 12; seats++ {
		t.Run(fmt.Sprint(seats), func(t *testing.T) {
			g := testGame("werewolf", seats)
			move(t, &g, 0, false, "开始游戏")
			target := -1
			for i, p := range g.Players {
				if p.Role == "村民" {
					target = i
					break
				}
			}
			for i, p := range g.Players {
				if p.Role == "狼人" {
					move(t, &g, i, true, fmt.Sprintf("刀 %d", target+1))
				}
			}
			for i, p := range g.Players {
				if p.Role == "预言家" {
					out := move(t, &g, i, true, fmt.Sprintf("验 %d", target+1))
					for _, d := range out {
						if strings.Contains(d.Text, "查验结果") && !d.Private {
							t.Fatal("泄露查验")
						}
					}
				}
				if p.Role == "女巫" || p.Role == "守卫" {
					move(t, &g, i, true, "跳过")
				}
			}
			if g.Players[target].Alive || g.Phase != "day" {
				t.Fatal("夜间结算错误", g.Phase)
			}
			move(t, &g, 0, false, "投票")
			victim := -1
			for i, p := range g.Players {
				if p.Role == "猎人" {
					victim = i
					break
				}
			}
			if victim < 0 {
				for i, p := range g.Players {
					if p.Alive && p.Role == "狼人" {
						victim = i
						break
					}
				}
			}
			for i, p := range g.Players {
				if p.Alive {
					v := victim
					if i == v {
						for j, q := range g.Players {
							if q.Alive && i != j {
								v = j
								break
							}
						}
					}
					move(t, &g, i, false, fmt.Sprintf("投票 %d", v+1))
				}
			}
			if seats >= 8 {
				if g.Phase != "hunter" {
					t.Fatal("猎人未触发", g.Phase)
				}
				move(t, &g, victim, true, "跳过")
			}
			if g.Phase != "wolves" {
				t.Fatal("未进入下一夜", g.Phase)
			}
		})
	}
}
func TestWerewolfWitchGuardRulesAndWin(t *testing.T) {
	g := testGame("werewolf", 6)
	g.Status = "playing"
	g.Phase = "night"
	g.Round = 2
	g.Players[0].Role = "女巫"
	g.Players[1].Role = "狼人"
	g.Players[2].Role = "狼人"
	g.Acts = map[string]int{"kill": 0}
	before, _ := json.Marshal(g)
	_, _, e := Apply(g, Input{Actor: "p1", Private: true, Text: "救"})
	if e == nil {
		t.Fatal("非首夜自救")
	}
	after, _ := json.Marshal(g)
	if string(before) != string(after) {
		t.Fatal("非法操作改变状态")
	}
	g.Players[1].Alive = false
	g.Players[2].Alive = false
	out := g.afterDeath("day")
	if g.Status != "ended" || !strings.Contains(out[0].Text, "好人获胜") {
		t.Fatal("胜负错误")
	}
}
func TestAvalonCompleteQuestAndAssassination(t *testing.T) {
	for seats := 5; seats <= 10; seats++ {
		t.Run(fmt.Sprint(seats), func(t *testing.T) {
			g := testGame("avalon", seats)
			move(t, &g, 0, false, "开始游戏")
			for round := 1; round <= 3; round++ {
				var team []string
				for i := 0; i < g.TeamSize(); i++ {
					team = append(team, fmt.Sprint(i+1))
				}
				move(t, &g, g.Leader, false, "组队 "+strings.Join(team, ","))
				for i := range g.Players {
					move(t, &g, i, true, "批准")
				}
				members := slices.Clone(g.Team)
				for _, i := range members {
					move(t, &g, i, true, "任务 成功")
				}
			}
			if g.Phase != "assassinate" {
				t.Fatal(g.Phase)
			}
			assassin, merlin := -1, -1
			for i, p := range g.Players {
				if p.Role == "刺客" {
					assassin = i
				}
				if p.Role == "梅林" {
					merlin = i
				}
			}
			out := move(t, &g, assassin, true, fmt.Sprintf("刺杀 %d", merlin+1))
			if g.Status != "ended" || !strings.Contains(out[0].Text, "刺中梅林") {
				t.Fatal("刺杀结算错误")
			}
		})
	}
}
func TestAvalonFiveRejectionsAndFourthMission(t *testing.T) {
	g := testGame("avalon", 5)
	move(t, &g, 0, false, "开始游戏")
	for attempt := 0; attempt < 5; attempt++ {
		var team []string
		for i := 0; i < g.TeamSize(); i++ {
			team = append(team, fmt.Sprint(i+1))
		}
		move(t, &g, g.Leader, false, "组队 "+strings.Join(team, ","))
		for i := range g.Players {
			move(t, &g, i, true, "反对")
		}
	}
	if g.Status != "ended" {
		t.Fatal("五次否决未结束")
	}
	g = testGame("avalon", 7)
	g.Status = "playing"
	g.Phase = "quest"
	g.Round = 4
	g.Team = []int{0, 1, 2, 3}
	g.Votes = map[string]int{}
	g.Players[0].Role = "刺客"
	move(t, &g, 0, true, "任务 失败")
	for i := 1; i <= 3; i++ {
		move(t, &g, i, true, "任务 成功")
	}
	if g.Successes != 1 || g.Failures != 0 {
		t.Fatal("七人第四任务应需两张失败")
	}
}
func TestQuizMurderAndAdventure(t *testing.T) {
	for _, kind := range []string{"chemistry", "personality"} {
		n := 1
		if kind == "chemistry" {
			n = 2
		}
		g := testGame(kind, n)
		// 开局只展示一次题目，性格测试不附带默契测试的私聊说明。
		g.Pack = Pack{Title: "测试"}
		for i := 0; i < 3; i++ {
			g.Pack.Questions = append(g.Pack.Questions, Question{Text: "问题", Options: []string{"甲", "乙", "丙", "丁"}, Scores: []int{0, 1, 2, 3}})
		}
		out := move(t, &g, 0, false, "开始游戏")
		if len(out) != 1 || strings.Count(out[0].Text, "第1/3题") != 1 || strings.Contains(out[0].Text, "私聊") != (kind == "chemistry") {
			t.Fatal("测试开局含重复或无关说明", kind)
		}
		for q := 0; q < 3; q++ {
			for i := range g.Players {
				move(t, &g, i, true, "答 D")
			}
		}
		if g.Status != "ended" {
			t.Fatal(kind)
		}
	}
	g := testGame("murder", 3)
	g.Pack = Pack{Title: "大厅", Text: "共同开场", Culprit: 2, Solution: "证据指向2号"}
	for i := 1; i <= 3; i++ {
		g.Pack.Characters = append(g.Pack.Characters, Character{Name: fmt.Sprint(i), Secret: "秘密", Goal: "目标"})
		g.Pack.Clues = append(g.Pack.Clues, Clue{ID: fmt.Sprint(i), Place: "大厅", Text: "线索", Owner: i})
	}
	move(t, &g, 0, false, "开始游戏")
	out := move(t, &g, 0, false, "搜证 大厅")
	if len(out) != 1 || !out[0].Private || out[0].Target != "p1" {
		t.Fatal("秘密线索泄露")
	}
	move(t, &g, 0, false, "指认")
	for i := range g.Players {
		move(t, &g, i, false, "指认 2")
	}
	if g.Status != "ended" {
		t.Fatal("剧本未结算")
	}
	for _, kind := range []string{"dungeon", "trpg"} {
		g = testGame(kind, 1)
		g.Pack = Pack{Title: "探险", Rooms: []Room{{ID: "a", Name: "入口", Text: "门", Links: []string{"b"}, Difficulty: 5, Success: "发现", Failure: "滑倒", Item: "工具"}, {ID: "b", Name: "大厅", Text: "大厅", Links: []string{"a", "c"}, Difficulty: 5, Success: "发现", Failure: "滑倒"}, {ID: "c", Name: "终点", Text: "终点", Links: []string{"b"}, Difficulty: 5, Success: "发现", Failure: "滑倒"}}}
		move(t, &g, 0, false, "开始游戏")
		move(t, &g, 0, false, "建角色 测试 智慧")
		for _, room := range []string{"a", "b", "c"} {
			if g.Position != room {
				move(t, &g, 0, false, "探索 "+room)
			}
			for !slices.Contains(g.Cleared, room) {
				g.Players[0].HP = 10
				move(t, &g, 0, false, "调查")
			}
		}
		if g.Status != "ended" {
			t.Fatal("探险未结算")
		}
	}
}
func TestValidationAndAuthorization(t *testing.T) {
	var p Pack
	for _, raw := range []string{`{"title":"x","text":"y"} garbage`, `{"title":"x","text":"y"}{}`, `{"title":"x","text":"y","unknown":true}`} {
		if Decode(raw, &p) == nil {
			t.Fatal("接受损坏结构", raw)
		}
	}
	g := testGame("undercover", 4)
	g.Players[1].Ready = false
	g.Pack = Pack{Title: "题", Words: []string{"甲", "乙"}}
	if _, _, e := Apply(g, Input{Actor: "p1", Text: "开始游戏"}); e == nil {
		t.Fatal("未确认就开局")
	}
	if _, _, e := Apply(g, Input{Actor: "p2", Text: "确认入局", Private: true, Friend: false}); e == nil {
		t.Fatal("非好友确认")
	}
	if _, _, e := Apply(g, Input{Actor: "stranger", Text: "确认入局", Private: true, Friend: true}); e == nil {
		t.Fatal("未报名者确认")
	}
}
func TestBidirectionalFriendReceiptChallenge(t *testing.T) {
	g := testGame("undercover", 4)
	g.Players[0].Ready = false
	out := move(t, &g, 0, true, "确认入局")
	if g.Players[0].Ready || len(out) != 1 || !out[0].Private || len(g.Players[0].Challenge) != 6 {
		t.Fatal("没有以私聊挑战验证收件")
	}
	_, _, e := Apply(g, Input{Actor: "p1", Text: "确认收件 000000", Private: true, Friend: true})
	if e == nil && g.Players[0].Challenge != "000000" {
		t.Fatal("错误验证码通过")
	}
	challenge := g.Players[0].Challenge
	if _, _, e = Apply(g, Input{Actor: "p1", Text: "确认收件 " + challenge, Private: false, Friend: true}); e == nil {
		t.Fatal("群内验证通过")
	}
	move(t, &g, 0, true, "确认收件 "+challenge)
	if !g.Players[0].Ready || g.Players[0].Challenge != "" {
		t.Fatal("正确验证码未完成确认或未销毁")
	}
}

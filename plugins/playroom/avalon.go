package playroom

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (g *Game) TeamSize() int {
	sizes := map[int][]int{5: {2, 3, 2, 3, 3}, 6: {2, 3, 4, 3, 4}, 7: {2, 3, 3, 4, 4}, 8: {3, 4, 4, 5, 5}, 9: {3, 4, 4, 5, 5}, 10: {3, 4, 4, 5, 5}}
	v := sizes[len(g.Players)]
	if len(v) == 0 || g.Round < 1 || g.Round > 5 {
		return 0
	}
	return v[g.Round-1]
}
func (g *Game) avalon(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	p := g.Players[idx]
	switch g.Phase {
	case "team":
		if idx != g.Leader || cmd != "组队" || in.Private {
			return nil, fmt.Errorf("请当前队长在群内组队")
		}
		team := []int{}
		for _, a := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '、' }) {
			i, e := ParseSeat(a, len(g.Players))
			if e != nil {
				return nil, e
			}
			if slices.Contains(team, i) {
				return nil, fmt.Errorf("队伍不能重复")
			}
			team = append(team, i)
		}
		if len(team) != g.TeamSize() {
			return nil, fmt.Errorf("本轮需要%d名队员", g.TeamSize())
		}
		g.Team = team
		g.Phase = "approval"
		g.Votes = map[string]int{}
		out := []Delivery{Public("提名队伍：" + g.teamText() + "。所有玩家私聊批准或反对。")}
		for _, q := range g.Players {
			out = append(out, Secret(q.ID, "本轮队伍："+g.teamText()+"。私聊游戏 "+g.Code+" 批准，或游戏 "+g.Code+" 反对。"))
		}
		return out, nil
	case "approval":
		if !in.Private || !slices.Contains([]string{"批准", "反对"}, cmd) {
			return nil, fmt.Errorf("请私聊批准或反对")
		}
		if _, ok := g.Votes[p.ID]; ok {
			return nil, fmt.Errorf("已提交审批")
		}
		value := 0
		if cmd == "批准" {
			value = 1
		}
		g.Votes[p.ID] = value
		if len(g.Votes) < len(g.Players) {
			return []Delivery{Secret(p.ID, "审批已记录。")}, nil
		}
		approved := 0
		for _, v := range g.Votes {
			approved += v
		}
		g.Votes = map[string]int{}
		if approved <= len(g.Players)/2 {
			g.Rejections++
			if g.Rejections >= 5 {
				return g.end("连续五次组队被拒绝，邪恶阵营获胜。\n" + g.identities()), nil
			}
			g.Leader = (g.Leader + 1) % len(g.Players)
			g.Phase = "team"
			return []Delivery{Public(fmt.Sprintf("批准%d票，组队未通过；连续拒绝%d/5次。\n%s", approved, g.Rejections, g.PhaseHelp()))}, nil
		}
		g.Rejections = 0
		g.Phase = "quest"
		out := []Delivery{Public(fmt.Sprintf("批准%d票，组队通过。等待队员秘密执行任务。", approved))}
		for _, i := range g.Team {
			out = append(out, Secret(g.Players[i].ID, "私聊「游戏 "+g.Code+" 任务 成功」或「任务 失败」；好人只能成功。"))
		}
		return out, nil
	case "quest":
		if !in.Private || cmd != "任务" || !slices.Contains(g.Team, idx) || !slices.Contains([]string{"成功", "失败"}, arg) {
			return nil, fmt.Errorf("仅本轮队员可私聊任务 成功/失败")
		}
		if _, ok := g.Votes[p.ID]; ok {
			return nil, fmt.Errorf("已经提交任务")
		}
		if arg == "失败" && !evil(p.Role) {
			return nil, fmt.Errorf("好人只能提交成功")
		}
		v := 0
		if arg == "失败" {
			v = 1
		}
		g.Votes[p.ID] = v
		if len(g.Votes) < len(g.Team) {
			return []Delivery{Secret(p.ID, "任务已记录。")}, nil
		}
		fails := 0
		for _, v := range g.Votes {
			fails += v
		}
		needed := 1
		if len(g.Players) >= 7 && g.Round == 4 {
			needed = 2
		}
		passed := fails < needed
		if passed {
			g.Successes++
		} else {
			g.Failures++
		}
		text := fmt.Sprintf("第%d次任务：%d张失败票，本轮需要%d张失败票才失败。累计成功%d，失败%d。", g.Round, fails, needed, g.Successes, g.Failures)
		g.Votes = map[string]int{}
		if g.Failures >= 3 {
			return g.end(text + "\n邪恶阵营获胜。\n" + g.identities()), nil
		}
		if g.Successes >= 3 {
			g.Phase = "assassinate"
			out := []Delivery{Public(text + "\n进入刺杀阶段。")}
			for _, q := range g.Players {
				if q.Role == "刺客" {
					out = append(out, Secret(q.ID, "私聊「游戏 "+g.Code+" 刺杀 序号」。命中梅林邪恶获胜，否则好人获胜。"))
				}
			}
			return out, nil
		}
		g.Round++
		g.Leader = (g.Leader + 1) % len(g.Players)
		g.Phase = "team"
		return []Delivery{Public(text + "\n" + g.PhaseHelp())}, nil
	case "assassinate":
		if !in.Private || p.Role != "刺客" || cmd != "刺杀" {
			return nil, fmt.Errorf("等待刺客私聊行动")
		}
		i, e := ParseSeat(arg, len(g.Players))
		if e != nil {
			return nil, e
		}
		winner := "好人阵营获胜。"
		if g.Players[i].Role == "梅林" {
			winner = "刺中梅林，邪恶阵营获胜。"
		}
		return g.end(winner + "\n" + g.identities()), nil
	}
	return nil, fmt.Errorf("当前阶段不接受此操作")
}
func (g *Game) teamText() string {
	var a []string
	for _, i := range g.Team {
		a = append(a, strconv.Itoa(i+1)+"号")
	}
	return strings.Join(a, "、")
}

package playroom

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (g *Game) wolfPrompts() []Delivery {
	out := []Delivery{Public("天黑请闭眼，等待狼人行动。")}
	for _, p := range g.Players {
		if p.Alive && p.Role == "狼人" {
			out = append(out, Secret(p.ID, "狼人阶段：私聊「游戏 "+g.Code+" 刀 序号」。可用「游戏 "+g.Code+" 狼聊 内容」与在场狼队交流。"))
		}
	}
	return out
}
func (g *Game) werewolf(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	p := &g.Players[idx]
	if cmd == "狼聊" {
		if !in.Private || p.Role != "狼人" || !p.Alive || !slices.Contains([]string{"wolves", "night"}, g.Phase) || arg == "" {
			return nil, fmt.Errorf("仅在场狼人在夜间通过私聊使用狼聊")
		}
		out := []Delivery{}
		for _, q := range g.Players {
			if q.Role == "狼人" && q.Alive {
				out = append(out, Secret(q.ID, fmt.Sprintf("狼队 %d号%s：%s", idx+1, p.Name, arg)))
			}
		}
		return out, nil
	}
	if g.Phase == "hunter" {
		if idx != g.Pending[0] || !in.Private {
			return nil, fmt.Errorf("等待猎人私聊行动")
		}
		if cmd != "开枪" && cmd != "跳过" {
			return nil, fmt.Errorf("请开枪或跳过")
		}
		out := []Delivery{}
		if cmd == "开枪" {
			target, e := ParseSeat(arg, len(g.Players))
			if e != nil {
				return nil, e
			}
			if !g.Players[target].Alive {
				return nil, fmt.Errorf("目标已出局")
			}
			g.Players[target].Alive = false
			out = append(out, Public(fmt.Sprintf("猎人带走%d号。", target+1)))
		}
		g.Pending = nil
		return append(out, g.afterDeath(g.PendingPhase)...), nil
	}
	if g.Phase == "day" && in.Actor == g.Owner && cmd == "投票" && arg == "" && !in.Private {
		g.Phase = "vote"
		g.Votes = map[string]int{}
		return []Delivery{Public(g.PhaseHelp())}, nil
	}
	if !p.Alive {
		return nil, fmt.Errorf("你已出局，请观战")
	}
	switch g.Phase {
	case "wolves":
		if !in.Private || p.Role != "狼人" || cmd != "刀" {
			return nil, fmt.Errorf("等待狼人私聊选择")
		}
		target, e := ParseSeat(arg, len(g.Players))
		if e != nil {
			return nil, e
		}
		if !g.Players[target].Alive || g.Players[target].Role == "狼人" {
			return nil, fmt.Errorf("不能选择出局者或狼队")
		}
		if _, ok := g.Votes[p.ID]; ok {
			return nil, fmt.Errorf("本夜已选择")
		}
		g.Votes[p.ID] = target
		wolves := 0
		for _, q := range g.Players {
			if q.Alive && q.Role == "狼人" {
				wolves++
			}
		}
		if len(g.Votes) < wolves {
			return []Delivery{Secret(p.ID, "已记录，等待其他狼人。")}, nil
		}
		counts := map[int]int{}
		for _, v := range g.Votes {
			counts[v]++
		}
		best, maxVotes := -1, 0
		for i := range g.Players {
			if counts[i] > maxVotes {
				best, maxVotes = i, counts[i]
			}
		}
		g.Acts = map[string]int{"kill": best}
		g.Votes = map[string]int{}
		g.Phase = "night"
		out := []Delivery{}
		for _, q := range g.Players {
			if !q.Alive {
				continue
			}
			help := ""
			switch q.Role {
			case "预言家":
				help = "验 序号"
			case "女巫":
				help = fmt.Sprintf("今晚%d号受到袭击。救药剩余：%t；毒药剩余：%t。回复「救」「毒 序号」或「跳过」。非首夜不能自救；每夜最多用一种药。", best+1, !g.HealUsed, !g.PoisonUsed)
			case "守卫":
				help = "守 序号，或跳过；不能连续两夜守同一人。"
			}
			if help != "" {
				out = append(out, Secret(q.ID, help))
			}
		}
		if g.nightReady() {
			out = append(out, g.resolveNight()...)
		}
		return out, nil
	case "night":
		if !in.Private || !slices.Contains([]string{"预言家", "女巫", "守卫"}, p.Role) {
			return nil, fmt.Errorf("等待技能角色私聊行动")
		}
		if _, ok := g.Acts[p.ID]; ok {
			return nil, fmt.Errorf("本夜已行动")
		}
		out := []Delivery{}
		switch p.Role {
		case "预言家":
			if cmd != "验" {
				return nil, fmt.Errorf("请输入验 序号")
			}
			i, e := ParseSeat(arg, len(g.Players))
			if e != nil {
				return nil, e
			}
			if !g.Players[i].Alive || i == idx {
				return nil, fmt.Errorf("请选择其他在场玩家")
			}
			alignment := "好人"
			if g.Players[i].Role == "狼人" {
				alignment = "狼人"
			}
			out = append(out, Secret(p.ID, fmt.Sprintf("查验结果：%d号是%s。", i+1, alignment)))
			g.Acts[p.ID] = i
		case "女巫":
			switch cmd {
			case "救":
				if g.HealUsed || g.Round > 1 && g.Acts["kill"] == idx {
					return nil, fmt.Errorf("救药不可用或非首夜不能自救")
				}
				g.HealUsed = true
				g.Acts["heal"] = 1
			case "毒":
				if g.PoisonUsed {
					return nil, fmt.Errorf("毒药已用完")
				}
				i, e := ParseSeat(arg, len(g.Players))
				if e != nil {
					return nil, e
				}
				if !g.Players[i].Alive || i == idx {
					return nil, fmt.Errorf("请选择其他在场玩家")
				}
				g.PoisonUsed = true
				g.Acts["poison"] = i
			case "跳过":
			default:
				return nil, fmt.Errorf("请救、毒 序号或跳过")
			}
			g.Acts[p.ID] = 1
		case "守卫":
			i := -1
			if cmd != "跳过" {
				if cmd != "守" {
					return nil, fmt.Errorf("请守 序号或跳过")
				}
				var e error
				i, e = ParseSeat(arg, len(g.Players))
				if e != nil {
					return nil, e
				}
				if !g.Players[i].Alive || i == g.LastGuard {
					return nil, fmt.Errorf("目标出局或连续守护同一人")
				}
			}
			g.LastGuard = i
			g.Acts["guard"] = i
			g.Acts[p.ID] = i
		}
		out = append(out, Secret(p.ID, "行动已记录。"))
		if g.nightReady() {
			out = append(out, g.resolveNight()...)
		}
		return out, nil
	case "day":
		if cmd == "发言" && arg != "" && !in.Private {
			return []Delivery{Public("已记录讨论。房主发送「投票」结束讨论。")}, nil
		}
		if cmd == "投票" && arg == "" && in.Actor == g.Owner && !in.Private {
			g.Phase = "vote"
			g.Votes = map[string]int{}
			return []Delivery{Public(g.PhaseHelp())}, nil
		}
		return nil, fmt.Errorf("请公开发言，或由房主发送投票")
	case "vote":
		if cmd != "投票" {
			return nil, fmt.Errorf("请投票 序号")
		}
		target, done, e := g.vote(idx, arg)
		if e != nil {
			return nil, e
		}
		if !done {
			return []Delivery{g.reply(in, "投票已记录。")}, nil
		}
		if target < 0 {
			if g.Ties == 1 {
				return []Delivery{Public("平票，重投一次。")}, nil
			}
			g.Ties = 0
			return append([]Delivery{Public("再次平票，无人出局。")}, g.afterDeath("wolves")...), nil
		}
		g.Players[target].Alive = false
		out := []Delivery{Public(fmt.Sprintf("%d号被放逐。", target+1))}
		if g.Players[target].Role == "猎人" {
			g.Phase = "hunter"
			g.Pending = []int{target}
			g.PendingPhase = "wolves"
			return append(out, Secret(g.Players[target].ID, "你是猎人，可私聊开枪 序号或跳过。")), nil
		}
		return append(out, g.afterDeath("wolves")...), nil
	}
	return nil, fmt.Errorf("当前阶段不接受此操作")
}
func (g *Game) nightReady() bool {
	for _, p := range g.Players {
		if p.Alive && slices.Contains([]string{"预言家", "女巫", "守卫"}, p.Role) {
			if _, ok := g.Acts[p.ID]; !ok {
				return false
			}
		}
	}
	return true
}
func (g *Game) resolveNight() []Delivery {
	deaths := []int{}
	kill := g.Acts["kill"]
	guard, guarded := g.Acts["guard"]
	heal := g.Acts["heal"] == 1 // 同守同救视为救援冲突，仍受刀伤；这是本基础板子的固定规则。
	if (!heal && (!guarded || guard != kill)) || (heal && guarded && guard == kill) {
		deaths = append(deaths, kill)
	}
	poison, poisoned := g.Acts["poison"]
	if poisoned && !slices.Contains(deaths, poison) {
		deaths = append(deaths, poison)
	}
	var names []string
	hunter := -1
	for _, i := range deaths {
		g.Players[i].Alive = false
		names = append(names, strconv.Itoa(i+1)+"号")
		if g.Players[i].Role == "猎人" && (!poisoned || poison != i) {
			hunter = i
		}
	}
	text := "天亮了，昨夜平安。"
	if len(names) > 0 {
		text = "天亮了，昨夜出局：" + strings.Join(names, "、") + "。"
	}
	out := []Delivery{Public(text)}
	if hunter >= 0 {
		g.Phase = "hunter"
		g.Pending = []int{hunter}
		g.PendingPhase = "day"
		return append(out, Secret(g.Players[hunter].ID, "猎人请私聊开枪 序号或跳过；被毒出局不能开枪。"))
	}
	return append(out, g.afterDeath("day")...)
}
func (g *Game) afterDeath(next string) []Delivery {
	wolves, good := 0, 0
	for _, p := range g.Players {
		if p.Alive {
			if p.Role == "狼人" {
				wolves++
			} else {
				good++
			}
		}
	}
	if wolves == 0 {
		return g.end("好人获胜！\n" + g.identities())
	}
	if wolves >= good {
		return g.end("狼人达到人数优势，狼人获胜！\n" + g.identities())
	}
	g.Phase = next
	g.Votes = map[string]int{}
	g.Acts = map[string]int{}
	if next == "wolves" {
		g.Round++
		return g.wolfPrompts()
	}
	return []Delivery{Public("进入白天讨论。\n" + g.Instructions())}
}

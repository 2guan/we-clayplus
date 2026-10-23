package playroom

import (
	"fmt"
	"slices"
	"strings"
)

func (g *Game) cluePlaces() string {
	var a []string
	for _, c := range g.Pack.Clues {
		if !slices.Contains(a, c.Place) {
			a = append(a, c.Place)
		}
	}
	return strings.Join(a, "、")
}
func (g *Game) murder(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	if cmd == "揭晓" {
		if in.Actor != g.Owner {
			return nil, fmt.Errorf("仅房主可以揭晓并结束")
		}
		return g.end(fmt.Sprintf("凶手是%d号。\n%s\n%s", g.Pack.Culprit, g.Pack.Solution, g.identities())), nil
	}
	switch g.Phase {
	case "investigate":
		if cmd == "指认" && arg == "" && in.Actor == g.Owner {
			g.Phase = "accuse"
			g.Votes = map[string]int{}
			return []Delivery{Public("进入最终指认，每位玩家回复「指认 序号」。")}, nil
		}
		if cmd == "发言" && arg != "" {
			return []Delivery{g.reply(in, "已记录讨论。房主可发送「指认」开始最终投票。")}, nil
		}
		if cmd != "搜证" {
			return nil, fmt.Errorf("请搜证 地点，或发言 内容")
		}
		out := []Delivery{}
		for _, c := range g.Pack.Clues {
			key := c.ID
			if c.Owner > 0 {
				if c.Owner != idx+1 {
					continue
				}
				key = fmt.Sprintf("%s:%s", c.ID, in.Actor)
			}
			if c.Place != arg || slices.Contains(g.Found, key) {
				continue
			}
			g.Found = append(g.Found, key)
			text := "线索「" + c.ID + "」：" + c.Text
			if c.Owner > 0 {
				out = append(out, Secret(in.Actor, text))
			} else {
				out = append(out, Public(text))
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("此处没有你尚未获取的线索。地点：%s", g.cluePlaces())
		}
		return out, nil
	case "accuse":
		if cmd != "指认" {
			return nil, fmt.Errorf("请指认 序号")
		}
		i, e := ParseSeat(arg, len(g.Players))
		if e != nil {
			return nil, e
		}
		if _, ok := g.Votes[in.Actor]; ok {
			return nil, fmt.Errorf("已经提交指认")
		}
		g.Votes[in.Actor] = i
		if len(g.Votes) < len(g.Players) {
			return []Delivery{g.reply(in, "指认已记录，等待其他玩家。")}, nil
		}
		lines := []string{}
		for _, p := range g.Players {
			result := "未猜中"
			if g.Votes[p.ID]+1 == g.Pack.Culprit {
				result = "猜中"
			}
			lines = append(lines, p.Name+"："+result)
		}
		return g.end(fmt.Sprintf("凶手是%d号。\n%s\n%s", g.Pack.Culprit, g.Pack.Solution, strings.Join(lines, "\n"))), nil
	}
	return nil, fmt.Errorf("当前阶段不接受此操作")
}
func (g *Game) quiz(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	if cmd != "答" {
		return nil, fmt.Errorf("请答 A/B/C/D")
	}
	if g.Kind == "chemistry" && !in.Private {
		return nil, fmt.Errorf("请通过好友私聊秘密作答")
	}
	answer := strings.ToUpper(strings.TrimSpace(arg))
	i := strings.Index("ABCD", answer)
	if len(answer) != 1 || i < 0 {
		return nil, fmt.Errorf("选项为A、B、C或D")
	}
	if _, ok := g.Answers[in.Actor]; ok {
		return nil, fmt.Errorf("本题已回答")
	}
	g.Answers[in.Actor] = i
	g.Players[idx].Score += g.Pack.Questions[g.Question].Scores[i]
	if len(g.Answers) < len(g.Players) {
		return []Delivery{Secret(in.Actor, "答案已保存，全部提交后统一揭晓。")}, nil
	}
	out := []Delivery{}
	if g.Kind == "chemistry" {
		counts := map[int]int{}
		var lines []string
		for _, p := range g.Players {
			a := g.Answers[p.ID]
			counts[a]++
			lines = append(lines, p.Name+"："+string(rune('A'+a)))
		}
		best := 0
		for _, n := range counts {
			if n > best {
				best = n
			}
		}
		g.Successes += best
		g.History = append(g.History, Line{Speaker: "本轮答案", Text: strings.Join(lines, "；")})
		out = append(out, Public("本题揭晓：\n"+strings.Join(lines, "\n")))
	}
	g.Question++
	g.Answers = map[string]int{}
	if g.Question == len(g.Pack.Questions) {
		text := "测试结果\n"
		if g.Kind == "chemistry" {
			score := 100 * g.Successes / (len(g.Players) * len(g.Pack.Questions))
			text += fmt.Sprintf("本次答题默契度：%d%%（选项相似度）。", score)
		} else {
			p := g.Players[0]
			ratio := 100 * p.Score / (3 * len(g.Pack.Questions))
			label := "随性探索型"
			if ratio >= 67 {
				label = "果断行动型"
			} else if ratio >= 34 {
				label = "平衡观察型"
			}
			text += fmt.Sprintf("%s：%s，答题行动倾向%d/100。", p.Name, label, ratio)
		}
		return append(out, g.end(text)...), nil
	}
	return append(out, Public(g.QuestionText())), nil
}
func (g *Game) room() Room {
	for _, r := range g.Pack.Rooms {
		if r.ID == g.Position {
			return r
		}
	}
	return Room{}
}
func (g *Game) roomText() string {
	r := g.room()
	return "【" + r.Name + " · " + r.ID + "】\n" + r.Text + "\n可前往：" + strings.Join(r.Links, "、") + fmt.Sprintf("\n难度%d；敌人生命%d。", r.Difficulty, max(0, r.Enemy-g.Acts["damage:"+r.ID]))
}
func (g *Game) adventure(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	p := &g.Players[idx]
	if g.Acts == nil {
		g.Acts = map[string]int{}
	}
	r := g.room()
	reply := func(t string) ([]Delivery, error) { return []Delivery{g.reply(in, t)}, nil }
	switch cmd {
	case "建角色":
		if g.Round > 1 || g.Players[idx].Skill != "" {
			return nil, fmt.Errorf("角色只能在首次行动前建立")
		}
		fields := strings.Fields(arg)
		if len(fields) != 2 || len([]rune(fields[0])) > 30 || !slices.Contains([]string{"力量", "敏捷", "智慧"}, fields[1]) {
			return nil, fmt.Errorf("格式：建角色 名字 力量/敏捷/智慧")
		}
		p.Name = fields[0]
		p.Skill = fields[1]
		return reply("角色已建立，生命10；擅长" + p.Skill + "，对应检定+2。")
	case "角色":
		return reply(fmt.Sprintf("%s · 生命%d/10 · 擅长%s", p.Name, p.HP, p.Skill))
	case "背包":
		return reply("背包：" + strings.Join(p.Items, "、"))
	case "地图":
		var lines []string
		for _, room := range g.Pack.Rooms {
			state := "未探索"
			if slices.Contains(g.Explored, room.ID) {
				state = "已探索"
			}
			lines = append(lines, room.ID+" "+room.Name+"（"+state+"）→"+strings.Join(room.Links, "、"))
		}
		return reply(strings.Join(lines, "\n"))
	}
	if p.HP <= 0 {
		return nil, fmt.Errorf("角色已倒下，等待其他玩家完成副本或房主结束")
	}
	if cmd == "探索" {
		if !slices.Contains(r.Links, arg) {
			return nil, fmt.Errorf("只能前往相邻地点：%s", strings.Join(r.Links, "、"))
		}
		if r.Enemy > g.Acts["damage:"+r.ID] {
			return nil, fmt.Errorf("当前敌人尚未击败，不能离开")
		}
		g.Position = arg
		if !slices.Contains(g.Explored, arg) {
			g.Explored = append(g.Explored, arg)
		}
		g.Round++
		return reply(g.roomText())
	}
	if cmd == "休息" {
		key := "rest:" + g.Position + ":" + p.ID
		if slices.Contains(g.Found, key) {
			return nil, fmt.Errorf("此处已经休息过")
		}
		if r.Enemy > g.Acts["damage:"+r.ID] {
			return nil, fmt.Errorf("有敌人时不能休息")
		}
		g.Found = append(g.Found, key)
		p.HP = min(10, p.HP+3)
		return reply(fmt.Sprintf("恢复3点生命，当前%d/10。", p.HP))
	}
	if cmd == "使用" {
		i := slices.Index(p.Items, arg)
		if i < 0 {
			return nil, fmt.Errorf("背包中没有此物品")
		}
		if g.Acts["boost:"+p.ID] > 0 {
			return nil, fmt.Errorf("已有待用物品加成，请先完成一次检定或战斗")
		}
		p.Items = append(p.Items[:i], p.Items[i+1:]...)
		g.Acts["boost:"+p.ID] = 3
		return reply("消耗物品，下次检定或战斗+3。")
	}
	if !slices.Contains([]string{"调查", "检定", "战斗"}, cmd) {
		return nil, fmt.Errorf("请使用地图、探索 地点ID、调查、检定 技能、战斗、使用 物品或休息")
	}
	if cmd != "战斗" && slices.Contains(g.Cleared, r.ID) {
		return nil, fmt.Errorf("此地已经调查完成")
	}
	if cmd == "战斗" && r.Enemy <= g.Acts["damage:"+r.ID] {
		return nil, fmt.Errorf("此处没有存活敌人")
	}
	skill := "智慧"
	threshold := r.Difficulty
	if cmd == "检定" {
		if !slices.Contains([]string{"力量", "敏捷", "智慧"}, arg) {
			return nil, fmt.Errorf("检定技能为力量、敏捷或智慧")
		}
		skill = arg
	}
	if cmd == "战斗" {
		skill = "力量"
		threshold = 10
	}
	die := Roll(20)
	bonus := g.Acts["boost:"+p.ID]
	g.Acts["boost:"+p.ID] = 0
	if p.Skill == skill {
		bonus += 2
	}
	passed := die+bonus >= threshold
	result := fmt.Sprintf("%s：d20=%d，加成%d，目标%d。", p.Name, die, bonus, threshold)
	g.Round++
	if passed {
		if cmd == "战斗" {
			damage := Roll(6)
			g.Acts["damage:"+r.ID] += damage
			result += fmt.Sprintf("命中，造成%d伤害，敌人剩余%d生命。", damage, max(0, r.Enemy-g.Acts["damage:"+r.ID]))
		} else {
			g.Cleared = append(g.Cleared, r.ID)
			result += "成功！" + r.Success
			if r.Item != "" {
				p.Items = append(p.Items, r.Item)
				result += "获得物品：" + r.Item
			}
		}
	} else {
		damage := Roll(4)
		p.HP = max(0, p.HP-damage)
		p.Alive = p.HP > 0
		result += fmt.Sprintf("失败，受到%d伤害，剩余%d生命。%s", damage, p.HP, r.Failure)
	}
	alive := false
	for _, q := range g.Players {
		alive = alive || q.HP > 0
	}
	if !alive {
		return g.end(result + "\n全队倒下，冒险结束。"), nil
	}
	allClear := len(g.Cleared) == len(g.Pack.Rooms)
	for _, room := range g.Pack.Rooms {
		if room.Enemy > g.Acts["damage:"+room.ID] {
			allClear = false
		}
	}
	if allClear {
		return g.end(result + "\n所有地点调查与战斗完成，冒险通关！"), nil
	}
	return reply(result)
}

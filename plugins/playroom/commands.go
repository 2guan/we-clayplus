package playroom

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Apply 在副本上执行；非法操作不会留下部分变更。
func Apply(g Game, in Input) (Game, []Delivery, error) {
	var next Game
	b, err := json.Marshal(g)
	if err != nil {
		return g, nil, err
	}
	if err = json.Unmarshal(b, &next); err != nil {
		return g, nil, err
	}
	out, err := next.apply(in)
	if err != nil {
		return g, nil, err
	}
	return next, out, nil
}
func (g *Game) apply(in Input) ([]Delivery, error) {
	if g.Votes == nil {
		g.Votes = map[string]int{}
	}
	if g.Acts == nil {
		g.Acts = map[string]int{}
	}
	if g.Answers == nil {
		g.Answers = map[string]int{}
	}
	text := strings.TrimSpace(in.Text)
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	idx := g.Index(in.Actor)
	if cmd == "怎么玩" || cmd == "进度" {
		return []Delivery{g.reply(in, g.Instructions())}, nil
	}
	if cmd == "结束游戏" || cmd == "暂停游戏" || cmd == "继续游戏" {
		if in.Actor != g.Owner {
			return nil, fmt.Errorf("请由房主操作")
		}
		if g.Status == "ended" {
			return nil, fmt.Errorf("本局已结束")
		}
		g.Status = map[string]string{"结束游戏": "ended", "暂停游戏": "paused", "继续游戏": "playing"}[cmd]
		if cmd == "继续游戏" && g.Phase == "" {
			g.Status = "lobby"
		}
		return []Delivery{Public(cmd + "。进度已保存。")}, nil
	}
	if g.Status == "paused" {
		return nil, fmt.Errorf("本局暂停中，请房主发送继续游戏")
	}
	if g.Status == "ended" {
		return nil, fmt.Errorf("本局已结束")
	}
	if cmd == "加入" {
		if g.Status != "lobby" {
			return nil, fmt.Errorf("已经开局，不能中途加入")
		}
		if idx >= 0 {
			return []Delivery{g.reply(in, "你已报名。私聊「确认入局 "+g.Code+"」完成可达性确认。")}, nil
		}
		d, _ := Find(g.Kind)
		if len(g.Players) >= d.Max || g.Seats > 0 && len(g.Players) >= g.Seats {
			return nil, fmt.Errorf("人数已满")
		}
		g.Players = append(g.Players, Player{ID: in.Actor, Name: in.Name, Alive: true, HP: 10, Ready: !d.Secret})
		if !d.Secret {
			return []Delivery{Public(in.Name + "已报名。等待房主开始游戏。")}, nil
		}
		return []Delivery{Public(in.Name + "已报名。请先加本人为好友，再私聊「确认入局 " + g.Code + "」。")}, nil
	}
	if idx < 0 {
		return nil, fmt.Errorf("你尚未在原会话报名")
	}
	if cmd == "确认入局" {
		if !in.Private || !in.Friend || g.Status != "lobby" {
			return nil, fmt.Errorf("请使用好友私聊确认，且需先在原群报名")
		}
		if g.Players[idx].Ready {
			return []Delivery{Secret(in.Actor, "你已完成双向私聊确认，等待房主开局。")}, nil
		}
		code := fmt.Sprintf("%06d", Roll(1000000)-1)
		g.Players[idx].Challenge = code
		return []Delivery{Secret(in.Actor, "收件验证码："+code+"。请回复「确认收件 "+g.Code+" "+code+"」完成双向确认，不要把验证码发到群里。")}, nil
	}
	if cmd == "确认收件" {
		p := &g.Players[idx]
		if !in.Private || !in.Friend || g.Status != "lobby" || p.Challenge == "" || arg != p.Challenge {
			return nil, fmt.Errorf("收件验证码无效，请在好友私聊中重新确认入局")
		}
		p.Ready = true
		p.Challenge = ""
		return []Delivery{Secret(in.Actor, "双向私聊确认成功，等待房主开局。"), Public(in.Name + "已完成双向私聊确认。")}, nil
	}
	if cmd == "退出" {
		if g.Status != "lobby" {
			return nil, fmt.Errorf("开局后离开请让房主暂停或结束，避免身份和人数变化")
		}
		if in.Actor == g.Owner {
			return nil, fmt.Errorf("房主请使用结束游戏")
		}
		g.Players = append(g.Players[:idx], g.Players[idx+1:]...)
		return []Delivery{Public(in.Name + "退出报名。")}, nil
	}
	if cmd == "开始游戏" {
		if in.Actor != g.Owner || g.Status != "lobby" {
			return nil, fmt.Errorf("请房主在报名阶段开局")
		}
		return g.start()
	}
	if g.Status == "lobby" {
		return nil, fmt.Errorf("仍在报名阶段，确认后请房主开始游戏")
	}
	switch g.Kind {
	case "undercover":
		return g.undercover(in, idx, cmd, arg)
	case "werewolf":
		return g.werewolf(in, idx, cmd, arg)
	case "avalon":
		return g.avalon(in, idx, cmd, arg)
	case "murder":
		return g.murder(in, idx, cmd, arg)
	case "chemistry", "personality":
		return g.quiz(in, idx, cmd, arg)
	case "dungeon", "trpg":
		return g.adventure(in, idx, cmd, arg)
	}
	return nil, fmt.Errorf("当前玩法由剧情服务处理")
}
func (g *Game) reply(in Input, text string) Delivery {
	if in.Private {
		return Secret(in.Actor, text)
	}
	return Public(text)
}
func (g *Game) start() ([]Delivery, error) {
	d, _ := Find(g.Kind)
	n := len(g.Players)
	if n < d.Min || n > d.Max || g.Seats > 0 && n != g.Seats {
		return nil, fmt.Errorf("需要%d—%d人；指定人数为%d，当前%d人", d.Min, d.Max, g.Seats, n)
	}
	for _, p := range g.Players {
		if !p.Ready {
			return nil, fmt.Errorf("%s尚未完成好友私聊确认", p.Name)
		}
	}
	if slices.Contains([]string{"undercover", "murder", "chemistry", "personality", "dungeon", "trpg"}, g.Kind) {
		if e := Validate(g.Kind, g.Pack, n); e != nil {
			return nil, e
		}
	}
	g.Status = "playing"
	g.Round = 1
	g.Votes = map[string]int{}
	g.Acts = map[string]int{}
	g.Answers = map[string]int{}
	for i := range g.Players {
		g.Players[i].Alive = true
		g.Players[i].HP = 10
	}
	out := []Delivery{}
	switch g.Kind {
	case "undercover":
		indices := make([]int, n)
		for i := range indices {
			indices[i] = i
		}
		shuffle(indices)
		wolves := 1
		if n >= 8 {
			wolves = 2
		}
		for i := range g.Players {
			g.Players[i].Role = "平民"
			g.Players[i].Word = g.Pack.Words[0]
		}
		for _, i := range indices[:wolves] {
			g.Players[i].Role = "卧底"
			g.Players[i].Word = g.Pack.Words[1]
		}
		for _, p := range g.Players {
			out = append(out, Secret(p.ID, "你的词语："+p.Word+"。不要发到群里，也不要直接说出词语。身份不公开。"))
		}
		g.Phase = "speech"
	case "werewolf":
		roles := []string{"狼人", "狼人", "预言家", "女巫"}
		if n >= 9 {
			roles = append(roles, "狼人")
		}
		if n >= 8 {
			roles = append(roles, "猎人")
		}
		if n >= 10 {
			roles = append(roles, "守卫")
		}
		for len(roles) < n {
			roles = append(roles, "村民")
		}
		shuffle(roles)
		for i := range g.Players {
			g.Players[i].Role = roles[i]
		}
		for i, p := range g.Players {
			text := "你的身份：" + p.Role + "。"
			if p.Role == "狼人" {
				text += "狼人同伴：" + g.roleSeats("狼人", false)
			}
			out = append(out, Secret(g.Players[i].ID, text))
		}
		g.LastGuard = -1
		g.Phase = "wolves"
		out = append(out, g.wolfPrompts()...)
	case "avalon":
		bad := map[int]int{5: 2, 6: 2, 7: 3, 8: 3, 9: 3, 10: 4}[n]
		roles := []string{"梅林", "刺客"}
		if n >= 7 {
			roles = append(roles, "派西维尔", "莫甘娜")
		}
		for countRole(roles, true) < bad {
			roles = append(roles, "爪牙")
		}
		for len(roles) < n {
			roles = append(roles, "忠臣")
		}
		shuffle(roles)
		for i := range g.Players {
			g.Players[i].Role = roles[i]
		}
		for _, p := range g.Players {
			text := "你的身份：" + p.Role + "。"
			if p.Role == "梅林" || evil(p.Role) {
				text += "邪恶阵营序号：" + g.evilSeats()
			}
			if p.Role == "派西维尔" {
				var seats []string
				for i, q := range g.Players {
					if q.Role == "梅林" || q.Role == "莫甘娜" {
						seats = append(seats, strconv.Itoa(i+1))
					}
				}
				text += "梅林与莫甘娜是以下两人，无法分辨：" + strings.Join(seats, "、")
			}
			out = append(out, Secret(p.ID, text))
		}
		g.Phase = "team"
		g.Leader = 0
	case "murder":
		for i := range g.Players {
			g.Players[i].Role = g.Pack.Characters[i].Name
			c := g.Pack.Characters[i]
			out = append(out, Secret(g.Players[i].ID, "角色："+c.Name+"\n秘密："+c.Secret+"\n目标："+c.Goal))
		}
		g.Phase = "investigate"
		out = append(out, Public(g.Pack.Title+"\n"+g.Pack.Text+"\n可搜证地点："+g.cluePlaces()))
	case "chemistry", "personality":
		g.Phase = "quiz"
		out = append(out, Public(g.Pack.Title+"\n"+g.QuestionText()))
	case "dungeon", "trpg":
		g.Phase = "explore"
		g.Position = g.Pack.Rooms[0].ID
		g.Explored = []string{g.Position}
		out = append(out, Public(g.Pack.Title+"\n"+g.Pack.Text+"\n"+g.roomText()+"\n"+g.PhaseHelp()))
	}
	if d.Secret && g.Kind != "chemistry" {
		out = append(out, Public(g.Instructions()))
	}
	return out, nil
}
func countRole(roles []string, bad bool) int {
	n := 0
	for _, r := range roles {
		if evil(r) == bad {
			n++
		}
	}
	return n
}
func evil(role string) bool { return slices.Contains([]string{"刺客", "莫甘娜", "爪牙"}, role) }
func (g *Game) evilSeats() string {
	var a []string
	for i, p := range g.Players {
		if evil(p.Role) {
			a = append(a, strconv.Itoa(i+1))
		}
	}
	return strings.Join(a, "、")
}
func (g *Game) roleSeats(role string, alive bool) string {
	var a []string
	for i, p := range g.Players {
		if p.Role == role && (!alive || p.Alive) {
			a = append(a, strconv.Itoa(i+1))
		}
	}
	return strings.Join(a, "、")
}
func (g *Game) alive() int {
	n := 0
	for _, p := range g.Players {
		if p.Alive {
			n++
		}
	}
	return n
}
func (g *Game) end(text string) []Delivery {
	g.Status = "ended"
	g.Phase = "done"
	return []Delivery{Public(text)}
}
func (g *Game) identities() string {
	var a []string
	for i, p := range g.Players {
		a = append(a, fmt.Sprintf("%d号%s：%s", i+1, p.Name, p.Role))
	}
	return strings.Join(a, "\n")
}
func (g *Game) vote(idx int, arg string) (int, bool, error) {
	target, e := ParseSeat(arg, len(g.Players))
	if e != nil {
		return 0, false, e
	}
	if !g.Players[idx].Alive || !g.Players[target].Alive || target == idx {
		return 0, false, fmt.Errorf("仅在场玩家可投票，不可投自己或出局玩家")
	}
	if _, ok := g.Votes[g.Players[idx].ID]; ok {
		return 0, false, fmt.Errorf("本轮已经投票")
	}
	g.Votes[g.Players[idx].ID] = target
	if len(g.Votes) < g.alive() {
		return 0, false, nil
	}
	counts := map[int]int{}
	for _, v := range g.Votes {
		counts[v]++
	}
	best, maxVotes, tie := -1, 0, false
	for i := range g.Players {
		v := counts[i]
		if v > maxVotes {
			best, maxVotes, tie = i, v, false
		} else if v == maxVotes && v > 0 {
			tie = true
		}
	}
	g.Votes = map[string]int{}
	if tie {
		g.Ties++
		return -1, true, nil
	}
	g.Ties = 0
	return best, true, nil
}
func (g *Game) undercover(in Input, idx int, cmd, arg string) ([]Delivery, error) {
	p := &g.Players[idx]
	if g.Phase == "guess" {
		if !in.Private || p.Role != "卧底" || p.Alive || len(g.Pending) != 1 || g.Pending[0] != idx {
			return nil, fmt.Errorf("等待出局卧底私聊猜词")
		}
		if cmd != "猜词" && cmd != "跳过" {
			return nil, fmt.Errorf("请猜词或跳过")
		}
		if cmd == "猜词" && strings.TrimSpace(arg) == g.Pack.Words[0] {
			return g.end("卧底猜中平民词，卧底获胜！\n" + g.wordReveal()), nil
		}
		return g.undercoverNext(), nil
	}
	if !p.Alive {
		return nil, fmt.Errorf("你已出局，请观战")
	}
	switch {
	case g.Phase == "speech" && cmd == "发言":
		if in.Private {
			return nil, fmt.Errorf("描述请发在原群")
		}
		if arg == "" || strings.Contains(arg, p.Word) {
			return nil, fmt.Errorf("描述不能为空或直接包含自己的词语")
		}
		if p.Spoke {
			return nil, fmt.Errorf("本轮已经描述")
		}
		p.Spoke = true
		for _, q := range g.Players {
			if q.Alive && !q.Spoke {
				return []Delivery{Public("已记录描述，等待其他玩家。")}, nil
			}
		}
		g.Phase = "vote"
		return []Delivery{Public("描述结束。\n" + g.PhaseHelp())}, nil
	case g.Phase == "vote" && cmd == "投票":
		target, done, e := g.vote(idx, arg)
		if e != nil {
			return nil, e
		}
		if !done {
			return []Delivery{g.reply(in, "投票已记录。")}, nil
		}
		if target < 0 {
			if g.Ties == 1 {
				return []Delivery{Public("平票，本轮重投一次。")}, nil
			}
			g.Ties = 0
			return append([]Delivery{Public("再次平票，本轮无人出局。")}, g.undercoverNext()...), nil
		}
		g.Players[target].Alive = false
		if g.Players[target].Role == "卧底" {
			g.Phase = "guess"
			g.Pending = []int{target}
			return []Delivery{Public(fmt.Sprintf("%d号出局，是卧底；等待其私聊猜平民词。", target+1)), Secret(g.Players[target].ID, "最后机会：猜词 词语，或跳过。")}, nil
		}
		return append([]Delivery{Public(fmt.Sprintf("%d号出局，是平民。", target+1))}, g.undercoverNext()...), nil
	}
	return nil, fmt.Errorf("当前阶段请使用：%s", g.PhaseHelp())
}
func (g *Game) wordReveal() string {
	return "平民词：" + g.Pack.Words[0] + "；卧底词：" + g.Pack.Words[1] + "\n" + g.identities()
}
func (g *Game) undercoverNext() []Delivery {
	g.Pending = nil
	good, bad := 0, 0
	for _, p := range g.Players {
		if p.Alive {
			if p.Role == "卧底" {
				bad++
			} else {
				good++
			}
		}
	}
	if bad == 0 {
		return g.end("平民获胜！\n" + g.wordReveal())
	}
	if bad >= good {
		return g.end("卧底获胜！\n" + g.wordReveal())
	}
	g.Round++
	g.Phase = "speech"
	for i := range g.Players {
		g.Players[i].Spoke = false
	}
	return []Delivery{Public(g.Instructions())}
}

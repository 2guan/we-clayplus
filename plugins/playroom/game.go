// Package playroom 保存互动游戏的确定性规则；身份、数值和胜负不交给模型裁决。
package playroom

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

type Definition struct {
	ID, Name, Trigger string
	Min, Max          int
	GroupOnly, Secret bool
}

var Definitions = []Definition{
	{"undercover", "谁是卧底", "玩谁是卧底", 4, 12, true, true},
	{"werewolf", "狼人杀", "开狼人杀", 6, 12, true, true},
	{"avalon", "阿瓦隆／阵营推理", "玩阵营推理", 5, 10, true, true},
	{"murder", "剧本杀", "开剧本杀", 3, 8, true, true},
	{"chemistry", "默契测试", "测默契", 2, 8, false, true},
	{"romance", "心动剧场", "心动剧场", 1, 1, false, false},
	{"radio", "深夜电台／树洞", "打开深夜电台", 1, 1, false, false},
	{"dungeon", "每日随机副本", "来个副本", 1, 6, false, false},
	{"trpg", "世界探险／文字跑团", "开始跑团", 1, 6, false, false},
	{"fortune", "每日运势／抽签", "抽个今日签", 1, 1, false, false},
	{"personality", "趣味性格测试", "测测我的", 1, 1, false, false},
	{"group_digest", "群聊日报／周报", "生成今天的群日报", 1, 1, true, false},
	{"group_memes", "群梗博物馆", "收藏这个梗", 1, 1, true, false},
}

func Find(id string) (Definition, bool) {
	for _, d := range Definitions {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}
func Match(text string) (string, string) {
	text = strings.TrimSpace(text)
	for _, d := range Definitions {
		if d.ID == "personality" && strings.HasPrefix(text, d.Trigger) {
			return d.ID, strings.TrimSpace(strings.TrimPrefix(text, d.Trigger))
		}
		if text == d.Trigger || strings.HasPrefix(text, d.Trigger+" ") {
			return d.ID, strings.TrimSpace(strings.TrimPrefix(text, d.Trigger))
		}
	}
	for _, a := range []struct{ word, id string }{{"开阿瓦隆", "avalon"}, {"玩阿瓦隆", "avalon"}, {"生成本周的群周报", "group_digest"}, {"生成今天的群日报", "group_digest"}, {"查看群梗", "group_memes"}, {"随机群梗", "group_memes"}} {
		if text == a.word || strings.HasPrefix(text, a.word+" ") {
			return a.id, strings.TrimSpace(strings.TrimPrefix(text, a.word)) + map[bool]string{true: "周报", false: ""}[strings.Contains(a.word, "周报")]
		}
	}
	return "", ""
}
func IsCommand(text string) bool {
	head := strings.Fields(text)
	if len(head) == 0 {
		return false
	}
	return slices.Contains([]string{"加入", "退出", "开始游戏", "结束游戏", "暂停游戏", "继续游戏", "进度", "怎么玩", "发言", "投票", "猜词", "组队", "批准", "反对", "任务", "刺杀", "刀", "验", "救", "毒", "守", "开枪", "跳过", "狼聊", "搜证", "指认", "揭晓", "答", "建角色", "角色", "地图", "背包", "探索", "调查", "检定", "战斗", "使用", "休息", "选择", "重试本轮"}, head[0])
}

type Player struct {
	Challenge string   `json:"challenge,omitempty"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Ready     bool     `json:"ready"`
	Alive     bool     `json:"alive"`
	Role      string   `json:"role,omitempty"`
	Word      string   `json:"word,omitempty"`
	Spoke     bool     `json:"spoke"`
	HP        int      `json:"hp"`
	Skill     string   `json:"skill,omitempty"`
	Items     []string `json:"items,omitempty"`
	Score     int      `json:"score"`
}
type Question struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
	Scores  []int    `json:"scores"`
}
type Character struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
	Goal   string `json:"goal"`
}
type Clue struct {
	ID    string `json:"id"`
	Place string `json:"place"`
	Text  string `json:"text"`
	Owner int    `json:"owner"`
}
type Room struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Text       string   `json:"text"`
	Links      []string `json:"links"`
	Difficulty int      `json:"difficulty"`
	Success    string   `json:"success"`
	Failure    string   `json:"failure"`
	Item       string   `json:"item"`
	Enemy      int      `json:"enemy"`
}
type Pack struct {
	Title      string      `json:"title"`
	Text       string      `json:"text"`
	Words      []string    `json:"words,omitempty"`
	Characters []Character `json:"characters,omitempty"`
	Clues      []Clue      `json:"clues,omitempty"`
	Culprit    int         `json:"culprit,omitempty"`
	Solution   string      `json:"solution,omitempty"`
	Questions  []Question  `json:"questions,omitempty"`
	Rooms      []Room      `json:"rooms,omitempty"`
	Choices    []string    `json:"choices,omitempty"`
}
type Line struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}
type Game struct {
	ID           string         `json:"id"`
	Code         string         `json:"code"`
	Kind         string         `json:"kind"`
	Target       string         `json:"target"`
	Owner        string         `json:"owner"`
	Theme        string         `json:"theme"`
	Status       string         `json:"status"`
	Phase        string         `json:"phase"`
	Round        int            `json:"round"`
	Seats        int            `json:"seats"`
	Players      []Player       `json:"players"`
	Pack         Pack           `json:"pack"`
	Votes        map[string]int `json:"votes,omitempty"`
	Acts         map[string]int `json:"acts,omitempty"`
	Team         []int          `json:"team,omitempty"`
	Leader       int            `json:"leader"`
	Rejections   int            `json:"rejections"`
	Successes    int            `json:"successes"`
	Failures     int            `json:"failures"`
	Ties         int            `json:"ties"`
	Pending      []int          `json:"pending,omitempty"`
	PendingPhase string         `json:"pending_phase,omitempty"`
	LastGuard    int            `json:"last_guard"`
	HealUsed     bool           `json:"heal_used"`
	PoisonUsed   bool           `json:"poison_used"`
	Question     int            `json:"question"`
	Answers      map[string]int `json:"answers,omitempty"`
	History      []Line         `json:"history,omitempty"`
	Position     string         `json:"position"`
	Explored     []string       `json:"explored,omitempty"`
	Cleared      []string       `json:"cleared,omitempty"`
	Rested       bool           `json:"rested"`
	Found        []string       `json:"found,omitempty"`
	Revision     int            `json:"revision"`
	Updated      string         `json:"updated"`
	Created      string         `json:"created"`
	Daily        string         `json:"daily,omitempty"`
}
type Input struct {
	Actor, Name, Text string
	Private, Friend   bool
}
type Delivery struct {
	Target  string `json:"target"`
	Text    string `json:"text"`
	Private bool   `json:"private"`
	Sent    bool   `json:"sent"`
}

func Public(text string) Delivery     { return Delivery{Text: text} }
func Secret(id, text string) Delivery { return Delivery{Target: id, Text: text, Private: true} }
func Roll(n int) int {
	if n < 1 {
		panic("invalid die")
	}
	v, e := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if e != nil {
		panic(e)
	}
	return int(v.Int64()) + 1
}
func shuffle[T any](a []T) {
	for i := len(a) - 1; i > 0; i-- {
		j := Roll(i+1) - 1
		a[i], a[j] = a[j], a[i]
	}
}
func (g *Game) Index(id string) int {
	for i, p := range g.Players {
		if p.ID == id {
			return i
		}
	}
	return -1
}
func (g *Game) Roster() string {
	var lines []string
	for i, p := range g.Players {
		state := "未私聊确认"
		if p.Ready {
			state = "已确认"
		}
		if g.Status != "lobby" {
			state = "在场"
			if !p.Alive {
				state = "出局"
			}
		}
		lines = append(lines, fmt.Sprintf("%d. %s（%s）", i+1, p.Name, state))
	}
	return strings.Join(lines, "\n")
}
func (g *Game) Instructions() string {
	d, _ := Find(g.Kind)
	base := "【" + d.Name + " · " + g.Code + "】\n" + g.Roster() + "\n"
	if g.Status == "lobby" {
		if !d.Secret {
			return base + "群内回复「加入」，房主发送「开始游戏」。无需加好友确认。房主可以暂停或结束。"
		}
		return base + "群内回复「加入」，随后私聊本人「确认入局 " + g.Code + "」，再按收到的收件码完成双向确认。全部确认后房主发送「开始游戏」。\n私聊游戏操作统一使用「游戏 " + g.Code + " 操作」。房主可结束游戏。"
	}
	return base + fmt.Sprintf("阶段：%s · 第%d轮\n", PhaseName(g.Phase), g.Round) + g.PhaseHelp()
}
func PhaseName(p string) string {
	m := map[string]string{"speech": "轮流发言", "vote": "投票", "guess": "出局猜词", "wolves": "狼人行动", "night": "夜间技能", "hunter": "猎人开枪", "day": "白天讨论", "team": "队长组队", "approval": "组队审批", "quest": "秘密任务", "assassinate": "刺杀梅林", "investigate": "搜证讨论", "accuse": "指认凶手", "quiz": "答题", "story": "剧情互动", "explore": "探索", "done": "已结束"}
	if v := m[p]; v != "" {
		return v
	}
	return p
}
func (g *Game) PhaseHelp() string {
	switch g.Phase {
	case "speech":
		return "使用「发言 内容」描述，所有在场玩家发言后进入投票。"
	case "vote":
		return "使用「投票 序号」；每人一票，不可投自己。"
	case "guess":
		return "出局的卧底请私聊「游戏 " + g.Code + " 猜词 词语」，或「游戏 " + g.Code + " 跳过」。"
	case "wolves":
		return "狼人私聊使用「刀 序号」或「狼聊 内容」。所有狼人提交后进入其他角色行动。"
	case "night":
		return "等待有技能的玩家私聊完成操作。无行动的玩家无需回复。"
	case "day":
		return "公开讨论使用「发言 内容」，房主发「投票」进入投票阶段。"
	case "hunter":
		return "等待猎人私聊「开枪 序号」或「跳过」。"
	case "team":
		return fmt.Sprintf("队长是%d号，发送「组队 序号,序号…」，本轮需要%d人。", g.Leader+1, g.TeamSize())
	case "approval":
		return "所有玩家私聊「批准」或「反对」，全部提交后公开统计。"
	case "quest":
		return "队员私聊「任务 成功」或「任务 失败」；好人只能提交成功。"
	case "assassinate":
		return "刺客私聊「刺杀 序号」，其他玩家等待结果。"
	case "investigate":
		return "使用「搜证 地点」，秘密线索私聊发放。讨论后房主发送「指认」开始最终投票。"
	case "accuse":
		return "使用「指认 序号」提交答案；全部提交后揭晓。"
	case "quiz":
		return g.QuestionText()
	case "explore":
		return "建角色 名字 力量/敏捷/智慧；地图、角色、背包；探索 地点ID、调查、检定 技能、战斗、使用 物品、休息。"
	case "story":
		return "直接回应剧情，或发送「选择 序号」。发送「结束游戏」退出。"
	}
	return ""
}
func (g *Game) QuestionText() string {
	if g.Question >= len(g.Pack.Questions) {
		return ""
	}
	q := g.Pack.Questions[g.Question]
	text := fmt.Sprintf("第%d/%d题：%s", g.Question+1, len(g.Pack.Questions), q.Text)
	for i, o := range q.Options {
		text += fmt.Sprintf("\n%s. %s", string(rune('A'+i)), o)
	}
	if g.Kind == "chemistry" {
		return text + "\n私聊「游戏 " + g.Code + " 答 A」作答。"
	}
	return text + "\n回复「答 A」。"
}
func Decode(raw string, p *Pack) error {
	d := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(p); err != nil {
		return fmt.Errorf("模型结构无效：%w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("模型返回了多份内容")
	}
	return nil
}
func Validate(kind string, p Pack, seats int) error {
	if len([]rune(p.Title)) > 80 || strings.TrimSpace(p.Title) == "" || len([]rune(p.Text)) > 6000 {
		return fmt.Errorf("标题或正文无效")
	}
	bounded := func(s string, n int) bool { return strings.TrimSpace(s) != "" && len([]rune(s)) <= n }
	switch kind {
	case "undercover":
		if len(p.Words) != 2 || p.Words[0] == p.Words[1] || !bounded(p.Words[0], 30) || !bounded(p.Words[1], 30) {
			return fmt.Errorf("需要两个不同的简短词语")
		}
		if strings.Contains(p.Text, p.Words[0]) || strings.Contains(p.Text, p.Words[1]) {
			return fmt.Errorf("公开开场不能包含秘密词语")
		}
	case "chemistry", "personality":
		if len(p.Questions) < 3 || len(p.Questions) > 10 {
			return fmt.Errorf("需要3—10题")
		}
		for _, q := range p.Questions {
			if !bounded(q.Text, 200) || len(q.Options) != 4 || len(q.Scores) != 4 {
				return fmt.Errorf("每题需要四个选项和四个分值")
			}
			for i, o := range q.Options {
				if !bounded(o, 100) || q.Scores[i] < 0 || q.Scores[i] > 3 {
					return fmt.Errorf("选项或分值无效")
				}
			}
		}
	case "murder":
		if len(p.Characters) != seats || p.Culprit < 1 || p.Culprit > seats || !bounded(p.Solution, 4000) || len(p.Clues) < seats || len(p.Clues) > 30 {
			return fmt.Errorf("角色、凶手、线索或解答不完整")
		}
		ids := map[string]bool{}
		for _, c := range p.Characters {
			if !bounded(c.Name, 40) || !bounded(c.Secret, 2000) || !bounded(c.Goal, 300) {
				return fmt.Errorf("角色资料无效")
			}
		}
		for _, c := range p.Clues {
			if !bounded(c.ID, 40) || ids[c.ID] || !bounded(c.Place, 80) || !bounded(c.Text, 1500) || c.Owner < 0 || c.Owner > seats {
				return fmt.Errorf("线索无效")
			}
			ids[c.ID] = true
		}
	case "dungeon", "trpg":
		if len(p.Rooms) < 3 || len(p.Rooms) > 12 {
			return fmt.Errorf("地图需要3—12个地点")
		}
		ids := map[string]bool{}
		for _, r := range p.Rooms {
			if !bounded(r.ID, 20) || ids[r.ID] || !bounded(r.Name, 60) || !bounded(r.Text, 1000) || !bounded(r.Success, 700) || !bounded(r.Failure, 700) || r.Difficulty < 5 || r.Difficulty > 18 || r.Enemy < 0 || r.Enemy > 12 || len(r.Links) == 0 || len([]rune(r.Item)) > 60 {
				return fmt.Errorf("地点参数无效")
			}
			ids[r.ID] = true
		}
		for _, r := range p.Rooms {
			for _, link := range r.Links {
				if !ids[link] || link == r.ID {
					return fmt.Errorf("地点连接无效")
				}
			}
		}
		seen := map[string]bool{}
		var walk func(string)
		walk = func(id string) {
			if seen[id] {
				return
			}
			seen[id] = true
			for _, r := range p.Rooms {
				if r.ID == id {
					for _, link := range r.Links {
						walk(link)
					}
				}
			}
		}
		walk(p.Rooms[0].ID)
		if len(seen) != len(p.Rooms) {
			return fmt.Errorf("地图存在不可达地点")
		}
	default:
		if !bounded(p.Text, 6000) || len(p.Choices) > 4 {
			return fmt.Errorf("模型没有有效正文")
		}
		for _, c := range p.Choices {
			if !bounded(c, 200) {
				return fmt.Errorf("选项无效")
			}
		}
	}
	return nil
}
func ParseSeat(text string, n int) (int, error) {
	i, e := strconv.Atoi(strings.TrimSpace(text))
	if e != nil || i < 1 || i > n {
		return -1, fmt.Errorf("请输入1—%d的玩家序号", n)
	}
	return i - 1, nil
}

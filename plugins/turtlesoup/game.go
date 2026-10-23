// Package turtlesoup 保存固定题目及主持协议，不依赖微信连接或控制台。
package turtlesoup

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

type Fact struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type Puzzle struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Theme      string   `json:"theme"`
	Difficulty string   `json:"difficulty"`
	Surface    string   `json:"surface"`
	Bottom     string   `json:"bottom"`
	Facts      []Fact   `json:"facts"`
	Required   []string `json:"required"`
	Hints      []string `json:"hints"`
	Enabled    bool     `json:"enabled"`
}
type Verdict struct {
	Intent string   `json:"intent"`
	Answer string   `json:"answer"`
	Facts  []string `json:"matched_fact_ids"`
	Solved bool     `json:"solved"`
}

// Decode 接受裸 JSON 或单个代码围栏，拒绝附加正文和未知字段。
func Decode(text string, out any) error {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json\n") || strings.HasPrefix(text, "```\n") {
		_, text, _ = strings.Cut(text, "\n")
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("模型未返回有效的游戏 JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("游戏 JSON 含多余内容")
	}
	return nil
}

func Validate(p Puzzle) error {
	for _, s := range []string{p.Title, p.Theme, p.Difficulty, p.Surface, p.Bottom} {
		if strings.TrimSpace(s) == "" || len([]rune(s)) > 1500 {
			return fmt.Errorf("题目字段缺失或过长")
		}
	}
	if len([]rune(p.Title)) > 80 || len([]rune(p.Theme)) > 100 || len([]rune(p.Difficulty)) > 20 || len(p.Facts) < 2 || len(p.Facts) > 12 || len(p.Hints) < 1 || len(p.Hints) > 5 || len(p.Required) < 2 {
		return fmt.Errorf("题目需要 2—12 项事实、至少两项通关要点及 1—5 条提示")
	}
	if strings.Contains(p.Surface, p.Bottom) {
		return fmt.Errorf("汤面不能包含完整汤底")
	}
	ids := map[string]bool{}
	for _, f := range p.Facts {
		if f.ID == "" || len(f.ID) > 40 || ids[f.ID] || strings.TrimSpace(f.Text) == "" || len([]rune(f.Text)) > 400 {
			return fmt.Errorf("关键事实无效或重复")
		}
		ids[f.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range p.Required {
		if !ids[id] || seen[id] {
			return fmt.Errorf("通关要点必须引用不同的关键事实")
		}
		seen[id] = true
	}
	for _, h := range p.Hints {
		if strings.TrimSpace(h) == "" || len([]rune(h)) > 300 || strings.Contains(h, p.Bottom) {
			return fmt.Errorf("提示为空、过长或包含完整答案")
		}
	}
	return nil
}

func ValidateVerdict(v Verdict, p Puzzle) error {
	if !slices.Contains([]string{"question", "guess", "unrelated"}, v.Intent) {
		return fmt.Errorf("主持意图无效")
	}
	if !slices.Contains([]string{"yes", "no", "irrelevant", "unknown", "split"}, v.Answer) {
		return fmt.Errorf("主持裁定无效")
	}
	seen := map[string]bool{}
	for _, id := range v.Facts {
		if seen[id] || !slices.ContainsFunc(p.Facts, func(f Fact) bool { return f.ID == id }) {
			return fmt.Errorf("主持引用了不存在或重复的事实")
		}
		seen[id] = true
	}
	if v.Solved && (v.Intent != "guess" || v.Answer != "yes" || !Solved(v, p)) {
		return fmt.Errorf("通关裁定未覆盖所有要点")
	}
	if v.Intent == "question" && len(v.Facts) > 0 {
		return fmt.Errorf("问题裁定不能冒充玩家猜中的事实")
	}
	if v.Intent == "unrelated" && (v.Solved || len(v.Facts) > 0) {
		return fmt.Errorf("无关消息不能改变游戏事实")
	}
	return nil
}
func Solved(v Verdict, p Puzzle) bool {
	return v.Solved && v.Intent == "guess" && len(p.Required) > 0 && !slices.ContainsFunc(p.Required, func(id string) bool { return !slices.Contains(v.Facts, id) })
}
func Answer(v Verdict) string {
	return map[string]string{"yes": "是。", "no": "不是。", "irrelevant": "无关。", "unknown": "信息不足，暂时无法确定。", "split": "请拆成两个问题，一次问一件事。"}[v.Answer]
}

// Defaults 为本项目自编题；不依赖第三方题库或网络。
var Defaults = []Puzzle{
	{ID: "builtin-office", Title: "提前打卡", Theme: "职场", Difficulty: "简单", Surface: "她每天都第一个打卡，却总在所有人下班后才离开。老板发现后，反而让她以后晚一点来。为什么？", Bottom: "她是公司聘请的保洁员。员工上班前和下班后都是她的清洁时段，中间本可离开休息，但她误以为必须整天留在公司。老板发现误会后，将清洁改到员工下班后的一个时段，让她不必清晨赶来。", Facts: []Fact{{"f1", "她是公司聘请的保洁员"}, {"f2", "清洁原分上班前、下班后两个时段，中间可以离开"}, {"f3", "她误以为需要全天留守，老板因此合并清洁时段"}}, Required: []string{"f1", "f3"}, Hints: []string{"她的岗位和大多数员工不同。", "中午没有人要求她留在公司。", "老板调整的是清洁工作的安排。"}, Enabled: true},
	{ID: "builtin-life", Title: "没响的门铃", Theme: "生活", Difficulty: "简单", Surface: "门铃坏了，他却知道每天来送东西的人已经到门口了。他没有看手机，也没看门外。为什么？", Bottom: "他是一位听力正常的钢琴调律师。每天送来的都是邻居的狗粮，送货员一到门外，家里的狗就会跑到门口抓门。他听到狗爪抓门便知道送货员来了，门铃是否能响不影响判断。", Facts: []Fact{{"f1", "家中有一只狗，主人听力正常"}, {"f2", "来人每天送狗粮，狗会跑到门口抓门"}, {"f3", "他凭狗抓门的声音判断来人"}}, Required: []string{"f2", "f3"}, Hints: []string{"他没有失去听力。", "屋里还有一个比他更早发现来人的伙伴。", "那个伙伴每天都很期待送来的东西。"}, Enabled: true},
	{ID: "builtin-mystery", Title: "空白的凭证", Theme: "悬疑", Difficulty: "中等", Surface: "她递上一张空白纸，工作人员立刻把一个袋子交给她。纸上没有隐形文字，也没有暗号；他们互不认识。为什么？", Bottom: "这是一个临时寄存点，打印机坏了，工作人员把空白纸随机撕成两半，一半交给寄存者，一半系在袋子上作为临时凭证。她来取物时，工作人员把两张纸的撕裂边缘拼合，确认吻合后将她寄存的袋子归还。", Facts: []Fact{{"f1", "这是临时寄存点，袋子是她之前寄存的"}, {"f2", "空白纸被随机撕成两半，一半交给她，一半系在袋子上"}, {"f3", "工作人员通过拼合撕裂边缘核对凭证，与文字及熟人关系无关"}}, Required: []string{"f2", "f3"}, Hints: []string{"工作人员需要核对的是物品和领取凭证。", "纸的形状也是信息。", "她手中的纸原来还有另一半。"}, Enabled: true},
}

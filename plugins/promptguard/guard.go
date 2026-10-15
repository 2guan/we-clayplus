// 文件名：guard.go
// 主要功能：以可解释规则检查不可信文本，区分数据资料与直接指令，不授予任何工具权限。
// 作者：2guan
package promptguard

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Rule 的来源只能是管理员保存的配置；匹配结果不包含输入原文。
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Source  string `json:"source"` // all / input / history / document / output / tool
	Level   string `json:"level"`  // high / medium
	Action  string `json:"action"` // block / observe
	Enabled bool   `json:"enabled"`
}
type Hit struct {
	Rule   string `json:"rule"`
	Name   string `json:"name"`
	Level  string `json:"level"`
	Action string `json:"action"`
	Source string `json:"source"`
}
type Config struct {
	Enabled bool
	Mode    string
	Rules   []Rule
}

// 默认规则只拦截明确越权意图，正常摘要、JSON 输出和拒答不会被宽泛关键词误杀。
func Builtins() []Rule {
	return []Rule{
		{"instruction-override", "覆盖既有指令", `(?i)(忽略|无视|丢弃|绕过)\s*(之前|前面|以上|所有|系统)[\s的所有]*(指令|规则|提示词|约束)|(?:ignore|disregard|bypass|override)\s+(?:all\s+)?(?:previous|prior|above|system)\s+(?:instructions|rules|prompts)`, "all", "high", "block", true},
		{"prompt-extraction", "提取隐藏提示词", `(?i)(输出|打印|泄露|复述|显示)\s*(你[的]?\s*)?(完整\s*)?(系统提示词|隐藏指令|初始指令)|(?:reveal|print|output|repeat)\s+(?:your\s+)?(?:system\s+prompt|hidden\s+instructions)`, "all", "high", "block", true},
		{"privilege-escalation", "伪造权限升级", `(?i)(开启|进入)\s*(开发者模式|越狱模式|无审查模式)|(给自己|为自己)\s*(添加|增加|赋予).*?(shell|执行权限|管理员权限)|(?:act\s+as|you\s+are\s+now)\s+(?:an?\s+)?(?:unrestricted|jailbroken|root\s+administrator)`, "all", "high", "block", true},
		{"role-delimiter", "伪造角色分隔符", `(?i)<\|(?:im_start|start_header_id)\|>\s*(system|developer)|\[/?INST\]|<<SYS>>`, "all", "high", "block", true},
		{"credential-extraction", "索取私密凭据", `(?i)(输出|读取|导出|发送|打印).{0,24}(API\s*KEY|私钥|访问令牌|会话密钥)|(?:dump|reveal|export|print)\s+(?:the\s+|your\s+)?(?:api\s*key|private\s*key|access\s*token)`, "all", "high", "block", true},
		{"capability-probing", "探询内部能力", `(?i)(列出|显示|告诉我).{0,16}(内部工具|内部能力|运行环境)|list\s+(?:all\s+)?(?:internal\s+tools|capabilities)`, "all", "medium", "observe", true},
	}
}

func Validate(rules []Rule) error {
	if len(rules) > 64 {
		return fmt.Errorf("自定义防护规则最多 64 条")
	}
	seen := map[string]bool{}
	for _, rule := range Builtins() {
		seen[rule.ID] = true
	}
	validID := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for _, r := range rules {
		if !validID.MatchString(r.ID) || len(r.ID) > 64 || seen[r.ID] || strings.TrimSpace(r.Name) == "" || len(r.Name) > 100 || r.Pattern == "" || len(r.Pattern) > 1000 {
			return fmt.Errorf("规则标识、名称或表达式无效/重复")
		}
		seen[r.ID] = true
		if !contains([]string{"all", "input", "history", "document", "output", "tool"}, r.Source) || !contains([]string{"high", "medium"}, r.Level) || !contains([]string{"block", "observe"}, r.Action) {
			return fmt.Errorf("规则来源、级别或动作无效")
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("规则 %s 的正则表达式无效", r.ID)
		}
	}
	return nil
}
func contains(items []string, value string) bool {
	for _, s := range items {
		if s == value {
			return true
		}
	}
	return false
}

// Normalize 只生成检测副本；统一全角、零宽字符及跨行空白，不改写归档原文。
func Normalize(text string) string {
	text = strings.Map(func(r rune) rune {
		if r >= 0xFF01 && r <= 0xFF5E {
			return r - 0xFEE0
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

// Check 的 document/history 来自资料而非指令；命中后由调用者隔离该条资料。
// 编辑器允许处理攻击示例作为文本，但不会获得工具或发送能力。
func Check(c Config, text, source string, editing bool) ([]Hit, error) {
	hits := []Hit{}
	if !c.Enabled {
		return hits, nil
	}
	if !contains([]string{"observe", "standard", "strict"}, c.Mode) {
		return nil, fmt.Errorf("防护模式无效")
	}
	if err := Validate(c.Rules); err != nil {
		return nil, err
	}
	text = Normalize(text)
	builtins := Builtins()
	for index, rule := range append(builtins, c.Rules...) {
		if !rule.Enabled || rule.Source != "all" && rule.Source != source {
			continue
		}
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("防护规则无法加载")
		}
		if !re.MatchString(text) {
			continue
		}
		action := rule.Action
		if c.Mode == "strict" {
			action = "block"
		}
		if c.Mode == "observe" || editing {
			action = "observe"
		}
		// 输出中的解释、引用或拒绝语不是新的输入指令。内置输入意图规则只观察；
		// 实际凭据/人设回显由执行层检测，管理员自定义输出规则仍严格生效。
		if source == "output" && index < len(builtins) && rule.ID != "role-delimiter" {
			action = "observe"
		}
		hits = append(hits, Hit{rule.ID, rule.Name, rule.Level, action, source})
	}
	return hits, nil
}
func Blocked(hits []Hit) bool {
	for _, hit := range hits {
		if hit.Action == "block" {
			return true
		}
	}
	return false
}

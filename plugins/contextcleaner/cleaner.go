// 文件名：cleaner.go
// 主要功能：整理模型上下文副本、隔离会话、控制容量并清理明确的推理输出，不修改原始消息。
// 作者：2guan
package contextcleaner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Message 保留真实身份和来源；它是资料，不会被转换成 system 指令。
type Message struct {
	ID      string `json:"id,omitempty"`
	Session string `json:"session,omitempty"`
	Role    string `json:"role"`
	Sender  string `json:"sender,omitempty"`
	Source  string `json:"source"`
	Kind    string `json:"kind,omitempty"`
	Text    string `json:"text"`
	Time    string `json:"time,omitempty"`
}
type Change struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type Config struct {
	Enabled, Controls, Dedupe, Media, Thinking, Silence, Stage bool
	PreserveAll                                                bool // 分析资料不采用历史滚动裁剪，超限必须由调用方报告。
	MaxMessages, Budget                                        int
}
type Result struct {
	Messages []Message `json:"messages"`
	Changes  []Change  `json:"changes"`
	Tokens   int       `json:"estimated_tokens"`
}

// Estimate 为保守上界而非厂商 tokenizer；把中文、标点和 JSON 包装都计入预算。
func Estimate(text string) int {
	n := 0
	for _, r := range text {
		if r <= 127 {
			n++
		} else {
			n += 4
		}
	}
	return n
}
func cleanControls(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

// Input 仅移除异常控制字符；当前请求中的代码、标签和措辞保持原样。
func Input(c Config, text string) string {
	if c.Enabled && c.Controls {
		return cleanControls(text)
	}
	return text
}
func Encode(messages []Message) string { b, _ := json.Marshal(messages); return string(b) }

func Prepare(c Config, messages []Message, session string) (Result, error) {
	result := Result{Messages: []Message{}, Changes: []Change{}}
	seen := map[string]bool{}
	for _, original := range messages {
		m := original
		// 身份边界无论插件开关都执行，不允许会话内容伪造高优先级角色。
		if session != "" && m.Session != "" && m.Session != session {
			result.Changes = append(result.Changes, Change{m.ID, "其他会话"})
			continue
		}
		if m.Role != "assistant" {
			m.Role = "user"
		}
		if c.Enabled {
			if c.Controls {
				m.Text = cleanControls(m.Text)
			}
			if c.Media && slices.Contains([]string{"image", "audio", "video", "file", "emoji"}, m.Kind) {
				m.Text = "[" + m.Kind + "媒体；原始内容保留在消息归档中]"
			}
			if strings.TrimSpace(m.Text) == "" {
				result.Changes = append(result.Changes, Change{m.ID, "空消息"})
				continue
			}
			if c.Dedupe && m.ID != "" && seen[m.ID] {
				result.Changes = append(result.Changes, Change{m.ID, "重复事件 ID"})
				continue
			}
		}
		seen[m.ID] = true
		if m.Text != original.Text {
			result.Changes = append(result.Changes, Change{m.ID, "控制字符或历史媒体整理"})
		}
		result.Messages = append(result.Messages, m)
	}
	if c.MaxMessages > 0 && len(result.Messages) > c.MaxMessages {
		for _, m := range result.Messages[:len(result.Messages)-c.MaxMessages] {
			result.Changes = append(result.Changes, Change{m.ID, "超出消息条数"})
		}
		result.Messages = result.Messages[len(result.Messages)-c.MaxMessages:]
	}
	// 必须保留最新消息；单条本身超预算时明确失败，不静默丢掉用户本轮请求。
	for !c.PreserveAll && c.Budget > 0 && len(result.Messages) > 1 && Estimate(Encode(result.Messages)) > c.Budget {
		result.Changes = append(result.Changes, Change{result.Messages[0].ID, "超出上下文预算"})
		result.Messages = result.Messages[1:]
	}
	result.Tokens = Estimate(Encode(result.Messages))
	if c.Budget <= 0 || len(result.Messages) > 0 && result.Tokens > c.Budget {
		if c.PreserveAll {
			return result, fmt.Errorf("分析资料超过可用容量，请减少范围或提高请求容量上限；未静默删除资料")
		}
		return result, fmt.Errorf("最新消息超过可用上下文预算，请缩短内容或提高预算")
	}
	return result, nil
}

var thinking = regexp.MustCompile(`(?is)<(?:think|thought|reasoning|reflection)\b[^>]*>.*?</(?:think|thought|reasoning|reflection)>`)
var unfinished = regexp.MustCompile(`(?is)<(?:think|thought|reasoning|reflection)\b[^>]*>.*$`)
var closing = regexp.MustCompile(`(?is)^.*?</(?:think|thought|reasoning|reflection)>`)
var stage = regexp.MustCompile(`[（(](笑|轻笑|叹气|停顿|思考|低声)[）)]`)

// Output 不根据“系统提示词”“抱歉”等普通词猜测泄露，也不会伪造一条替代回复。
func Output(c Config, text string, chat bool) (string, bool) {
	if !c.Enabled {
		return text, false
	}
	if c.Controls {
		text = cleanControls(text)
	}
	// 合法 JSON 和 Markdown 代码围栏中的标签是正文示例，不能作为推理块删除。
	if !json.Valid([]byte(strings.TrimSpace(text))) {
		parts := strings.Split(text, "```")
		for i := range parts {
			if i%2 != 0 {
				continue
			}
			if c.Thinking {
				parts[i] = thinking.ReplaceAllString(parts[i], "")
				parts[i] = closing.ReplaceAllString(parts[i], "")
				parts[i] = unfinished.ReplaceAllString(parts[i], "")
			}
			if chat && c.Stage {
				parts[i] = stage.ReplaceAllString(parts[i], "")
			}
		}
		text = strings.Join(parts, "```")
	}
	text = strings.TrimSpace(text)
	if chat && c.Silence && (text == "NO_REPLY" || text == "[SILENT]") {
		return "", true
	}
	return text, false
}

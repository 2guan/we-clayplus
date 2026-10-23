// 主要功能：在微信发送前识别明显失控的碎片短行和重复文本，作为模型失败重试。
package dashboard

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

var replyRecordLine = regexp.MustCompile(`^(?:〔m[0-9a-f]{8}〕)?(?:\d{4}[-/]\d{1,2}[-/]\d{1,2}[ T])?\[?(?:[01]?\d|2[0-3]):[0-5]\d(?::[0-5]\d)?\]?\s+(.{1,80}?)[：:]`)
var replyRoleLine = regexp.MustCompile(`(?i)^\[(?:assistant|user|system|developer)\]`)

func replyQualityError(text string, conversation *ai.Conversation) error {
	if strings.TrimSpace(text) == (writeIntentMissing{}).Error() {
		return fmt.Errorf("模型复读内部操作拒绝提示，已停止发送")
	}
	names := map[string]bool{"本人": true, "assistant": true}
	var selfAddress *regexp.Regexp
	if conversation != nil {
		c := conversation
		if self := strings.TrimSpace(c.SelfName); self != "" {
			selfAddress = regexp.MustCompile(`(?:^|[。！？!?；;\n])\s*@?` + regexp.QuoteMeta(self) + `[，,、\s]*(?:你|您)`)
		}
		names[strings.TrimSpace(c.SelfName)] = true
		for _, name := range c.Names {
			names[strings.TrimSpace(name)] = true
		}
	}
	if contextcleaner.ReplyDecision(text) {
		return fmt.Errorf("模型把回复判断或规划写入正文，已停止发送")
	}
	var lines []string
	short, most := 0, 0
	counts := map[string]int{}
	fence := ""
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		// 代码本身可能有大量短行、缩进或重复；不据此判断自然语言生成失控。
		if fence != "" {
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fence = line[:3]
			continue
		}
		if line == "" {
			continue
		}
		if replyRoleLine.MatchString(line) {
			return fmt.Errorf("模型把角色记录标签写入正文，已停止发送")
		}
		if selfAddress != nil && !strings.HasPrefix(line, ">") && selfAddress.MatchString(line) {
			return fmt.Errorf("模型把本人当成其他成员称呼，已停止发送；请只用本人第一人称回复")
		}
		// 只检查已知发言人的记录前缀，不拦普通时间表达、引用和代码中的日志。
		if match := replyRecordLine.FindStringSubmatch(line); len(match) == 2 {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(match[1]), "〔本人〕"))
			if names[name] {
				return fmt.Errorf("模型把时间和发言人记录标签写入正文，已停止发送")
			}
		}
		lines = append(lines, line)
		if utf8.RuneCountInString(line) <= 4 {
			short++
		}
		counts[line]++
		most = max(most, counts[line])
	}
	if len(lines) >= 32 && short*100 >= len(lines)*80 {
		return fmt.Errorf("模型回复包含大量碎片短行（%d/%d），已停止发送", short, len(lines))
	}
	if most >= 12 && most*100 >= len(lines)*70 {
		return fmt.Errorf("模型回复同一行异常重复（%d/%d），已停止发送", most, len(lines))
	}
	// 仅识别至少 96 字符、连续至少 12 次的短周期重复，不限制正常长回复。
	runes := []rune(strings.Join(lines, "\n"))
	for width := 1; width <= 64 && width*12 <= len(runes); width++ {
		run := width
		for i := width; i < len(runes); i++ {
			if runes[i] == runes[i-width] {
				run++
			} else {
				run = width
			}
			if run < max(96, width*12) {
				continue
			}
			for _, r := range runes[i-width+1 : i+1] {
				if unicode.IsLetter(r) || unicode.IsNumber(r) {
					return fmt.Errorf("模型回复出现连续重复文本，已停止发送")
				}
			}
			run = width // 跳过纯标点分隔线，继续检查其后的正文。
		}
	}
	return nil
}

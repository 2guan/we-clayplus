// 主要功能：在微信发送前识别明显失控的碎片短行和重复文本，作为模型失败重试。
package dashboard

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func replyQualityError(text string) error {
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

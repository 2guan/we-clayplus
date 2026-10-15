// 文件名：room_name.go
// 主要功能：为未命名群按成员顺序生成最多十五个字的展示名称。
// 作者：2guan
package contactability

import "strings"

// 只生成展示文字，不修改微信群名；按 Unicode 字符截取，避免截断中文。
func memberRoomName(names []string) string {
	clean := make([]string, 0, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			clean = append(clean, name)
		}
	}
	text := []rune(strings.Join(clean, "、"))
	if len(text) > 15 {
		return string(text[:15]) + "..."
	}
	return string(text)
}

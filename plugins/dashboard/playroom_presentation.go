package dashboard

import (
	"regexp"
	"strings"
)

var playPluginLabel = regexp.MustCompile(`^【互动插件\s*·\s*[a-z_]+】$`)

// 只去掉独立的系统标签和本人署名，保留正文中的称呼、角色台词与来源引用。
func compactPlayReply(text, nickname string) string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		label := strings.Trim(line, "#*>▍|｜ ")
		signature := false
		if nickname != "" {
			for _, prefix := range []string{"主持：", "主持:", "主持人：", "主持人:", "主播：", "主播:"} {
				if strings.HasPrefix(label, prefix) && strings.TrimSpace(strings.TrimPrefix(label, prefix)) == nickname {
					signature = true
					break
				}
			}
		}
		if signature || playPluginLabel.MatchString(label) {
			continue
		}
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, raw)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

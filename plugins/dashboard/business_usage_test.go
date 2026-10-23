package dashboard

import (
	"github.com/we-clay/we-clay/plugins/playroom"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"strings"
	"testing"
)

func TestBusinessUsageExamplesMatchActualEntryPoints(t *testing.T) {
	for _, d := range toolbox.Catalog {
		u := businessUsage(d.ID)
		if u == nil || len(u.Steps) < 4 || d.Trigger != "" && len(u.Commands) == 0 {
			t.Fatal("使用说明或示例不完整", d.ID)
		}
		for _, c := range u.Commands {
			for _, example := range c.Examples {
				if strings.TrimSpace(example) != example || strings.Contains(example, "[主题]") || strings.Contains(example, "<") || strings.Contains(example, "@本人") {
					t.Fatal("示例含不可直接使用的占位语法或伪造 @", d.ID, example)
				}
				if strings.HasPrefix(c.Name, "开局") || c.Name == "私聊开局" || c.Name == "开始测试" || c.Name == "每日抽签与主题" || c.Name == "本群日报" || c.Name == "本群周报" || c.Name == "引用收藏" || c.Name == "查询与回顾" {
					if d.ID == "turtlesoup" {
						if cmd, _ := soupCommand(example); cmd != "start" {
							t.Fatal("海龟汤示例无法开局", example)
						}
					} else if kind, _ := playroom.Match(example); kind != d.ID {
						t.Fatal("互动触发词与真实路由不符", d.ID, example)
					}
				}
				if _, interactive := playroom.Find(d.ID); !interactive && d.ID != "turtlesoup" {
					if kind, _ := toolbox.Match(example); kind != d.ID {
						t.Fatal("业务触发词与真实路由不符", d.ID, example)
					}
				}
			}
		}
	}
	for _, id := range []string{"cron", "welcome", "auto_accept", "dashboard", "prompt_guard", "context_cleaner"} {
		if u := builtinPluginUsage(id, false); u == nil || len(u.Commands) != 0 {
			t.Fatal("事件或页面插件不应编造聊天口令", id)
		}
	}
}

// 文件名：console_config_test.go
// 主要功能：在临时配置文件和插件替身上验证控制台保存及现有重载流程，不连接微信。
// 作者：2guan
package plugin

import (
	sdk "github.com/we-clay/we-clay/sdk/plugin"
	"os"
	"strings"
	"testing"
)

// 目录必须保留插件声明的示例原文，旧式插件没有 schema 时不伪造触发词。
func TestConsolePluginsUsage(t *testing.T) {
	withConfigWatchState(t)
	example := "/example say \"你好，世界\""
	plugins = []*wrapper{{Metadata: &sdk.Metadata{Name: "example", Description: "示例插件"}, Config: &Config{Enable: true}, commandSchemas: []*sdk.CommandSchema{nil, {Main: "example", Sub: "say", Description: "回显文字", Usage: "/example say <text>", Examples: []string{"", example}}}}, {Metadata: &sdk.Metadata{Name: "legacy"}, Config: &Config{}, commands: []string{"legacy"}}}
	items := ConsolePlugins()
	if len(items) != 2 || items[0].Usage == nil || len(items[0].Usage.Commands) != 1 {
		t.Fatalf("未返回命令说明：%+v", items)
	}
	command := items[0].Usage.Commands[0]
	if command.Name != "/example say" || command.Syntax != "/example say <text>" || len(command.Examples) != 1 || command.Examples[0] != example {
		t.Fatalf("命令内容丢失：%+v", command)
	}
	if len(items[1].Usage.Commands) != 0 || !strings.Contains(strings.Join(items[1].Usage.Steps, ""), "未提供详细命令说明") {
		t.Fatal("旧插件说明不准确")
	}
}

func TestConsoleConfigurationSaveAndReload(t *testing.T) {
	withConfigWatchState(t)
	cfg := &Config{Enable: true, Mode: "blacklist", Config: map[string]any{"message": "before"}}
	configs = map[string]*Config{"example": cfg, "untouched": {Enable: false, Mode: "whitelist"}}
	impl := &configWatchTestPlugin{metadata: &sdk.Metadata{Name: "example"}}
	var p sdk.Plugin = impl
	plugins = []*wrapper{{Metadata: impl.metadata, Config: cfg, plugin: &p, abilities: []string{"config"}}}
	if err := saveConfig(); err != nil {
		t.Fatal(err)
	}
	original, err := ConsoleConfiguration("example", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	edited := "enable = false\npriority = 7\nmode = 'whitelist'\nlimits = []\n[config]\nmessage = 'after'\n"
	doc, err := ConsoleConfiguration("example", &edited, original.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Text, "priority = 7") {
		t.Fatal("执行优先级丢失", doc.Text)
	}
	if err = reloadPluginConfigFromFile(); err != nil {
		t.Fatal(err)
	}
	if impl.calls != 1 || !strings.Contains(string(impl.data), "after") || plugins[0].Enable || plugins[0].Metadata.Priority != 7 {
		t.Fatal("配置未完整传入运行插件")
	}
	if configs["untouched"] == nil || configs["untouched"].Mode != "whitelist" {
		t.Fatal("覆盖了其他插件配置")
	}
	saved, _ := os.ReadFile(configPath)
	if _, err = ConsoleConfiguration("example", &edited, original.Revision); err == nil {
		t.Fatal("冲突未阻止")
	}
	invalid := "enable = true\nmode = 'wrong'"
	if _, err = ConsoleConfiguration("example", &invalid, doc.Revision); err == nil {
		t.Fatal("非法名单模式未阻止")
	}
	after, _ := os.ReadFile(configPath)
	if string(after) != string(saved) {
		t.Fatal("拒绝请求修改了文件")
	}
	if _, err = ConsoleConfiguration("../../escape", nil, ""); err == nil {
		t.Fatal("允许任意路径")
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("配置文件权限错误")
	}
}

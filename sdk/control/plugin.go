// 文件名：plugin.go
// 主要功能：定义控制台插件目录和 TOML 配置文档的宿主契约，不暴露任意文件路径。
// 作者：2guan
package control

// PluginInfo 只返回展示元数据，插件凭据不进入公共状态接口。
type PluginInfo struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Enabled     bool         `json:"enabled"`
	Builtin     bool         `json:"builtin"`
	Status      string       `json:"status"`
	Abilities   []string     `json:"abilities"`
	Usage       *PluginUsage `json:"usage,omitempty"`
}

// PluginUsage 仅承载公开说明；示例必须来自插件声明，不推测可执行触发词。
type PluginUsage struct {
	Steps    []string            `json:"steps"`
	Commands []PluginCommandHelp `json:"commands,omitempty"`
}

// PluginCommandHelp 将命令语法与可直接复制的示例分开，避免复制占位参数。
type PluginCommandHelp struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Syntax      string   `json:"syntax"`
	Examples    []string `json:"examples,omitempty"`
}

// PluginDocument 使用内容摘要检查并发编辑；Source 仅为展示用的配置来源。
type PluginDocument struct {
	ID       string         `json:"id"`
	Text     string         `json:"text"`
	Values   map[string]any `json:"values"`
	Revision string         `json:"revision"`
	Source   string         `json:"source"`
	Effect   string         `json:"effect"`
}

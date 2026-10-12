package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/plugin"
)

// ==================== 命令结构体定义 ====================

// EchoCommand 回显命令
type EchoCommand struct {
	_      struct{} `cmd:"example echo" help:"回显文本" usage:"/example echo <text> [--upper] [--prefix 前缀]" example:"/example echo hello --upper --prefix 测试"`
	Text   string   `arg:"text" help:"回显内容" required:"true" variadic:"true"`
	Upper  bool     `flag:"upper" help:"转换为大写"`
	Prefix string   `flag:"prefix" help:"自定义前缀"`
}

// StatusCommand 状态查询命令
type StatusCommand struct {
	_ struct{} `cmd:"example status" help:"查看插件状态" usage:"/example status" example:"/example status"`
}

// ConfigCommand 配置命令
type ConfigCommand struct {
	_     struct{} `cmd:"example config" help:"修改配置" usage:"/example config <key> <value>" example:"/example config prefix [新前缀]"`
	Key   string   `arg:"key" help:"配置项" required:"true"`
	Value string   `arg:"value" help:"配置值" required:"true" variadic:"true"`
}

// SessionCommand 会话查询命令
type SessionCommand struct {
	_ struct{} `cmd:"example session" help:"查看会话信息" usage:"/example session" example:"/example session"`
}

// ==================== 命令接口实现 ====================

// GetCommands 返回命令列表
func (p *ExamplePlugin) GetCommands() []string {
	return plugin.CommandCommands()
}

// OnCommand 命令分发
func (p *ExamplePlugin) OnCommand(cmd *plugin.Command) (string, error) {
	p.stats.TotalCommands++
	return plugin.DispatchCommand(cmd)
}

// ==================== 命令处理函数 ====================

// handleEcho 处理回显命令
func (p *ExamplePlugin) handleEcho(cmd EchoCommand) (string, error) {
	text := cmd.Text

	// 转换为大写
	if cmd.Upper {
		text = strings.ToUpper(text)
	}

	// 添加前缀
	prefix := cmd.Prefix
	if prefix == "" {
		prefix = p.Config.EchoPrefix
	}

	return prefix + text, nil
}

// handleStatus 处理状态查询命令
func (p *ExamplePlugin) handleStatus(cmd StatusCommand) (string, error) {
	p.mu.RLock()
	sessionCount := len(p.sessions)
	p.mu.RUnlock()

	status := fmt.Sprintf(`📊 Example 插件状态

版本: %s
作者: %s

📈 统计信息:
- 处理消息数: %d
- 处理命令数: %d
- 被调用次数: %d
- 活跃会话数: %d

⚙️ 配置:
- 回显前缀: %s
- 自动回复: %v
- 最大长度: %d
`,
		p.GetMetadata().Version,
		p.GetMetadata().Author,
		p.stats.TotalMessages,
		p.stats.TotalCommands,
		p.stats.TotalCalls,
		sessionCount,
		p.Config.EchoPrefix,
		p.Config.ReplyEnabled,
		p.Config.MaxLength,
	)

	return status, nil
}

// handleConfig 处理配置命令
func (p *ExamplePlugin) handleConfig(cmd ConfigCommand) (string, error) {
	switch cmd.Key {
	case "prefix":
		p.Config.EchoPrefix = cmd.Value
	case "reply":
		p.Config.ReplyEnabled = cmd.Value == "true" || cmd.Value == "1"
	case "maxlen":
		var maxLen int
		fmt.Sscanf(cmd.Value, "%d", &maxLen)
		if maxLen > 0 {
			p.Config.MaxLength = maxLen
		}
	default:
		return fmt.Sprintf("未知配置项: %s", cmd.Key), nil
	}

	// 保存配置
	if err := p.SaveConfig(p); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ 配置已更新: %s = %s", cmd.Key, cmd.Value), nil
}

// handleSession 处理会话查询命令
func (p *ExamplePlugin) handleSession(cmd SessionCommand) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.sessions) == 0 {
		return "当前没有活跃会话", nil
	}

	var result strings.Builder
	result.WriteString("🔄 活跃会话列表:\n\n")

	for sender, session := range p.sessions {
		duration := time.Since(session.StartTime)
		result.WriteString(fmt.Sprintf("👤 %s\n", sender))
		result.WriteString(fmt.Sprintf("  - 消息数: %d\n", session.Count))
		result.WriteString(fmt.Sprintf("  - 最后消息: %s\n", session.LastMessage))
		result.WriteString(fmt.Sprintf("  - 持续时间: %v\n\n", duration.Round(time.Second)))
	}

	return result.String(), nil
}

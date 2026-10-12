package main

import (
	"log/slog"
	"sync"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// Config 插件配置结构
// 演示如何定义插件配置，支持通过 TOML 文件配置
type Config struct {
	EchoPrefix   string `toml:"echo_prefix" comment:"回显消息的前缀"`
	ReplyEnabled bool   `toml:"reply_enabled" comment:"是否自动回复"`
	MaxLength    int    `toml:"max_length" comment:"消息最大长度"`
}

// ExamplePlugin 示例插件
// 这个插件演示了 we-clay 插件系统的所有核心功能：
// 1. 基础插件接口（必须）
// 2. 生命周期管理
// 3. 事件订阅和处理
// 4. 命令处理
// 5. 被调用能力（供其他插件调用）
// 6. 配置管理
// 7. 会话劫持
type ExamplePlugin struct {
	// 配置能力（提供配置读写功能）
	plugin.ConfigAbility[Config]

	// 需要的能力（Host 会自动注入）
	message message.Ability       // 消息发送/接收能力
	contact contact.Ability       // 联系人管理能力
	session plugin.SessionAbility // 会话劫持能力
	caller  plugin.CallerAbility  // 调用其他插件能力

	// 插件内部状态
	mu       sync.RWMutex
	sessions map[string]*SessionData // 会话数据
	stats    Stats                   // 统计数据
}

// Stats 统计数据
type Stats struct {
	TotalMessages int
	TotalCommands int
	TotalCalls    int
}

// GetMetadata 返回插件元数据
// 这是所有插件必须实现的接口
func (p *ExamplePlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "example",         // 插件唯一标识
		Author:      "we-clay Team",      // 作者
		Version:     "2.0.0",           // 版本号
		Description: "完整示例插件，展示所有插件功能", // 描述
		Priority:    0,                 // 优先级（越小越先执行）
		Next:        false,             // 是否传递给下一个插件
		AlwaysRun:   false,             // 是否总是运行（忽略黑白名单）
	}
}

func main() {
	// 创建插件实例
	p := &ExamplePlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: Config{
				EchoPrefix:   "[Echo] ",
				ReplyEnabled: true,
				MaxLength:    500,
			},
		},
	}

	// 注册所有命令处理函数
	if err := plugin.RegisterCommand(p.handleEcho); err != nil {
		slog.Error("[example] 注册 echo 命令失败", "err", err)
		return
	}
	if err := plugin.RegisterCommand(p.handleStatus); err != nil {
		slog.Error("[example] 注册 status 命令失败", "err", err)
		return
	}
	if err := plugin.RegisterCommand(p.handleConfig); err != nil {
		slog.Error("[example] 注册 config 命令失败", "err", err)
		return
	}
	if err := plugin.RegisterCommand(p.handleSession); err != nil {
		slog.Error("[example] 注册 session 命令失败", "err", err)
		return
	}

	slog.Info("[example] 示例插件启动中...")

	// 启动插件
	plugin.Start(p)
}

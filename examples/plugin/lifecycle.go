package main

import "log/slog"

// OnLoad 插件加载时调用
// 用于初始化资源、加载配置等
func (p *ExamplePlugin) OnLoad() error {
	slog.Info("[example] 插件加载中...")

	// 初始化内部状态
	p.sessions = make(map[string]*SessionData)
	p.stats = Stats{}

	// 初始化默认配置
	if p.Config.EchoPrefix == "" {
		p.Config.EchoPrefix = "[Echo] "
	}
	if p.Config.MaxLength == 0 {
		p.Config.MaxLength = 500
	}

	slog.Info("[example] 插件加载完成",
		"prefix", p.Config.EchoPrefix,
		"reply_enabled", p.Config.ReplyEnabled,
		"max_length", p.Config.MaxLength)

	return nil
}

// OnUnload 插件卸载时调用
// 用于清理资源、保存状态等
func (p *ExamplePlugin) OnUnload() error {
	slog.Info("[example] 插件卸载中...")

	// 输出统计信息
	slog.Info("[example] 统计信息",
		"total_messages", p.stats.TotalMessages,
		"total_commands", p.stats.TotalCommands,
		"total_calls", p.stats.TotalCalls)

	// 清理资源
	p.mu.Lock()
	p.sessions = nil
	p.mu.Unlock()

	slog.Info("[example] 插件卸载完成")
	return nil
}

// OnEnable 插件启用时调用
func (p *ExamplePlugin) OnEnable() error {
	slog.Info("[example] 插件已启用")
	return nil
}

// OnDisable 插件禁用时调用
func (p *ExamplePlugin) OnDisable() error {
	slog.Info("[example] 插件已禁用")
	return nil
}

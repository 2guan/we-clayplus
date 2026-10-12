package main

import "fmt"

// GetCapabilities 返回插件提供的能力列表
func (p *ExamplePlugin) GetCapabilities() []string {
	return []string{
		"format",   // 格式化文本
		"validate", // 验证文本长度
		"stats",    // 获取统计信息
	}
}

// OnCall 处理能力调用
func (p *ExamplePlugin) OnCall(capability string, args map[string]string) (string, []byte, error) {
	p.stats.TotalCalls++

	switch capability {
	case "format":
		// 格式化文本
		text := args["text"]
		result := p.Config.EchoPrefix + text
		return "text", []byte(result), nil

	case "validate":
		// 验证文本长度
		text := args["text"]
		valid := len(text) <= p.Config.MaxLength
		return "bool", []byte(fmt.Sprint(valid)), nil

	case "stats":
		// 返回统计信息
		stats := fmt.Sprintf(`{"messages":%d,"commands":%d,"calls":%d}`,
			p.stats.TotalMessages,
			p.stats.TotalCommands,
			p.stats.TotalCalls,
		)
		return "json", []byte(stats), nil

	default:
		return "", nil, fmt.Errorf("不支持的能力: %s", capability)
	}
}

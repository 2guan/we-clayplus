// 文件名：console_capabilities.go
// 主要功能：从仓库唯一的能力清单提供控制台行为目录，避免维护第二份 core 支持列表。
// 作者：2guan
package gateway

import (
	_ "embed"
	"encoding/json"
	"github.com/we-clay/we-clay/sdk/control"
)

//go:embed capabilities.json
var consoleCapabilities []byte

func dashboardCapabilities() map[string]control.Capability {
	var result map[string]control.Capability
	if err := json.Unmarshal(consoleCapabilities, &result); err != nil {
		panic("内嵌能力清单格式无效")
	}
	return result
}

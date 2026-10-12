package plugin

import (
	"slices"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

// isAllowed 检查发送者是否被允许调用插件
func isAllowed(sender string, w *wrapper) bool {
	if w.Config == nil {
		return true
	}

	if config.Get().Owner == sender {
		return true
	}

	inLimits := slices.Contains(w.Config.Limits, sender)

	switch w.Config.Mode {
	case "blacklist":
		// 黑名单模式：在 limits 中的不可调用
		return !inLimits
	case "whitelist":
		// 白名单模式：在 limits 中的可调用
		return inLimits
	default:
		return true
	}
}

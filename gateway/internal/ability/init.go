package ability

import (
	_ "github.com/we-clay/we-clay/gateway/internal/ability/cdn"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/favor"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/label"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/login"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/message"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/miniapp"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/moments"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/official"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/payment"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/report"
	_ "github.com/we-clay/we-clay/gateway/internal/ability/user"
)

import contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
import chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"

// Inject 注入能力层
func Inject() {
}

func Destroy() {
	contactability.Destroy()
	chatroomability.Destroy()
}

// 文件名：conversation.go
// 主要功能：解析联系人置顶和群免打扰状态，区分未知字段与明确关闭，不猜测群折叠。
// 作者：2guan
package contactability

import (
	sdk "github.com/we-clay/we-clay/sdk/contact"
	"google.golang.org/protobuf/proto"
	"strings"
)

// CONTACTFLAG_TOPCONTACT=0x800。只合并掩码覆盖的位，避免备注等增量更新清除置顶。
// chatroom_notify：0 不通知、1 通知；私聊上的默认 0 不代表免打扰。
func mergeConversation(c *sdk.Contact, mask, value, notify *uint32) {
	const top uint32 = 0x800
	if value != nil && (mask == nil || *mask&top != 0) {
		c.Pinned = proto.Bool(*value&top != 0)
	}
	if strings.HasSuffix(c.Username, "@chatroom") && notify != nil && *notify <= 1 {
		c.Muted = proto.Bool(*notify == 0)
	}
}

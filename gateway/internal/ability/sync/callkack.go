package sync

import (
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"log/slog"

	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
)

func CallBack() {
	if !messageability.OutboundReady() {
		return
	}
	result, err := messageapi.Get().Sync(262151)
	if err != nil {
		slog.Warn("消息同步失败", "err", err)
		return
	}
	slog.Debug("同步数据成功")
	handle(result)
}

func handle(result *messageapi.SyncResult) {
	if result.ModifyUserInfo != nil || result.UserInfoExtend != nil {
		handleUserInfo(result.ModifyUserInfo, result.UserInfoExtend)
	}
	if result.NewMessage != nil {
		handleMessage(result.NewMessage)
	}
	if result.ModifyContact != nil {
		handleContact(result.ModifyContact)
	}
	if result.ModifyChatroomMember != nil {
		handleChatroomMember(result.ModifyChatroomMember)
	}
	if result.ModifyChatroomMemberDisplayName != nil {
		handleChatroomMemberDisplayName(result.ModifyChatroomMemberDisplayName)
	}
}

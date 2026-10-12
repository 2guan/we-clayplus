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
	if err := (&processor{}).handle(result); err != nil {
		slog.Warn("处理同步事件失败", "err", err)
	}
}

func (p *processor) handle(result *messageapi.SyncResult) error {
	if result.ModifyUserInfo != nil || result.UserInfoExtend != nil {
		if err := p.handleUserInfo(result.ModifyUserInfo, result.UserInfoExtend); err != nil {
			return err
		}
	}
	if result.NewMessage != nil {
		if err := p.handleMessage(result.NewMessage); err != nil {
			return err
		}
	}
	if result.ModifyContact != nil {
		if err := p.handleContact(result.ModifyContact); err != nil {
			return err
		}
	}
	if result.ModifyChatroomMember != nil {
		if err := p.handleChatroomMember(result.ModifyChatroomMember); err != nil {
			return err
		}
	}
	if result.ModifyChatroomMemberDisplayName != nil {
		if err := p.handleChatroomMemberDisplayName(result.ModifyChatroomMemberDisplayName); err != nil {
			return err
		}
	}
	return nil
}

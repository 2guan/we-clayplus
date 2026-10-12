package sync

import (
	"log/slog"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
)

func (p *processor) handleUserInfo(infos []*messageapi.ModifyUserInfo, extends []*messageapi.UserInfoExtend) error {
	maxLen := len(infos)
	if len(extends) > maxLen {
		maxLen = len(extends)
	}
	for index := 0; index < maxLen; index++ {
		var info *messageapi.ModifyUserInfo
		var ext *messageapi.UserInfoExtend
		if index < len(infos) {
			info = infos[index]
		}
		if index < len(extends) {
			ext = extends[index]
		}
		event, err := contactability.ApplySelfInfo(info, ext)
		if err != nil {
			slog.Warn("处理用户信息变更失败", "err", err)
			continue
		}
		if event != nil {
			if err := p.publishChangeEvents([]*pluginsdk.ChangeEvent{event}); err != nil {
				return err
			}
		}
	}
	return nil
}

// 文件名：room.go
// 主要功能：从 core 读取群成员和公告，仅返回展示所需字段，不泄露协议凭据。
// 作者：2guan
package console

import (
	"fmt"
	chatroomapi "github.com/we-clay/we-clay/gateway/internal/api/chatroom"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
)

func (b *Bridge) roomInfo(id string) (dashboard.RoomInfo, error) {
	out := dashboard.RoomInfo{Members: []dashboard.RoomMember{}}
	// 成员与公告互不依赖，并行读取，避免打开资料时累加两次网络等待。
	type infoResult struct {
		info *chatroom.GetInfo_Response
		err  error
	}
	infoDone := make(chan infoResult, 1)
	go func() { info, err := chatroom.Instance.GetInfo(id); infoDone <- infoResult{info, err} }()

	// 普通联系人详情可能只有成员名字；专用成员接口才包含头像和群内昵称。
	data, err := chatroomapi.Get().ListMembers(id)
	if err != nil {
		return out, err
	}
	if data == nil || data.GetBaseResponse() == nil || data.GetBaseResponse().GetCode() != 0 || data.GetResult() == nil {
		return out, fmt.Errorf("群成员回执无效")
	}
	if c := contact.Instance.Get(id); c != nil {
		out.Name = c.Nickname
	}
	for _, m := range data.GetResult().GetList() {
		avatar := m.GetSmallAvatarUrl()
		if avatar == "" {
			avatar = m.GetBigAvatarUrl()
		}
		out.Members = append(out.Members, dashboard.RoomMember{ID: m.GetUsername(), Name: m.GetNickname(), DisplayName: m.GetDisplayName(), Avatar: avatar})
		if self := contact.Instance.GetSelf(); self != nil && m.GetUsername() == self.Username {
			out.SelfName = m.GetDisplayName()
			if out.SelfName == "" {
				out.SelfName = m.GetNickname()
			}
		}
	}
	// 公告失败不丢弃已成功读取的成员，界面明确区分未设置与未获取。
	result := <-infoDone
	if result.err != nil || result.info == nil {
		out.Notice = "群公告暂未获取，可重新打开重试"
	} else {
		out.Announcement = result.info.GetAnnouncement()
	}
	return out, nil
}

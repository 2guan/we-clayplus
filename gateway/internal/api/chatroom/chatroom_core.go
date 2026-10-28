// Package chatroomapi 提供群聊服务的远程实现（通过 gRPC 调用 core）。
package chatroomapi

import (
	"fmt"
	"net/url"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
)

// remote 群聊服务 远程实现（通过 gRPC 调用 core）
type remote struct{}

// Get 获取 ChatroomService 单例（gRPC 模式）
var Get = sync.OnceValue(func() ChatroomService {
	return &remote{}
})

// Create 创建群聊 POST /api/chatroom
func (w remote) Create(members []string) (*CreateChatroomResponse, error) {
	var resp CreateChatroomResponse
	if err := api.GetCore().Post("/api/chatroom").Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// FacingCreate 面对面建群 POST /api/chatroom/facing
func (w remote) FacingCreate(password string, latitude, longitude float32, operate uint32) (*FacingCreateChatroomResponse, error) {
	var resp FacingCreateChatroomResponse
	if err := api.GetCore().Post("/api/chatroom/facing").Body(map[string]any{
		"password":  password,
		"latitude":  latitude,
		"longitude": longitude,
		"operate":   operate,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetInfo 获取群详细信息 GET /api/chatroom/info/{chatroom}
func (w remote) GetInfo(chatroomID string) (*GetChatroomInfoDetailResponse, error) {
	var resp GetChatroomInfoDetailResponse
	if err := api.GetCore().Get(fmt.Sprintf("/api/chatroom/info/%s", url.PathEscape(chatroomID))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (w remote) ListMembers(chatroomID string) (*ListMembersResponse, error) {
	var resp ListMembersResponse
	if err := api.GetCore().Get("/api/chatroom/members/%s").Path(chatroomID).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetMemberDetail 获取群成员详情 POST /api/chatroom/members/detail/{chatroom}
func (w remote) GetMemberDetail(chatroomID string, members []string) (*GetChatroomMembersResponse, error) {
	var resp GetChatroomMembersResponse
	if err := api.GetCore().Post("/api/chatroom/members/detail/%s").Path(chatroomID).Body(members).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetQRCode 获取群二维码 GET /api/chatroom/qrcode/{chatroom}
func (w remote) GetQRCode(chatroomID string) (*GetChatroomQRCodeResponse, error) {
	var resp GetChatroomQRCodeResponse
	if err := api.GetCore().Get(fmt.Sprintf("/api/chatroom/qrcode/%s", url.PathEscape(chatroomID))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AddMember 添加群成员 POST /api/chatroom/members/{chatroom}
func (w remote) AddMember(chatroomID string, members []string) (*AddChatroomMemberResponse, error) {
	var resp AddChatroomMemberResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/chatroom/members/%s", url.PathEscape(chatroomID))).Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// InviteMember 邀请群成员 POST /api/chatroom/invite/{chatroom}
func (w remote) InviteMember(chatroomID string, members []string) (*InviteChatroomMemberResponse, error) {
	var resp InviteChatroomMemberResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/chatroom/invite/%s", url.PathEscape(chatroomID))).Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RemoveMember 移除群成员 DELETE /api/chatroom/members/{chatroom} (with body)
func (w remote) RemoveMember(chatroomID string, members []string) (*RemoveChatroomMemberResponse, error) {
	var resp RemoveChatroomMemberResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/chatroom/members/%s", url.PathEscape(chatroomID))).Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetName 设置群名称 PUT /api/chatroom/name/{chatroom}?name=xxx
func (w remote) SetName(chatroomID, name string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/chatroom/name/%s", url.PathEscape(chatroomID))).Query("name", name).DoProto(&resp); err != nil {
		return nil, err
	}
	if resp.GetResult().Code == nil {
		// packed 命令状态经 JSON 丢失时，只读确认，不重发写操作。
		detail, err := contactapi.Get().Detail([]string{chatroomID})
		if err != nil {
			return nil, fmt.Errorf("群名修改回执已收到，但读回失败，结果未确认，请勿自动重试: %w", err)
		}
		list := detail.GetContactList()
		if len(list) != 1 || list[0].GetUsername().GetValue() != chatroomID || list[0].GetNickname().GetValue() != name {
			return nil, fmt.Errorf("群名修改回执已收到，但读回不匹配，结果未确认，请勿自动重试")
		}
	}
	return &resp, nil
}

// SetDisplayName 设置当前账号在群内的显示名 PUT /api/chatroom/display-name/{chatroom}
func (w remote) SetDisplayName(chatroomID, displayName string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/chatroom/display-name/%s", url.PathEscape(chatroomID))).Body(map[string]string{"display_name": displayName}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetAnnouncement 设置群公告 PUT /api/chatroom/announcement/{chatroom}
func (w remote) SetAnnouncement(chatroomID, content string) (*SetAnnouncementResponse, error) {
	var resp SetAnnouncementResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/chatroom/announcement/%s", url.PathEscape(chatroomID))).Body(map[string]any{"content": content}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetRemark 设置群备注 PUT /api/chatroom/remark/{chatroom}?remark=xxx
func (w remote) SetRemark(chatroomID, remark string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/chatroom/remark/%s", url.PathEscape(chatroomID))).Query("remark", remark).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetContactList 保存到通讯录 PUT /api/chatroom/contact-list/{chatroom}?save=xxx
func (w remote) SetContactList(chatroomID string, save bool) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/chatroom/contact-list/%s", url.PathEscape(chatroomID))).Query("save", save).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetAdmin 设置群管理员 POST /api/chatroom/admins/{chatroom}
func (w remote) SetAdmin(chatroomID string, members []string) (*ChatroomAdminResponse, error) {
	var resp ChatroomAdminResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/chatroom/admins/%s", url.PathEscape(chatroomID))).Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RemoveAdmin 移除群管理员 DELETE /api/chatroom/admins/{chatroom} (with body)
func (w remote) RemoveAdmin(chatroomID string, members []string) (*ChatroomAdminResponse, error) {
	var resp ChatroomAdminResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/chatroom/admins/%s", url.PathEscape(chatroomID))).Body(map[string]any{"members": members}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TransferOwner 转让群主 POST /api/chatroom/transfer/{chatroom}?new_owner=xxx
func (w remote) TransferOwner(chatroomID, newOwner string) (*ChatroomAdminResponse, error) {
	var resp ChatroomAdminResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/chatroom/transfer/%s", url.PathEscape(chatroomID))).Query("new_owner", newOwner).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Quit 退出群聊 DELETE /api/chatroom/quit/{chatroom}
func (w remote) Quit(chatroomID string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/chatroom/quit/%s", url.PathEscape(chatroomID))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ScanJoin 扫码进群 POST /api/chatroom/join/scan
func (w remote) ScanJoin(qrcodeURL string) (*JoinResult, error) {
	var resp struct {
		ChatroomID string `json:"chatroomID"`
		Message    string `json:"message"`
	}
	if err := api.GetCore().Post("/api/chatroom/join/scan").Body(map[string]any{"url": qrcodeURL}).DoJson(&resp); err != nil {
		return nil, err
	}
	return &JoinResult{
		ChatroomId: resp.ChatroomID,
		Message:    resp.Message,
	}, nil
}

// ScanJoinEnterprise 企业微信扫码进群
func (w remote) ScanJoinEnterprise(qrcodeURL string) (*JoinResult, error) {
	return w.ScanJoin(qrcodeURL)
}

// ConsentJoin 同意入群邀请 POST /api/chatroom/join/consent
func (w remote) ConsentJoin(inviteURL string) (*JoinResult, error) {
	var resp struct {
		ChatroomID string `json:"chatroomID"`
		Message    string `json:"message"`
	}
	if err := api.GetCore().Post("/api/chatroom/join/consent").Body(map[string]any{"url": inviteURL}).DoJson(&resp); err != nil {
		return nil, err
	}
	return &JoinResult{
		ChatroomId: resp.ChatroomID,
		Message:    resp.Message,
	}, nil
}

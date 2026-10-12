// Package chatroomapi 提供群聊服务的 API 接口定义。
package chatroomapi

// ChatroomService 群聊服务 API 接口（返回 API proto 类型）
type ChatroomService interface {
	// Create 创建群聊
	Create(members []string) (*CreateChatroomResponse, error)
	// FacingCreate 面对面建群
	FacingCreate(password string, latitude, longitude float32, operate uint32) (*FacingCreateChatroomResponse, error)
	// GetInfo 获取群详细信息
	GetInfo(chatroomID string) (*GetChatroomInfoDetailResponse, error)
	// ListMembers 获取群成员列表
	ListMembers(chatroomID string) (*ListMembersResponse, error)
	// GetMemberDetail 获取群成员详情
	GetMemberDetail(chatroomID string, members []string) (*GetChatroomMembersResponse, error)
	// GetQRCode 获取群二维码
	GetQRCode(chatroomID string) (*GetChatroomQRCodeResponse, error)
	// AddMember 添加群成员
	AddMember(chatroomID string, members []string) (*AddChatroomMemberResponse, error)
	// InviteMember 邀请群成员
	InviteMember(chatroomID string, members []string) (*InviteChatroomMemberResponse, error)
	// RemoveMember 移除群成员
	RemoveMember(chatroomID string, members []string) (*RemoveChatroomMemberResponse, error)
	// SetName 设置群名称
	SetName(chatroomID, name string) (*OperateResponse, error)
	// SetAnnouncement 设置群公告
	SetAnnouncement(chatroomID, content string) (*SetAnnouncementResponse, error)
	// SetRemark 设置群备注
	SetRemark(chatroomID, remark string) (*OperateResponse, error)
	// SetContactList 保存到通讯录
	SetContactList(chatroomID string, save bool) (*OperateResponse, error)
	// SetAdmin 设置群管理员
	SetAdmin(chatroomID string, members []string) (*ChatroomAdminResponse, error)
	// RemoveAdmin 移除群管理员
	RemoveAdmin(chatroomID string, members []string) (*ChatroomAdminResponse, error)
	// TransferOwner 转让群主
	TransferOwner(chatroomID, newOwner string) (*ChatroomAdminResponse, error)
	// Quit 退出群聊
	Quit(chatroomID string) (*OperateResponse, error)
	// ScanJoin 扫码进群
	ScanJoin(qrcodeURL string) (*JoinResult, error)
	// ScanJoinEnterprise 企业微信扫码进群
	ScanJoinEnterprise(qrcodeURL string) (*JoinResult, error)
	// ConsentJoin 同意入群邀请
	ConsentJoin(inviteURL string) (*JoinResult, error)
}

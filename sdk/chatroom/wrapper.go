package chatroom

import (
	"context"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程群聊服务
type Client struct {
	Client ChatroomServiceClient
}

var _ Ability = (*Client)(nil)

func (c Client) Save(chatroom string, save bool) (*Save_Response, error) {
	return c.Client.Save(context.Background(), &Save_Request{Chatroom: chatroom, Save: save})
}

func (c Client) ListMembers(chatroom string) []*Member {
	if response, err := c.Client.ListMembers(context.Background(), &ListMembers_Request{Chatroom: chatroom}); err != nil {
		return nil
	} else {
		return response.Members
	}
}

func (c Client) GetMembersDetail(chatroom string, members []string) []*Member {
	if response, err := c.Client.GetMembersDetail(context.Background(), &GetMemberDetail_Request{Chatroom: chatroom, Members: members}); err != nil {
		return nil
	} else {
		return response.Members
	}
}

// GetMember 获取群成员信息
func (c Client) GetMember(chatroom string, member string) *Member {
	if response, err := c.Client.GetMember(context.Background(), &GetMember_Request{Chatroom: chatroom, Member: member}); err != nil {
		return nil
	} else {
		return response.Member
	}
}

// Create 创建群聊
func (c Client) Create(members []string) (*Create_Response, error) {
	return c.Client.Create(context.Background(), &Create_Request{Members: members})
}

// FacingCreate 面对面建群
func (c Client) FacingCreate(password string, latitude, longitude float32, operate uint32) (*FacingCreate_Response, error) {
	return c.Client.FacingCreate(context.Background(), &FacingCreate_Request{
		Password:  password,
		Latitude:  latitude,
		Longitude: longitude,
		Operate:   operate,
	})
}

// GetInfo 获取群信息
func (c Client) GetInfo(chatroom string) (*GetInfo_Response, error) {
	return c.Client.GetInfo(context.Background(), &GetInfo_Request{Chatroom: chatroom})
}

// GetQRCode 获取群二维码
func (c Client) GetQRCode(chatroom string) (*GetQRCode_Response, error) {
	return c.Client.GetQRCode(context.Background(), &GetQRCode_Request{Chatroom: chatroom})
}

// AddMember 添加群成员
func (c Client) AddMember(chatroom string, members []string) (*AddMember_Response, error) {
	return c.Client.AddMember(context.Background(), &AddMember_Request{
		Chatroom: chatroom,
		Members:  members,
	})
}

// InviteMember 邀请群成员
func (c Client) InviteMember(chatroom string, members []string) (*InviteMember_Response, error) {
	return c.Client.InviteMember(context.Background(), &InviteMember_Request{
		Chatroom: chatroom,
		Members:  members,
	})
}

// RemoveMember 移除群成员
func (c Client) RemoveMember(chatroom string, members []string) (*RemoveMember_Response, error) {
	return c.Client.RemoveMember(context.Background(), &RemoveMember_Request{
		Chatroom: chatroom,
		Members:  members,
	})
}

// SetName 设置群名称
func (c Client) SetName(chatroom, name string) (*SetName_Response, error) {
	return c.Client.SetName(context.Background(), &SetName_Request{
		Chatroom: chatroom,
		Name:     name,
	})
}

// SetAnnouncement 设置群公告
func (c Client) SetAnnouncement(chatroom, content string) (*SetAnnouncement_Response, error) {
	return c.Client.SetAnnouncement(context.Background(), &SetAnnouncement_Request{
		Chatroom: chatroom,
		Content:  content,
	})
}

// SetRemark 设置群备注
func (c Client) SetRemark(chatroom, remark string) (*SetRemark_Response, error) {
	return c.Client.SetRemark(context.Background(), &SetRemark_Request{
		Chatroom: chatroom,
		Remark:   remark,
	})
}

// SetContactList 保存到通讯录
func (c Client) SetContactList(chatroom string, save bool) (*Save_Response, error) {
	return c.Client.Save(context.Background(), &Save_Request{
		Chatroom: chatroom,
		Save:     save,
	})
}

// SetAdmin 设置群管理员
func (c Client) SetAdmin(chatroom string, members []string) (*SetAdmin_Response, error) {
	return c.Client.SetAdmin(context.Background(), &SetAdmin_Request{
		Chatroom: chatroom,
		Members:  members,
	})
}

// RemoveAdmin 移除群管理员
func (c Client) RemoveAdmin(chatroom string, members []string) (*RemoveAdmin_Response, error) {
	return c.Client.RemoveAdmin(context.Background(), &RemoveAdmin_Request{
		Chatroom: chatroom,
		Members:  members,
	})
}

// TransferOwner 转让群主
func (c Client) TransferOwner(chatroom, newOwner string) (*TransferOwner_Response, error) {
	return c.Client.TransferOwner(context.Background(), &TransferOwner_Request{
		Chatroom: chatroom,
		NewOwner: newOwner,
	})
}

// Quit 退出群聊
func (c Client) Quit(chatroom string) (*Quit_Response, error) {
	return c.Client.Quit(context.Background(), &Quit_Request{Chatroom: chatroom})
}

// ScanJoin 扫码进群
func (c Client) ScanJoin(qrcodeURL string) (*ScanJoin_Response, error) {
	return c.Client.ScanJoin(context.Background(), &ScanJoin_Request{QrcodeUrl: qrcodeURL})
}

// ScanJoinEnterprise 企业微信扫码进群
func (c Client) ScanJoinEnterprise(qrcodeURL string) (*ScanJoin_Response, error) {
	return c.Client.ScanJoinEnterprise(context.Background(), &ScanJoin_Request{QrcodeUrl: qrcodeURL})
}

// ConsentJoin 同意入群邀请
func (c Client) ConsentJoin(inviteURL string) (*ConsentJoin_Response, error) {
	return c.Client.ConsentJoin(context.Background(), &ConsentJoin_Request{InviteUrl: inviteURL})
}

// GetChatroomByKey 按键查询缓存群信息（gRPC Client 不支持，返回未找到）
func (c Client) GetChatroomByKey(key string) (*Chatroom, bool) {
	return nil, false
}

// GetChatroomByStrategy 按策略查询缓存群信息（gRPC Client 不支持，返回未找到）
func (c Client) GetChatroomByStrategy(key string, strategy RetrievalType) (*Chatroom, bool) {
	return nil, false
}

// GetChatroomMembers 获取缓存群成员列表（gRPC Client 不支持，返回未找到）
func (c Client) GetChatroomMembers(chatroom string) ([]*Member, bool) {
	return nil, false
}

// Server 实现 ChatroomServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedChatroomServiceServer
	Impl Ability
}

var _ ChatroomServiceServer = (*Server)(nil)

// Create 创建群聊
func (s Server) Create(ctx context.Context, request *Create_Request) (*Create_Response, error) {
	return s.Impl.Create(request.Members)
}

// FacingCreate 面对面建群
func (s Server) FacingCreate(ctx context.Context, request *FacingCreate_Request) (*FacingCreate_Response, error) {
	return s.Impl.FacingCreate(request.Password, request.Latitude, request.Longitude, request.Operate)
}

// GetInfo 获取群信息
func (s Server) GetInfo(ctx context.Context, request *GetInfo_Request) (*GetInfo_Response, error) {
	return s.Impl.GetInfo(request.Chatroom)
}

// GetQRCode 获取群二维码
func (s Server) GetQRCode(ctx context.Context, request *GetQRCode_Request) (*GetQRCode_Response, error) {
	return s.Impl.GetQRCode(request.Chatroom)
}

// AddMember 添加群成员
func (s Server) AddMember(ctx context.Context, request *AddMember_Request) (*AddMember_Response, error) {
	return s.Impl.AddMember(request.Chatroom, request.Members)
}

// InviteMember 邀请群成员
func (s Server) InviteMember(ctx context.Context, request *InviteMember_Request) (*InviteMember_Response, error) {
	return s.Impl.InviteMember(request.Chatroom, request.Members)
}

// RemoveMember 移除群成员
func (s Server) RemoveMember(ctx context.Context, request *RemoveMember_Request) (*RemoveMember_Response, error) {
	return s.Impl.RemoveMember(request.Chatroom, request.Members)
}

// SetName 设置群名称
func (s Server) SetName(ctx context.Context, request *SetName_Request) (*SetName_Response, error) {
	return s.Impl.SetName(request.Chatroom, request.Name)
}

// SetAnnouncement 设置群公告
func (s Server) SetAnnouncement(ctx context.Context, request *SetAnnouncement_Request) (*SetAnnouncement_Response, error) {
	return s.Impl.SetAnnouncement(request.Chatroom, request.Content)
}

// SetRemark 设置群备注
func (s Server) SetRemark(ctx context.Context, request *SetRemark_Request) (*SetRemark_Response, error) {
	return s.Impl.SetRemark(request.Chatroom, request.Remark)
}

// Save 保存到通讯录
func (s Server) Save(ctx context.Context, request *Save_Request) (*Save_Response, error) {
	return s.Impl.Save(request.Chatroom, request.Save)
}

// SetAdmin 设置群管理员
func (s Server) SetAdmin(ctx context.Context, request *SetAdmin_Request) (*SetAdmin_Response, error) {
	return s.Impl.SetAdmin(request.Chatroom, request.Members)
}

// RemoveAdmin 移除群管理员
func (s Server) RemoveAdmin(ctx context.Context, request *RemoveAdmin_Request) (*RemoveAdmin_Response, error) {
	return s.Impl.RemoveAdmin(request.Chatroom, request.Members)
}

// TransferOwner 转让群主
func (s Server) TransferOwner(ctx context.Context, request *TransferOwner_Request) (*TransferOwner_Response, error) {
	return s.Impl.TransferOwner(request.Chatroom, request.NewOwner)
}

// Quit 退出群聊
func (s Server) Quit(ctx context.Context, request *Quit_Request) (*Quit_Response, error) {
	return s.Impl.Quit(request.Chatroom)
}

// ScanJoin 扫码进群
func (s Server) ScanJoin(ctx context.Context, request *ScanJoin_Request) (*ScanJoin_Response, error) {
	return s.Impl.ScanJoin(request.QrcodeUrl)
}

// ScanJoinEnterprise 企业微信扫码进群
func (s Server) ScanJoinEnterprise(ctx context.Context, request *ScanJoin_Request) (*ScanJoin_Response, error) {
	return s.Impl.ScanJoinEnterprise(request.QrcodeUrl)
}

// ConsentJoin 同意入群邀请
func (s Server) ConsentJoin(ctx context.Context, request *ConsentJoin_Request) (*ConsentJoin_Response, error) {
	return s.Impl.ConsentJoin(request.InviteUrl)
}

func (s Server) GetMember(ctx context.Context, request *GetMember_Request) (*GetMember_Response, error) {
	return &GetMember_Response{Member: s.Impl.GetMember(request.Chatroom, request.Member)}, nil
}

func (s Server) ListMembers(ctx context.Context, request *ListMembers_Request) (*ListMembers_Response, error) {
	return &ListMembers_Response{Members: s.Impl.ListMembers(request.Chatroom)}, nil
}

func (s Server) GetMembersDetail(ctx context.Context, request *GetMemberDetail_Request) (*GetMemberDetail_Response, error) {
	return &GetMemberDetail_Response{Members: s.Impl.GetMembersDetail(request.Chatroom, request.Members)}, nil
}

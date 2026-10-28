package chatroom

import "time"

// RetrievalType 群信息检索策略
type RetrievalType int32

const (
	RetrievalType_RETRIEVAL_TYPE_GROUP_ID RetrievalType = 0
	RetrievalType_RETRIEVAL_TYPE_NAME     RetrievalType = 1
)

// ChatroomCache 群缓存结构
type ChatroomCache struct {
	data      map[string]*Chatroom
	updatedAt map[string]time.Time
	ttl       time.Duration // 预留 TTL，暂不使用
}

// NewChatroomCache 创建群缓存
func NewChatroomCache() *ChatroomCache {
	return &ChatroomCache{
		data:      make(map[string]*Chatroom),
		updatedAt: make(map[string]time.Time),
	}
}

// ChatroomMemberCache 群成员缓存结构
type ChatroomMemberCache struct {
	data      map[string][]*Member
	updatedAt map[string]time.Time
	ttl       time.Duration // 预留 TTL，暂不使用
}

// NewChatroomMemberCache 创建群成员缓存
func NewChatroomMemberCache() *ChatroomMemberCache {
	return &ChatroomMemberCache{
		data:      make(map[string][]*Member),
		updatedAt: make(map[string]time.Time),
	}
}

// Chatroom 群信息（SDK 缓存域模型）
type Chatroom struct {
	ChatroomId     string
	Name           string
	Announcement   string
	Owner          string
	Admins         []string
	Remark         string
	BigAvatarUrl   string
	SmallAvatarUrl string
}

// Ability 群聊能力接口（供插件嵌入使用）
type Ability interface {
	// Create 创建群聊
	Create(members []string) (*Create_Response, error)
	// FacingCreate 面对面建群
	FacingCreate(password string, latitude, longitude float32, operate uint32) (*FacingCreate_Response, error)
	// GetInfo 获取群信息
	GetInfo(chatroom string) (*GetInfo_Response, error)
	// GetQRCode 获取群二维码
	GetQRCode(chatroom string) (*GetQRCode_Response, error)
	// AddMember 添加群成员
	AddMember(chatroom string, members []string) (*AddMember_Response, error)
	// InviteMember 邀请群成员
	InviteMember(chatroom string, members []string) (*InviteMember_Response, error)
	// RemoveMember 移除群成员
	RemoveMember(chatroom string, members []string) (*RemoveMember_Response, error)
	// SetName 设置群名称
	SetName(chatroom, name string) (*SetName_Response, error)
	// SetAnnouncement 设置群公告
	SetAnnouncement(chatroom, content string) (*SetAnnouncement_Response, error)
	// SetRemark 设置群备注
	SetRemark(chatroom, remark string) (*SetRemark_Response, error)
	// Save 保存到通讯录
	Save(chatroom string, save bool) (*Save_Response, error)
	// SetAdmin 设置群管理员
	SetAdmin(chatroom string, members []string) (*SetAdmin_Response, error)
	// RemoveAdmin 移除群管理员
	RemoveAdmin(chatroom string, members []string) (*RemoveAdmin_Response, error)
	// TransferOwner 转让群主
	TransferOwner(chatroom, newOwner string) (*TransferOwner_Response, error)
	// Quit 退出群聊
	Quit(chatroom string) (*Quit_Response, error)
	// ScanJoin 扫码进群
	ScanJoin(qrcodeURL string) (*ScanJoin_Response, error)
	// ScanJoinEnterprise 企业微信扫码进群
	ScanJoinEnterprise(qrcodeURL string) (*ScanJoin_Response, error)
	// ConsentJoin 同意入群邀请
	ConsentJoin(inviteURL string) (*ConsentJoin_Response, error)

	// ListMembers 获取缓存群成员列表
	ListMembers(chatroom string) []*Member
	// GetMember 获取群成员信息
	GetMember(chatroom string, member string) *Member
	// GetMembersDetail 获取群成员详情
	GetMembersDetail(chatroom string, members []string) []*Member
}

// DisplayNameAbility 是可选扩展，避免新增能力破坏已有 Ability 实现。
type DisplayNameAbility interface {
	SetDisplayName(chatroom, displayName string) (*SetDisplayName_Response, error)
}

// Instance 群聊能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

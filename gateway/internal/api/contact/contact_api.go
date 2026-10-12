// Package contactapi 提供联系人服务的 API 接口定义。
package contactapi

// ContactService 联系人服务 API 接口（返回 API proto 类型）
type ContactService interface {
	// List 获取联系人列表（增量同步）
	List() ([]string, error)
	// Detail 获取联系人详细信息
	Detail(usernames []string) (*GetContactDetailResponse, error)
	// SetRemark 设置联系人备注
	SetRemark(username, remark string) (*OperateResponse, error)
	// Search 搜索联系人
	Search(keyword string, fromScene, searchScene uint32) (*SearchContactResponse, error)
	// Verify 通过好友验证
	Verify(v1, v2 string, scene int) (*VerifyUserResponse, error)
	// Request 发送好友申请
	Request(v1, v2, content string, operate, scene int) (*VerifyUserResponse, error)
	// BlacklistAdd 添加到黑名单
	BlacklistAdd(username string) (*OperateResponse, error)
	// BlacklistRemove 从黑名单移除
	BlacklistRemove(username string) (*OperateResponse, error)
	// Delete 删除联系人
	Delete(username string) (*OperateResponse, error)
	// LbsFind 附近的人
	LbsFind(latitude, longitude float32, operate uint32) (*LbsResponse, error)
	// UploadContact 上传通讯录匹配好友
	UploadContact(phones []string, currentPhone string, operate int32) (*UploadContactResponse, error)
}

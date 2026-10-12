package user

import "io"

// Ability 用户能力接口（供插件嵌入使用）
type Ability interface {
	// GetProfile 获取个人信息
	GetProfile() (*GetProfile_Response, error)
	// UpdateProfile 更新个人信息
	UpdateProfile(params UpdateProfileParams) (*UpdateProfile_Response, error)
	// UploadAvatar 上传当前账号头像（1 字节至 MaxAvatarSize），会修改微信资料。
	// 返回 core 原始回执；当前完成离线契约验证，仍待真机验收。失败时不要自动重传。
	UploadAvatar(reader io.Reader) (*UploadAvatar_Response, error)
	// GetQRCode 获取个人二维码
	GetQRCode(style int32) (*GetQRCode_Response, error)
	// SetPrivacy 设置隐私选项
	SetPrivacy(function, value int32) (*SetPrivacy_Response, error)
	// SetAlias 设置微信号
	SetAlias(alias string) (*SetAlias_Response, error)
	// VerifyPassword 验证密码
	VerifyPassword(password string) (*VerifyPassword_Response, error)
	// SetPassword 设置密码
	SetPassword(password, ticket string) (*SetPassword_Response, error)
	// SendVerifyMobile 发送手机验证码
	SendVerifyMobile(mobile string, opcode uint32) (*SendVerifyMobile_Response, error)
	// BindMobile 绑定手机号
	BindMobile(mobile, verifyCode string) (*BindMobile_Response, error)
	// BindEmail 绑定邮箱
	BindEmail(email string) (*BindEmail_Response, error)
	// SendVerifyEmail 发送验证邮件
	SendVerifyEmail() (*SendVerifyEmail_Response, error)
	// GetSafetyInfo 获取安全设备列表
	GetSafetyInfo() (*GetSafetyInfo_Response, error)
	// DelSafeDevice 删除安全设备
	DelSafeDevice(uuid string) (*DelSafeDevice_Response, error)
	// ReportMotion 上报运动步数
	ReportMotion(deviceID, deviceType string, stepCount int64) (*ReportMotion_Response, error)
	// GetBoundHardDevices 获取绑定的硬件设备列表
	GetBoundHardDevices() (*GetBoundHardDevices_Response, error)
	// GetCert 获取证书信息
	GetCert(currentVersion uint32) (*GetCert_Response, error)
}

// UpdateProfileParams 更新个人信息参数
type UpdateProfileParams struct {
	NickName  string
	Sex       int32
	Country   string
	Province  string
	City      string
	Signature string
}

// Instance 用户能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

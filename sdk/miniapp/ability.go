package miniapp

// Ability 小程序能力接口（供插件嵌入使用）
type Ability interface {
	// JSLogin 小程序 JS 登录授权
	JSLogin(appID string) (*JSLogin_Response, error)
	// QrcodeAuthLogin 小程序扫码授权登录
	QrcodeAuthLogin(uuid string) (*QrcodeAuthLogin_Response, error)
	// GetRuntimeSession 获取小程序运行时会话
	GetRuntimeSession(appID string) (*GetRuntimeSession_Response, error)
	// GetSessionQRCode 获取小程序会话二维码
	GetSessionQRCode(appID string) (*GetSessionQRCode_Response, error)
	// OperateWxData 操作小程序数据
	OperateWxData(appID string, data []byte, opt int32) (*OperateWxData_Response, error)
	// CloudCallFunction 调用小程序云函数
	CloudCallFunction(appID string, data []byte) (*CloudCallFunction_Response, error)
	// SendVerifyCode 发送手机验证码
	SendVerifyCode(appID, mobile string, opcode int) (*SendVerifyCode_Response, error)
	// CheckVerifyCode 校验手机验证码
	CheckVerifyCode(appID, mobile, code string, opcode int) (*CheckVerifyCode_Response, error)
	// AddMobile 绑定手机号
	AddMobile(appID, mobile, code string) (*AddMobile_Response, error)
	// DelMobile 解绑手机号
	DelMobile(appID, mobile string) (*DelMobile_Response, error)
	// GetAllMobile 获取已绑定手机号列表
	GetAllMobile(appID string) (*GetAllMobile_Response, error)
	// GetRandomAvatar 获取随机头像
	GetRandomAvatar(appID string) (*GetRandomAvatar_Response, error)
	// AddAvatar 设置头像
	AddAvatar(appID, nickname, afilekey string) (*AddAvatar_Response, error)
	// UploadAvatarImg 上传自定义头像图片
	UploadAvatarImg(appID, jpglink string) (*UploadAvatarImg_Response, error)
	// GetRecord 获取小程序使用记录
	GetRecord() (*GetRecord_Response, error)
	// AddRecord 添加小程序使用记录
	AddRecord(username string) (*AddRecord_Response, error)
	// GetUserOpenID 获取用户 OpenID
	GetUserOpenID(appID, username string) (*GetUserOpenID_Response, error)
	// OauthSdkApp SDK OAuth 确认授权
	OauthSdkApp(appID string, scope []string, operate uint32) (*OauthSdkApp_Response, error)
	// ThirdAppGrant 第三方 APP OAuth 授权
	ThirdAppGrant(appID, oauthURL string) (*ThirdAppGrant_Response, error)
}

// Instance 小程序能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

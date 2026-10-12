// Package miniappapi 提供小程序服务的 API 接口定义。
package miniappapi

// MiniAppService 小程序服务 API 接口（纯协议层通信）
type MiniAppService interface {
	// JSLogin 小程序 JS 登录授权
	JSLogin(appID string) (*JSLoginResponse, error)
	// QrcodeAuthLogin 小程序扫码授权登录
	QrcodeAuthLogin(uuid string) (*StringResponse, error)
	// GetRuntimeSession 获取小程序运行时会话
	GetRuntimeSession(appID string) (*GetRuntimeSessionResponse, error)
	// GetSessionQRCode 获取小程序会话二维码
	GetSessionQRCode(appID string) (*StringResponse, error)
	// OperateWxData 操作小程序数据
	OperateWxData(appID string, data []byte, opt int32) (*JSOperateResponse, error)
	// CloudCallFunction 调用小程序云函数
	CloudCallFunction(appID string, data []byte) (*JSOperateResponse, error)
	// SendVerifyCode 发送手机验证码
	SendVerifyCode(appID, mobile string, opcode int) (*PostVerifyCodeResponse, error)
	// CheckVerifyCode 校验手机验证码
	CheckVerifyCode(appID, mobile, code string, opcode int) (*CheckVerifyCodeResponse, error)
	// AddMobile 绑定手机号
	AddMobile(appID, mobile, code string) (*CheckVerifyCodeResponse, error)
	// DelMobile 解绑手机号
	DelMobile(appID, mobile string) (*OperateResponse, error)
	// GetAllMobile 获取已绑定手机号列表
	GetAllMobile(appID string) (*OperateResponse, error)
	// GetRandomAvatar 获取随机头像
	GetRandomAvatar(appID string) (*OAuthGetRandomAvatarResponse, error)
	// AddAvatar 设置头像
	AddAvatar(appID, nickname, afilekey string) (*OperateResponse, error)
	// UploadAvatarImg 上传自定义头像图片
	UploadAvatarImg(appID, jpglink string) (*OAuthAddAvatarImageResponse, error)
	// GetRecord 获取小程序使用记录
	GetRecord() (*GetUsageRecordResponse, error)
	// AddRecord 添加小程序使用记录
	AddRecord(username string) (*OperateResponse, error)
	// GetUserOpenID 获取用户 OpenID
	GetUserOpenID(appID, username string) (*BizJsApiGetUserOpenIdResponse, error)
	// OauthSdkApp SDK OAuth 确认授权
	OauthSdkApp(appID string, scope []string, operate uint32) (*SDKOAuthAuthorizeConfirmResponse, error)
	// ThirdAppGrant 第三方 APP OAuth 授权
	ThirdAppGrant(appID, oauthURL string) (*StringResponse, error)
}

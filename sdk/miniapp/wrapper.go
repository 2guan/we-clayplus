package miniapp

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程小程序服务
type Client struct {
	Client MiniAppServiceClient
}

var _ Ability = (*Client)(nil)

// JSLogin 小程序 JS 登录授权
func (c Client) JSLogin(appID string) (*JSLogin_Response, error) {
	return c.Client.JSLogin(context.Background(), &JSLogin_Request{AppId: appID})
}

// QrcodeAuthLogin 小程序扫码授权登录
func (c Client) QrcodeAuthLogin(uuid string) (*QrcodeAuthLogin_Response, error) {
	return c.Client.QrcodeAuthLogin(context.Background(), &QrcodeAuthLogin_Request{Uuid: uuid})
}

// GetRuntimeSession 获取小程序运行时会话
func (c Client) GetRuntimeSession(appID string) (*GetRuntimeSession_Response, error) {
	return c.Client.GetRuntimeSession(context.Background(), &GetRuntimeSession_Request{AppId: appID})
}

// GetSessionQRCode 获取小程序会话二维码
func (c Client) GetSessionQRCode(appID string) (*GetSessionQRCode_Response, error) {
	return c.Client.GetSessionQRCode(context.Background(), &GetSessionQRCode_Request{AppId: appID})
}

// OperateWxData 操作小程序数据
func (c Client) OperateWxData(appID string, data []byte, opt int32) (*OperateWxData_Response, error) {
	return c.Client.OperateWxData(context.Background(), &OperateWxData_Request{
		AppId: appID,
		Data:  data,
		Opt:   opt,
	})
}

// CloudCallFunction 调用小程序云函数
func (c Client) CloudCallFunction(appID string, data []byte) (*CloudCallFunction_Response, error) {
	return c.Client.CloudCallFunction(context.Background(), &CloudCallFunction_Request{
		AppId: appID,
		Data:  data,
	})
}

// SendVerifyCode 发送手机验证码
func (c Client) SendVerifyCode(appID, mobile string, opcode int) (*SendVerifyCode_Response, error) {
	return c.Client.SendVerifyCode(context.Background(), &SendVerifyCode_Request{
		AppId:  appID,
		Mobile: mobile,
		Opcode: int32(opcode),
	})
}

// CheckVerifyCode 校验手机验证码
func (c Client) CheckVerifyCode(appID, mobile, code string, opcode int) (*CheckVerifyCode_Response, error) {
	return c.Client.CheckVerifyCode(context.Background(), &CheckVerifyCode_Request{
		AppId:  appID,
		Mobile: mobile,
		Code:   code,
		Opcode: int32(opcode),
	})
}

// AddMobile 绑定手机号
func (c Client) AddMobile(appID, mobile, code string) (*AddMobile_Response, error) {
	return c.Client.AddMobile(context.Background(), &AddMobile_Request{
		AppId:  appID,
		Mobile: mobile,
		Code:   code,
	})
}

// DelMobile 解绑手机号
func (c Client) DelMobile(appID, mobile string) (*DelMobile_Response, error) {
	return c.Client.DelMobile(context.Background(), &DelMobile_Request{
		AppId:  appID,
		Mobile: mobile,
	})
}

// GetAllMobile 获取已绑定手机号列表
func (c Client) GetAllMobile(appID string) (*GetAllMobile_Response, error) {
	return c.Client.GetAllMobile(context.Background(), &GetAllMobile_Request{AppId: appID})
}

// GetRandomAvatar 获取随机头像
func (c Client) GetRandomAvatar(appID string) (*GetRandomAvatar_Response, error) {
	return c.Client.GetRandomAvatar(context.Background(), &GetRandomAvatar_Request{AppId: appID})
}

// AddAvatar 设置头像
func (c Client) AddAvatar(appID, nickname, afilekey string) (*AddAvatar_Response, error) {
	return c.Client.AddAvatar(context.Background(), &AddAvatar_Request{
		AppId:    appID,
		Nickname: nickname,
		Afilekey: afilekey,
	})
}

// UploadAvatarImg 上传自定义头像图片
func (c Client) UploadAvatarImg(appID, jpglink string) (*UploadAvatarImg_Response, error) {
	return c.Client.UploadAvatarImg(context.Background(), &UploadAvatarImg_Request{
		AppId:   appID,
		Jpglink: jpglink,
	})
}

// GetRecord 获取小程序使用记录
func (c Client) GetRecord() (*GetRecord_Response, error) {
	return c.Client.GetRecord(context.Background(), &GetRecord_Request{})
}

// AddRecord 添加小程序使用记录
func (c Client) AddRecord(username string) (*AddRecord_Response, error) {
	return c.Client.AddRecord(context.Background(), &AddRecord_Request{Username: username})
}

// GetUserOpenID 获取用户 OpenID
func (c Client) GetUserOpenID(appID, username string) (*GetUserOpenID_Response, error) {
	return c.Client.GetUserOpenID(context.Background(), &GetUserOpenID_Request{
		AppId:    appID,
		Username: username,
	})
}

// OauthSdkApp SDK OAuth 确认授权
func (c Client) OauthSdkApp(appID string, scope []string, operate uint32) (*OauthSdkApp_Response, error) {
	return c.Client.OauthSdkApp(context.Background(), &OauthSdkApp_Request{
		AppId:   appID,
		Scope:   scope,
		Operate: operate,
	})
}

// ThirdAppGrant 第三方 APP OAuth 授权
func (c Client) ThirdAppGrant(appID, oauthURL string) (*ThirdAppGrant_Response, error) {
	return c.Client.ThirdAppGrant(context.Background(), &ThirdAppGrant_Request{
		AppId:    appID,
		OauthUrl: oauthURL,
	})
}

// Server 实现 MiniAppServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedMiniAppServiceServer
	Impl Ability
}

// JSLogin 小程序 JS 登录授权
func (s Server) JSLogin(ctx context.Context, request *JSLogin_Request) (*JSLogin_Response, error) {
	return s.Impl.JSLogin(request.AppId)
}

// QrcodeAuthLogin 小程序扫码授权登录
func (s Server) QrcodeAuthLogin(ctx context.Context, request *QrcodeAuthLogin_Request) (*QrcodeAuthLogin_Response, error) {
	return s.Impl.QrcodeAuthLogin(request.Uuid)
}

// GetRuntimeSession 获取小程序运行时会话
func (s Server) GetRuntimeSession(ctx context.Context, request *GetRuntimeSession_Request) (*GetRuntimeSession_Response, error) {
	return s.Impl.GetRuntimeSession(request.AppId)
}

// GetSessionQRCode 获取小程序会话二维码
func (s Server) GetSessionQRCode(ctx context.Context, request *GetSessionQRCode_Request) (*GetSessionQRCode_Response, error) {
	return s.Impl.GetSessionQRCode(request.AppId)
}

// OperateWxData 操作小程序数据
func (s Server) OperateWxData(ctx context.Context, request *OperateWxData_Request) (*OperateWxData_Response, error) {
	return s.Impl.OperateWxData(request.AppId, request.Data, request.Opt)
}

// CloudCallFunction 调用小程序云函数
func (s Server) CloudCallFunction(ctx context.Context, request *CloudCallFunction_Request) (*CloudCallFunction_Response, error) {
	return s.Impl.CloudCallFunction(request.AppId, request.Data)
}

// SendVerifyCode 发送手机验证码
func (s Server) SendVerifyCode(ctx context.Context, request *SendVerifyCode_Request) (*SendVerifyCode_Response, error) {
	return s.Impl.SendVerifyCode(request.AppId, request.Mobile, int(request.Opcode))
}

// CheckVerifyCode 校验手机验证码
func (s Server) CheckVerifyCode(ctx context.Context, request *CheckVerifyCode_Request) (*CheckVerifyCode_Response, error) {
	return s.Impl.CheckVerifyCode(request.AppId, request.Mobile, request.Code, int(request.Opcode))
}

// AddMobile 绑定手机号
func (s Server) AddMobile(ctx context.Context, request *AddMobile_Request) (*AddMobile_Response, error) {
	return s.Impl.AddMobile(request.AppId, request.Mobile, request.Code)
}

// DelMobile 解绑手机号
func (s Server) DelMobile(ctx context.Context, request *DelMobile_Request) (*DelMobile_Response, error) {
	return s.Impl.DelMobile(request.AppId, request.Mobile)
}

// GetAllMobile 获取已绑定手机号列表
func (s Server) GetAllMobile(ctx context.Context, request *GetAllMobile_Request) (*GetAllMobile_Response, error) {
	return s.Impl.GetAllMobile(request.AppId)
}

// GetRandomAvatar 获取随机头像
func (s Server) GetRandomAvatar(ctx context.Context, request *GetRandomAvatar_Request) (*GetRandomAvatar_Response, error) {
	return s.Impl.GetRandomAvatar(request.AppId)
}

// AddAvatar 设置头像
func (s Server) AddAvatar(ctx context.Context, request *AddAvatar_Request) (*AddAvatar_Response, error) {
	return s.Impl.AddAvatar(request.AppId, request.Nickname, request.Afilekey)
}

// UploadAvatarImg 上传自定义头像图片
func (s Server) UploadAvatarImg(ctx context.Context, request *UploadAvatarImg_Request) (*UploadAvatarImg_Response, error) {
	return s.Impl.UploadAvatarImg(request.AppId, request.Jpglink)
}

// GetRecord 获取小程序使用记录
func (s Server) GetRecord(ctx context.Context, request *GetRecord_Request) (*GetRecord_Response, error) {
	return s.Impl.GetRecord()
}

// AddRecord 添加小程序使用记录
func (s Server) AddRecord(ctx context.Context, request *AddRecord_Request) (*AddRecord_Response, error) {
	return s.Impl.AddRecord(request.Username)
}

// GetUserOpenID 获取用户 OpenID
func (s Server) GetUserOpenID(ctx context.Context, request *GetUserOpenID_Request) (*GetUserOpenID_Response, error) {
	return s.Impl.GetUserOpenID(request.AppId, request.Username)
}

// OauthSdkApp SDK OAuth 确认授权
func (s Server) OauthSdkApp(ctx context.Context, request *OauthSdkApp_Request) (*OauthSdkApp_Response, error) {
	return s.Impl.OauthSdkApp(request.AppId, request.Scope, request.Operate)
}

// ThirdAppGrant 第三方 APP OAuth 授权
func (s Server) ThirdAppGrant(ctx context.Context, request *ThirdAppGrant_Request) (*ThirdAppGrant_Response, error) {
	return s.Impl.ThirdAppGrant(request.AppId, request.OauthUrl)
}

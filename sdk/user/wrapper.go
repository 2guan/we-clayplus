package user

import (
	"bytes"
	"context"
	"io"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程用户服务。
type Client struct {
	Client UserServiceClient
}

var _ Ability = (*Client)(nil)

// GetProfile 获取个人信息
func (c Client) GetProfile() (*GetProfile_Response, error) {
	return c.Client.GetProfile(context.Background(), &GetProfile_Request{})
}

// UpdateProfile 更新个人信息
func (c Client) UpdateProfile(params UpdateProfileParams) (*UpdateProfile_Response, error) {
	return c.Client.UpdateProfile(context.Background(), &UpdateProfile_Request{
		Nickname:  params.NickName,
		Sex:       params.Sex,
		Country:   params.Country,
		Province:  params.Province,
		City:      params.City,
		Signature: params.Signature,
	})
}

// UploadAvatar 上传头像
func (c Client) UploadAvatar(reader io.Reader) (*UploadAvatar_Response, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return c.Client.UploadAvatar(context.Background(), &UploadAvatar_Request{Data: data})
}

// GetQRCode 获取个人二维码
func (c Client) GetQRCode(style int32) (*GetQRCode_Response, error) {
	return c.Client.GetQRCode(context.Background(), &GetQRCode_Request{Style: style})
}

// SetPrivacy 设置隐私选项
func (c Client) SetPrivacy(function, value int32) (*SetPrivacy_Response, error) {
	return c.Client.SetPrivacy(context.Background(), &SetPrivacy_Request{Function: function, Value: value})
}

// SetAlias 设置微信号
func (c Client) SetAlias(alias string) (*SetAlias_Response, error) {
	return c.Client.SetAlias(context.Background(), &SetAlias_Request{Alias: alias})
}

// VerifyPassword 验证密码
func (c Client) VerifyPassword(password string) (*VerifyPassword_Response, error) {
	return c.Client.VerifyPassword(context.Background(), &VerifyPassword_Request{Password: password})
}

// SetPassword 设置密码
func (c Client) SetPassword(password, ticket string) (*SetPassword_Response, error) {
	return c.Client.SetPassword(context.Background(), &SetPassword_Request{Password: password, Ticket: ticket})
}

// SendVerifyMobile 发送手机验证码
func (c Client) SendVerifyMobile(mobile string, opcode uint32) (*SendVerifyMobile_Response, error) {
	return c.Client.SendVerifyMobile(context.Background(), &SendVerifyMobile_Request{Mobile: mobile, Opcode: opcode})
}

// BindMobile 绑定手机号
func (c Client) BindMobile(mobile, verifyCode string) (*BindMobile_Response, error) {
	return c.Client.BindMobile(context.Background(), &BindMobile_Request{Mobile: mobile, VerifyCode: verifyCode})
}

// BindEmail 绑定邮箱
func (c Client) BindEmail(email string) (*BindEmail_Response, error) {
	return c.Client.BindEmail(context.Background(), &BindEmail_Request{Email: email})
}

// SendVerifyEmail 发送验证邮件
func (c Client) SendVerifyEmail() (*SendVerifyEmail_Response, error) {
	return c.Client.SendVerifyEmail(context.Background(), &SendVerifyEmail_Request{})
}

// GetSafetyInfo 获取安全设备列表
func (c Client) GetSafetyInfo() (*GetSafetyInfo_Response, error) {
	return c.Client.GetSafetyInfo(context.Background(), &GetSafetyInfo_Request{})
}

// DelSafeDevice 删除安全设备
func (c Client) DelSafeDevice(uuid string) (*DelSafeDevice_Response, error) {
	return c.Client.DelSafeDevice(context.Background(), &DelSafeDevice_Request{Uuid: uuid})
}

// ReportMotion 上报运动步数
func (c Client) ReportMotion(deviceID, deviceType string, stepCount int64) (*ReportMotion_Response, error) {
	return c.Client.ReportMotion(context.Background(), &ReportMotion_Request{
		DeviceId:   deviceID,
		DeviceType: deviceType,
		StepCount:  stepCount,
	})
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (c Client) GetBoundHardDevices() (*GetBoundHardDevices_Response, error) {
	return c.Client.GetBoundHardDevices(context.Background(), &GetBoundHardDevices_Request{})
}

// GetCert 获取证书信息
func (c Client) GetCert(currentVersion uint32) (*GetCert_Response, error) {
	return c.Client.GetCert(context.Background(), &GetCert_Request{CurrentVersion: currentVersion})
}

// Server 实现 UserServiceServer 接口，将 gRPC 请求委托给 Ability 实现。
type Server struct {
	UnimplementedUserServiceServer
	Impl Ability
}

// GetProfile 获取个人信息
func (s Server) GetProfile(ctx context.Context, request *GetProfile_Request) (*GetProfile_Response, error) {
	return s.Impl.GetProfile()
}

// UpdateProfile 更新个人信息
func (s Server) UpdateProfile(ctx context.Context, request *UpdateProfile_Request) (*UpdateProfile_Response, error) {
	return s.Impl.UpdateProfile(UpdateProfileParams{
		NickName:  request.Nickname,
		Sex:       request.Sex,
		Country:   request.Country,
		Province:  request.Province,
		City:      request.City,
		Signature: request.Signature,
	})
}

// UploadAvatar 上传头像
func (s Server) UploadAvatar(ctx context.Context, request *UploadAvatar_Request) (*UploadAvatar_Response, error) {
	return s.Impl.UploadAvatar(bytes.NewReader(request.Data))
}

// GetQRCode 获取个人二维码
func (s Server) GetQRCode(ctx context.Context, request *GetQRCode_Request) (*GetQRCode_Response, error) {
	return s.Impl.GetQRCode(request.Style)
}

// SetPrivacy 设置隐私选项
func (s Server) SetPrivacy(ctx context.Context, request *SetPrivacy_Request) (*SetPrivacy_Response, error) {
	return s.Impl.SetPrivacy(request.Function, request.Value)
}

// SetAlias 设置微信号
func (s Server) SetAlias(ctx context.Context, request *SetAlias_Request) (*SetAlias_Response, error) {
	return s.Impl.SetAlias(request.Alias)
}

// VerifyPassword 验证密码
func (s Server) VerifyPassword(ctx context.Context, request *VerifyPassword_Request) (*VerifyPassword_Response, error) {
	return s.Impl.VerifyPassword(request.Password)
}

// SetPassword 设置密码
func (s Server) SetPassword(ctx context.Context, request *SetPassword_Request) (*SetPassword_Response, error) {
	return s.Impl.SetPassword(request.Password, request.Ticket)
}

// SendVerifyMobile 发送手机验证码
func (s Server) SendVerifyMobile(ctx context.Context, request *SendVerifyMobile_Request) (*SendVerifyMobile_Response, error) {
	return s.Impl.SendVerifyMobile(request.Mobile, request.Opcode)
}

// BindMobile 绑定手机号
func (s Server) BindMobile(ctx context.Context, request *BindMobile_Request) (*BindMobile_Response, error) {
	return s.Impl.BindMobile(request.Mobile, request.VerifyCode)
}

// BindEmail 绑定邮箱
func (s Server) BindEmail(ctx context.Context, request *BindEmail_Request) (*BindEmail_Response, error) {
	return s.Impl.BindEmail(request.Email)
}

// SendVerifyEmail 发送验证邮件
func (s Server) SendVerifyEmail(ctx context.Context, request *SendVerifyEmail_Request) (*SendVerifyEmail_Response, error) {
	return s.Impl.SendVerifyEmail()
}

// GetSafetyInfo 获取安全设备列表
func (s Server) GetSafetyInfo(ctx context.Context, request *GetSafetyInfo_Request) (*GetSafetyInfo_Response, error) {
	return s.Impl.GetSafetyInfo()
}

// DelSafeDevice 删除安全设备
func (s Server) DelSafeDevice(ctx context.Context, request *DelSafeDevice_Request) (*DelSafeDevice_Response, error) {
	return s.Impl.DelSafeDevice(request.Uuid)
}

// ReportMotion 上报运动步数
func (s Server) ReportMotion(ctx context.Context, request *ReportMotion_Request) (*ReportMotion_Response, error) {
	return s.Impl.ReportMotion(request.DeviceId, request.DeviceType, request.StepCount)
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (s Server) GetBoundHardDevices(ctx context.Context, request *GetBoundHardDevices_Request) (*GetBoundHardDevices_Response, error) {
	return s.Impl.GetBoundHardDevices()
}

// GetCert 获取证书信息
func (s Server) GetCert(ctx context.Context, request *GetCert_Request) (*GetCert_Response, error) {
	return s.Impl.GetCert(request.CurrentVersion)
}

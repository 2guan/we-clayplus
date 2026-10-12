// Package userapi 提供用户服务的远程实现（通过 gRPC 调用 core）。
package userapi

import (
	"errors"
	"io"
	"strconv"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
	usersdk "github.com/we-clay/we-clay/sdk/user"
)

// remote 用户服务 远程实现（通过 gRPC 调用 core）。
type remote struct{}

// Get 获取 UserService 单例（gRPC 模式）。
var Get = sync.OnceValue(func() UserService {
	return &remote{}
})

// GetProfile 获取个人信息
func (w remote) GetProfile() (*GetProfileResponse, error) {
	var resp GetProfileResponse
	if err := api.GetCore().Get("/api/user/profile").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateProfile 更新个人信息
func (w remote) UpdateProfile(params UpdateProfileParams) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put("/api/user/profile").Body(map[string]any{
		"nickname":  params.NickName,
		"signature": params.Signature,
		"country":   params.Country, "province": params.Province, "city": params.City,
		"sex": params.Sex,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadAvatar 上传头像
func (w remote) UploadAvatar(reader io.Reader) (*UploadAvatarResponse, error) {
	if reader == nil {
		return nil, errors.New("头像 reader 不能为空")
	}
	data, err := io.ReadAll(io.LimitReader(reader, usersdk.MaxAvatarSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > usersdk.MaxAvatarSize {
		return nil, errors.New("头像数据必须为 1 字节至 16 MiB")
	}
	var resp UploadAvatarResponse
	if err := api.GetCore().Post("/api/user/avatar").Multipart(map[string][]byte{
		"avatar": data,
	}, nil).DoProto(&resp); err != nil {
		return nil, err
	}
	// core 在内部分块上传，回执中的 offset（如有）应为最终完成位置。
	// 字段可选，不能凭缺省 0 判断，也不能用请求长度填造服务器回执。
	if resp.Size != nil && resp.GetSize() != uint32(len(data)) || resp.Offset != nil && resp.GetOffset() != uint32(len(data)) {
		return nil, errors.New("头像上传回执长度或完成位置不匹配，结果未确认，请勿自动重试")
	}
	return &resp, nil
}

// GetQRCode 获取个人二维码
func (w remote) GetQRCode(style int32) (*GetQRCodeResponse, error) {
	var resp GetQRCodeResponse
	if err := api.GetCore().Get("/api/user/qrcode").Query("style", style).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetPrivacy 设置隐私选项
func (w remote) SetPrivacy(function, value int32) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put("/api/user/privacy").Body(map[string]any{
		"options": []map[string]any{{"type": function, "value": value}},
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetAlias 设置微信号
func (w remote) SetAlias(alias string) (*GeneralSetResponse, error) {
	var resp GeneralSetResponse
	if err := api.GetCore().Put("/api/user/alias").Body(map[string]any{
		"alias": alias,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// VerifyPassword 验证密码
func (w remote) VerifyPassword(password string) (*VerifyPasswordResponse, error) {
	var resp VerifyPasswordResponse
	if err := api.GetCore().Post("/api/user/password/verify").Body(map[string]any{
		"password": password,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetPassword 设置密码
func (w remote) SetPassword(password, ticket string) (*SetPasswordResponse, error) {
	var resp SetPasswordResponse
	if err := api.GetCore().Put("/api/user/password").Body(map[string]any{
		"new_password": password,
		"ticket":       ticket,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVerifyMobile 发送手机验证码
func (w remote) SendVerifyMobile(mobile string, opcode uint32) (*BindOpMobileResponse, error) {
	var resp BindOpMobileResponse
	if err := api.GetCore().Post("/api/user/mobile/verify-code").Body(map[string]any{
		"mobile":  mobile,
		"operate": opcode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BindMobile 绑定手机号
func (w remote) BindMobile(mobile, verifyCode string) (*BindOpMobileResponse, error) {
	var resp BindOpMobileResponse
	if err := api.GetCore().Post("/api/user/mobile").Body(map[string]any{
		"mobile":      mobile,
		"verify_code": verifyCode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BindEmail 绑定邮箱
func (w remote) BindEmail(email string) (*BindEmailResponse, error) {
	var resp BindEmailResponse
	if err := api.GetCore().Post("/api/user/email").Body(map[string]any{
		"email": email,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVerifyEmail 发送验证邮件
func (w remote) SendVerifyEmail() (*SendVerifyEmailResponse, error) {
	var resp SendVerifyEmailResponse
	if err := api.GetCore().Post("/api/user/email/verify").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSafetyInfo 获取安全设备列表
func (w remote) GetSafetyInfo() (*GetSafetyInfoResponse, error) {
	var resp GetSafetyInfoResponse
	if err := api.GetCore().Get("/api/user/safety/devices").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DelSafeDevice 删除安全设备
func (w remote) DelSafeDevice(uuid string) (*DeleteSafeDeviceResponse, error) {
	var resp DeleteSafeDeviceResponse
	if err := api.GetCore().Delete("/api/user/safety/devices").Body(map[string]any{
		"uuid": uuid,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReportMotion 上报运动步数
func (w remote) ReportMotion(deviceID, deviceType string, stepCount int64) (*UploadDeviceStepResponse, error) {
	var resp UploadDeviceStepResponse
	if err := api.GetCore().Post("/api/user/motion").Body(map[string]any{
		"device_id":   deviceID,
		"device_type": deviceType,
		"step_count":  stepCount,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBoundHardDevices 获取绑定的硬件设备列表
func (w remote) GetBoundHardDevices() (*GetBoundHardDevicesResponse, error) {
	var resp GetBoundHardDevicesResponse
	if err := api.GetCore().Get("/api/user/devices").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetCert 获取证书信息
func (w remote) GetCert(currentVersion uint32) (*GetCertResponse, error) {
	var resp GetCertResponse
	if err := api.GetCore().Get("/api/user/cert").
		Query("current_version", strconv.FormatUint(uint64(currentVersion), 10)).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

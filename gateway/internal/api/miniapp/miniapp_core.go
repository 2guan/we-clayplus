// Package miniappapi 提供小程序服务的远程实现（通过 gRPC 调用 core）。
package miniappapi

import (
	"errors"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 小程序服务 远程实现（通过 gRPC 调用 core）
type remote struct{}

// Get 获取 MiniAppService 单例（gRPC 模式）
var Get = sync.OnceValue(func() MiniAppService {
	return &remote{}
})

// JSLogin 小程序 JS 登录授权
func (w remote) JSLogin(appID string) (*JSLoginResponse, error) {
	var resp JSLoginResponse
	if err := api.GetCore().Post("/api/miniapp/login/js").Body(map[string]any{"app_id": appID}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// QrcodeAuthLogin 小程序扫码授权登录
func (w remote) QrcodeAuthLogin(uuid string) (*StringResponse, error) {
	var code string
	if err := api.GetCore().Post("/api/miniapp/login/qrcode").Body(map[string]any{"uuid": uuid}).DoJson(&code); err != nil {
		return nil, err
	}
	return &StringResponse{Value: code}, nil
}

// GetRuntimeSession 获取小程序运行时会话
func (w remote) GetRuntimeSession(appID string) (*GetRuntimeSessionResponse, error) {
	var resp GetRuntimeSessionResponse
	if err := api.GetCore().Post("/api/miniapp/session/runtime").Body(map[string]any{"app_id": appID}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSessionQRCode 获取小程序会话二维码
func (w remote) GetSessionQRCode(appID string) (*StringResponse, error) {
	var url string
	if err := api.GetCore().Post("/api/miniapp/session/qrcode").Body(map[string]any{"app_id": appID}).DoJson(&url); err != nil {
		return nil, err
	}
	return &StringResponse{Value: url}, nil
}

// OperateWxData 操作小程序数据
func (w remote) OperateWxData(appID string, data []byte, opt int32) (*JSOperateResponse, error) {
	var resp JSOperateResponse
	if err := api.GetCore().Post("/api/miniapp/operate").Body(map[string]any{
		"app_id":  appID,
		"data":    data,
		"operate": opt,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CloudCallFunction 调用小程序云函数
func (w remote) CloudCallFunction(appID string, data []byte) (*JSOperateResponse, error) {
	var resp JSOperateResponse
	if err := api.GetCore().Post("/api/miniapp/cloud/function").Body(map[string]any{
		"app_id": appID,
		"data":   data,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVerifyCode 发送手机验证码
func (w remote) SendVerifyCode(appID, mobile string, opcode int) (*PostVerifyCodeResponse, error) {
	var resp PostVerifyCodeResponse
	if err := api.GetCore().Post("/api/miniapp/mobile/send-code").Body(map[string]any{
		"app_id": appID,
		"mobile": mobile,
		"opcode": opcode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CheckVerifyCode 校验手机验证码
func (w remote) CheckVerifyCode(appID, mobile, code string, opcode int) (*CheckVerifyCodeResponse, error) {
	var resp CheckVerifyCodeResponse
	if err := api.GetCore().Post("/api/miniapp/mobile/check-code").Body(map[string]any{
		"app_id": appID,
		"mobile": mobile,
		"code":   code,
		"opcode": opcode,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AddMobile 绑定手机号
func (w remote) AddMobile(appID, mobile, code string) (*CheckVerifyCodeResponse, error) {
	var resp CheckVerifyCodeResponse
	if err := api.GetCore().Post("/api/miniapp/mobile").Body(map[string]any{
		"app_id": appID,
		"mobile": mobile,
		"code":   code,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DelMobile 解绑手机号
func (w remote) DelMobile(appID, mobile string) (*OperateResponse, error) {
	if _, err := api.GetCore().Delete("/api/miniapp/mobile").Body(map[string]any{
		"app_id": appID,
		"mobile": mobile,
	}).Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0}, nil
}

// GetAllMobile 获取已绑定手机号列表
func (w remote) GetAllMobile(appID string) (*OperateResponse, error) {
	if _, err := api.GetCore().Get("/api/miniapp/mobile").Query("app_id", appID).Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0}, nil
}

// GetRandomAvatar 获取随机头像
func (w remote) GetRandomAvatar(appID string) (*OAuthGetRandomAvatarResponse, error) {
	var resp OAuthGetRandomAvatarResponse
	if err := api.GetCore().Get("/api/miniapp/avatar/random").Query("app_id", appID).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AddAvatar 设置头像
func (w remote) AddAvatar(appID, nickname, afilekey string) (*OperateResponse, error) {
	if _, err := api.GetCore().Post("/api/miniapp/avatar").Body(map[string]any{
		"app_id":   appID,
		"nickname": nickname,
		"file_key": afilekey,
	}).Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0}, nil
}

// UploadAvatarImg 上传自定义头像图片
func (w remote) UploadAvatarImg(appID, jpglink string) (*OAuthAddAvatarImageResponse, error) {
	var resp OAuthAddAvatarImageResponse
	if err := api.GetCore().Post("/api/miniapp/avatar/upload").Body(map[string]any{
		"app_id":   appID,
		"jpg_link": jpglink,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetRecord 获取小程序使用记录
func (w remote) GetRecord() (*GetUsageRecordResponse, error) {
	var resp GetUsageRecordResponse
	if err := api.GetCore().Get("/api/miniapp/record").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AddRecord 添加小程序使用记录
func (w remote) AddRecord(username string) (*OperateResponse, error) {
	if _, err := api.GetCore().Post("/api/miniapp/record").Body(map[string]any{
		"username": username,
	}).Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0}, nil
}

// GetUserOpenID 获取用户 OpenID
func (w remote) GetUserOpenID(appID, username string) (*BizJsApiGetUserOpenIdResponse, error) {
	var resp BizJsApiGetUserOpenIdResponse
	if err := api.GetCore().Post("/api/miniapp/openid").Body(map[string]any{
		"app_id":   appID,
		"username": username,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// OauthSdkApp SDK OAuth 确认授权
func (w remote) OauthSdkApp(appID string, scope []string, operate uint32) (*SDKOAuthAuthorizeConfirmResponse, error) {
	if operate != 1 || len(scope) != 1 || scope[0] != "snsapi_userinfo" {
		return nil, errors.New("SDK OAuth requires operate=1 and snsapi_userinfo; use ConfirmSDKOAuth for explicit parameters")
	}
	var resp SDKOAuthAuthorizeConfirmResponse
	if err := api.GetCore().Post("/api/miniapp/oauth/sdk").Body(map[string]any{
		"app_id": appID,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ThirdAppGrant 第三方 APP OAuth 授权
func (w remote) ThirdAppGrant(appID, oauthURL string) (*StringResponse, error) {
	var code string
	if err := api.GetCore().Post("/api/miniapp/oauth/third").Body(map[string]any{
		"app_id": appID,
		"url":    oauthURL,
	}).DoJson(&code); err != nil {
		return nil, err
	}
	return &StringResponse{Value: code}, nil
}

// SessionQRCodeParams is a core parameter contract, independent of core implementation types.
type SessionQRCodeParams struct {
	AppID     string `json:"app_id"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Package   string `json:"package"`
	PaySign   string `json:"pay_sign"`
}

func GetSignedSessionQRCode(p SessionQRCodeParams) (string, error) {
	var result string
	err := api.GetCore().Post("/api/miniapp/session/qrcode").Body(p).DoJson(&result)
	return result, err
}
func ConfirmSDKOAuth(appID string, avatarID int32, state, bundleID string) (*SDKOAuthAuthorizeConfirmResponse, error) {
	var result SDKOAuthAuthorizeConfirmResponse
	if err := api.GetCore().Post("/api/miniapp/oauth/sdk").Body(map[string]any{"app_id": appID, "avatar_id": avatarID, "state": state, "package_name": bundleID}).DoProto(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

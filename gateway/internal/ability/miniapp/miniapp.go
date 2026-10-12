// Package miniappability 提供小程序能力的实现（直连型）。
package miniappability

import (
	"errors"
	sdk "github.com/we-clay/we-clay/sdk/miniapp"

	miniappapi "github.com/we-clay/we-clay/gateway/internal/api/miniapp"
	sdkroot "github.com/we-clay/we-clay/sdk"
)

// ability 小程序能力实现（直连型）
type ability struct {
	api miniappapi.MiniAppService
}

func init() {
	sdk.Instance = &ability{api: miniappapi.Get()}
}

// JSLogin 小程序 JS 登录授权
func (a ability) JSLogin(appID string) (*sdk.JSLogin_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.JSLogin")
}

// QrcodeAuthLogin 小程序扫码授权登录
func (a ability) QrcodeAuthLogin(uuid string) (*sdk.QrcodeAuthLogin_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.QrcodeAuthLogin")
}

// GetRuntimeSession 获取小程序运行时会话
func (a ability) GetRuntimeSession(appID string) (*sdk.GetRuntimeSession_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.GetRuntimeSession")
}

// GetSessionQRCode 获取小程序会话二维码
func (a ability) GetSessionQRCode(appID string) (*sdk.GetSessionQRCode_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.GetSessionQRCode")
}

// OperateWxData 操作小程序数据
func (a ability) OperateWxData(appID string, data []byte, opt int32) (*sdk.OperateWxData_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.OperateWxData")
}

// CloudCallFunction 调用小程序云函数
func (a ability) CloudCallFunction(appID string, data []byte) (*sdk.CloudCallFunction_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.CloudCallFunction")
}

// SendVerifyCode 发送手机验证码
func (a ability) SendVerifyCode(appID, mobile string, opcode int) (*sdk.SendVerifyCode_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.SendVerifyCode")
}

// CheckVerifyCode 校验手机验证码
func (a ability) CheckVerifyCode(appID, mobile, code string, opcode int) (*sdk.CheckVerifyCode_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.CheckVerifyCode")
}

// AddMobile 绑定手机号
func (a ability) AddMobile(appID, mobile, code string) (*sdk.AddMobile_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.AddMobile")
}

// DelMobile 解绑手机号
func (a ability) DelMobile(appID, mobile string) (*sdk.DelMobile_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.DelMobile")
}

// GetAllMobile 获取已绑定手机号列表
func (a ability) GetAllMobile(appID string) (*sdk.GetAllMobile_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.GetAllMobile")
}

// GetRandomAvatar 获取随机头像
func (a ability) GetRandomAvatar(appID string) (*sdk.GetRandomAvatar_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.GetRandomAvatar")
}

// AddAvatar 设置头像
func (a ability) AddAvatar(appID, nickname, afilekey string) (*sdk.AddAvatar_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.AddAvatar")
}

// UploadAvatarImg 上传自定义头像图片
func (a ability) UploadAvatarImg(appID, jpglink string) (*sdk.UploadAvatarImg_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.UploadAvatarImg")
}

// GetRecord 获取小程序使用记录
func (a ability) GetRecord() (*sdk.GetRecord_Response, error) {
	resp, err := a.api.GetRecord()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("miniapp 回执缺失，结果未确认")
	}
	result := resp.GetResult().GetValue()
	status := resp.GetStatus()
	return &sdk.GetRecord_Response{
		Result:      &result,
		StarList:    mapItems(resp.GetStarList()),
		HistoryList: mapItems(resp.GetHistoryList()),
		Status:      &status,
	}, nil
}

// AddRecord 添加小程序使用记录
func (a ability) AddRecord(username string) (*sdk.AddRecord_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.AddRecord")
}

// GetUserOpenID 获取用户 OpenID
func (a ability) GetUserOpenID(appID, username string) (*sdk.GetUserOpenID_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.GetUserOpenID")
}

// OauthSdkApp SDK OAuth 确认授权
func (a ability) OauthSdkApp(appID string, scope []string, operate uint32) (*sdk.OauthSdkApp_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.OauthSdkApp")
}

// ThirdAppGrant 第三方 APP OAuth 授权
func (a ability) ThirdAppGrant(appID, oauthURL string) (*sdk.ThirdAppGrant_Response, error) {
	return nil, sdkroot.NotImplemented("miniapp.ThirdAppGrant")
}

func mapJSLogin(resp *miniappapi.JSLoginResponse) *sdk.JSLogin_Response {
	code := resp.GetCode()
	appName := resp.GetAppName()
	appIcon := resp.GetAppIcon()
	openid := resp.GetOpenid()
	sessionKey := resp.GetSessionKey()
	sessionTicket := resp.GetSessionTicket()
	lifespan := resp.GetLifespan()
	state := resp.GetState()
	signature := resp.GetSignature()
	return &sdk.JSLogin_Response{
		JsapiResult:   mapResult(resp.GetJsapiResult()),
		Code:          &code,
		ScopeInfo:     mapScopeInfo(resp.GetScopeInfo()),
		AppName:       &appName,
		AppIcon:       &appIcon,
		Openid:        &openid,
		SessionKey:    &sessionKey,
		SessionTicket: &sessionTicket,
		Lifespan:      &lifespan,
		State:         &state,
		Signature:     &signature,
	}
}

func mapOperateWxData(resp *miniappapi.JSOperateResponse) *sdk.OperateWxData_Response {
	appName := resp.GetAppName()
	appIcon := resp.GetAppIcon()
	cancelWording := resp.GetCancelWording()
	allowWording := resp.GetAllowWording()
	applyWording := resp.GetApplyWording()
	return &sdk.OperateWxData_Response{
		JsapiResult:   mapResult(resp.GetJsapiResult()),
		Data:          resp.GetData(),
		ScopeInfo:     mapScopeInfo(resp.GetScopeInfo()),
		AppName:       &appName,
		AppIcon:       &appIcon,
		CancelWording: &cancelWording,
		AllowWording:  &allowWording,
		ApplyWording:  &applyWording,
	}
}

func mapCheckVerifyCode(resp *miniappapi.CheckVerifyCodeResponse) *sdk.CheckVerifyCode_Response {
	status := resp.GetStatus()
	encryptedData := resp.GetEncryptedData()
	iv := resp.GetIv()
	showMobile := resp.GetShowMobile()
	cloudID := resp.GetCloudId()
	return &sdk.CheckVerifyCode_Response{
		Status:        &status,
		EncryptedData: &encryptedData,
		Iv:            &iv,
		ShowMobile:    &showMobile,
		CloudId:       &cloudID,
	}
}

func mapResult(result *miniappapi.Result) *sdk.Result {
	if result == nil {
		return nil
	}
	return &sdk.Result{
		Code:  result.GetCode(),
		Error: result.GetError(),
	}
}

func mapScopeInfo(info *miniappapi.ScopeInfo) *sdk.ScopeInfo {
	if info == nil {
		return nil
	}
	scope := info.GetScope()
	description := info.GetDescription()
	authState := info.GetAuthState()
	extendDesc := info.GetExtendDesc()
	authDescription := info.GetAuthDescription()
	return &sdk.ScopeInfo{
		Scope:           &scope,
		Description:     &description,
		AuthState:       &authState,
		ExtendDesc:      &extendDesc,
		AuthDescription: &authDescription,
	}
}

func mapItems(items []*miniappapi.Item) []*sdk.Item {
	result := make([]*sdk.Item, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		username := item.GetUsername()
		nickname := item.GetNickname()
		iconURL := item.GetIconUrl()
		updateTime := item.GetUpdateTime()
		result = append(result, &sdk.Item{
			Username:   &username,
			Nickname:   &nickname,
			IconUrl:    &iconURL,
			UpdateTime: &updateTime,
		})
	}
	return result
}

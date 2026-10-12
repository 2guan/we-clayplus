// Package loginability 提供登录能力的实现。
package loginability

import (
	sdk "github.com/we-clay/we-clay/sdk/login"

	"github.com/we-clay/we-clay/gateway/internal/api"
	loginapi "github.com/we-clay/we-clay/gateway/internal/api/login"
)

// ability 登录能力实现（直连型）
type ability struct {
	api loginapi.LoginService
}

func init() {
	sdk.Instance = &ability{api: loginapi.Get()}
}

// Login 执行扫码登录
func (a ability) Login() (*sdk.Login_Response, error) {
	resp, err := a.api.Login()
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.Login_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Init 首次登录后初始化
func (a ability) Init() (*sdk.Init_Response, error) {
	resp, err := a.api.Init()
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.Init_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Refresh 刷新登录状态
func (a ability) Refresh() (*sdk.Refresh_Response, error) {
	resp, err := a.api.Refresh()
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.Refresh_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Wakeup 唤醒登录
func (a ability) Wakeup() (*sdk.Wakeup_Response, error) {
	resp, err := a.api.Wakeup()
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.Wakeup_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Logout 登出
func (a ability) Logout() (*sdk.Logout_Response, error) {
	resp, err := a.api.Logout()
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.Logout_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PasswordLogin 使用账号密码登录
func (a ability) PasswordLogin(req *sdk.PasswordLogin_Request) (*sdk.PasswordLogin_Response, error) {
	var apiReq loginapi.PasswordLoginRequest
	if err := api.TransformProto(req, &apiReq); err != nil {
		return nil, err
	}
	resp, err := a.api.PasswordLogin(&apiReq)
	if resp == nil || err != nil {
		return nil, err
	}
	var result sdk.PasswordLogin_Response
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

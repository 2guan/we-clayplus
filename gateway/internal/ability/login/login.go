// Package loginability 提供登录能力的实现。
package loginability

import (
	sdk "github.com/we-clay/we-clay/sdk/login"

	loginapi "github.com/we-clay/we-clay/gateway/internal/api/login"
	sdkroot "github.com/we-clay/we-clay/sdk"
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
	return nil, sdkroot.NotImplemented(
		"login.Login")
}

// Init 首次登录后初始化
func (a ability) Init() (*sdk.Init_Response, error) {
	return nil, sdkroot.NotImplemented(
		"login.Init")
}

// Refresh 刷新登录状态
func (a ability) Refresh() (*sdk.Refresh_Response, error) {
	return nil, sdkroot.NotImplemented(
		"login.Refresh")
}

// Wakeup 唤醒登录
func (a ability) Wakeup() (*sdk.Wakeup_Response, error) {
	return nil, sdkroot.NotImplemented(
		"login.Wakeup")
}

// Logout 登出
func (a ability) Logout() (*sdk.Logout_Response, error) {
	return nil, sdkroot.NotImplemented(
		"login.Logout")
}

// PasswordLogin 使用账号密码登录
func (a ability) PasswordLogin(req *sdk.PasswordLogin_Request) (*sdk.PasswordLogin_Response, error) {
	return nil, sdkroot.NotImplemented(
		"login.PasswordLogin",
	)
}

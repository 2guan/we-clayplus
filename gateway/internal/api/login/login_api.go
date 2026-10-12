// Package loginapi 提供登录服务的 API 接口定义。
package loginapi

// LoginService 登录服务 API 接口（返回 API proto 类型）
type LoginService interface {
	// Login 执行扫码登录
	Login() (*QRCodeResult, error)
	// Init 首次登录后初始化
	Init() (*InitResponse, error)
	// Refresh 刷新登录状态
	Refresh() (*OperateResponse, error)
	// Wakeup 唤醒登录
	Wakeup() (*OperateResponse, error)
	// Logout 登出
	Logout() (*OperateResponse, error)
	// PasswordLogin 使用账号密码登录
	PasswordLogin(req *PasswordLoginRequest) (*PasswordLoginResult, error)
}

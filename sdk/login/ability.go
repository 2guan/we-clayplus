package login

// Ability 登录能力接口（供插件嵌入使用）
type Ability interface {
	// Login 执行扫码登录
	Login() (*Login_Response, error)
	// Init 首次登录后初始化
	Init() (*Init_Response, error)
	// Refresh 刷新登录状态
	Refresh() (*Refresh_Response, error)
	// Wakeup 唤醒登录
	Wakeup() (*Wakeup_Response, error)
	// Logout 登出
	Logout() (*Logout_Response, error)
	// PasswordLogin 使用账号密码登录
	PasswordLogin(req *PasswordLogin_Request) (*PasswordLogin_Response, error)
}

// Instance 登录能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

package login

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程登录服务
type Client struct {
	Client LoginServiceClient
}

var _ Ability = (*Client)(nil)

// Login 执行扫码登录
func (c Client) Login() (*Login_Response, error) {
	return c.Client.Login(context.Background(), &Login_Request{})
}

// Init 首次登录后初始化
func (c Client) Init() (*Init_Response, error) {
	return c.Client.Init(context.Background(), &Init_Request{})
}

// Refresh 刷新登录状态
func (c Client) Refresh() (*Refresh_Response, error) {
	return c.Client.Refresh(context.Background(), &Refresh_Request{})
}

// Wakeup 唤醒登录
func (c Client) Wakeup() (*Wakeup_Response, error) {
	return c.Client.Wakeup(context.Background(), &Wakeup_Request{})
}

// Logout 登出
func (c Client) Logout() (*Logout_Response, error) {
	return c.Client.Logout(context.Background(), &Logout_Request{})
}

// PasswordLogin 使用账号密码登录
func (c Client) PasswordLogin(req *PasswordLogin_Request) (*PasswordLogin_Response, error) {
	return c.Client.PasswordLogin(context.Background(), req)
}

// Server 实现 LoginServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedLoginServiceServer
	Impl Ability
}

// Login 执行扫码登录
func (s Server) Login(ctx context.Context, request *Login_Request) (*Login_Response, error) {
	return s.Impl.Login()
}

// Init 首次登录后初始化
func (s Server) Init(ctx context.Context, request *Init_Request) (*Init_Response, error) {
	return s.Impl.Init()
}

// Refresh 刷新登录状态
func (s Server) Refresh(ctx context.Context, request *Refresh_Request) (*Refresh_Response, error) {
	return s.Impl.Refresh()
}

// Wakeup 唤醒登录
func (s Server) Wakeup(ctx context.Context, request *Wakeup_Request) (*Wakeup_Response, error) {
	return s.Impl.Wakeup()
}

// Logout 登出
func (s Server) Logout(ctx context.Context, request *Logout_Request) (*Logout_Response, error) {
	return s.Impl.Logout()
}

// PasswordLogin 使用账号密码登录
func (s Server) PasswordLogin(ctx context.Context, request *PasswordLogin_Request) (*PasswordLogin_Response, error) {
	return s.Impl.PasswordLogin(request)
}

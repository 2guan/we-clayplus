package official

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程公众号服务。
type Client struct {
	Client OfficialServiceClient
}

var _ Ability = (*Client)(nil)

// Follow 关注公众号
func (c Client) Follow(appid string) (*Follow_Response, error) {
	return c.Client.Follow(context.Background(), &Follow_Request{Appid: appid})
}

// Quit 取关公众号
func (c Client) Quit(appid string) (*Quit_Response, error) {
	return c.Client.Quit(context.Background(), &Quit_Request{Appid: appid})
}

// MpGetA8Key 获取公众号 A8Key
func (c Client) MpGetA8Key(url string) (*MpGetA8Key_Response, error) {
	return c.Client.MpGetA8Key(context.Background(), &MpGetA8Key_Request{Url: url})
}

// JSAPIPreVerify JSAPI 预验证
func (c Client) JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerify_Response, error) {
	return c.Client.JSAPIPreVerify(context.Background(), &JSAPIPreVerify_Request{
		Url:       url,
		Appid:     appid,
		JsapiList: jsapiList,
	})
}

// OauthAuthorize OAuth 授权
func (c Client) OauthAuthorize(url, appid string) (*OauthAuthorize_Response, error) {
	return c.Client.OauthAuthorize(context.Background(), &OauthAuthorize_Request{
		Url:   url,
		Appid: appid,
	})
}

// ReadArticle 阅读公众号文章
func (c Client) ReadArticle(url string) (*ReadArticle_Response, error) {
	return c.Client.ReadArticle(context.Background(), &ReadArticle_Request{Url: url})
}

// LikeArticle 点赞公众号文章
func (c Client) LikeArticle(url string) (*LikeArticle_Response, error) {
	return c.Client.LikeArticle(context.Background(), &LikeArticle_Request{Url: url})
}

// Server 实现 OfficialServiceServer 接口，将 gRPC 请求委托给 Ability 实现。
type Server struct {
	UnimplementedOfficialServiceServer
	Impl Ability
}

// Follow 关注公众号
func (s Server) Follow(ctx context.Context, request *Follow_Request) (*Follow_Response, error) {
	return s.Impl.Follow(request.Appid)
}

// Quit 取关公众号
func (s Server) Quit(ctx context.Context, request *Quit_Request) (*Quit_Response, error) {
	return s.Impl.Quit(request.Appid)
}

// MpGetA8Key 获取公众号 A8Key
func (s Server) MpGetA8Key(ctx context.Context, request *MpGetA8Key_Request) (*MpGetA8Key_Response, error) {
	return s.Impl.MpGetA8Key(request.Url)
}

// JSAPIPreVerify JSAPI 预验证
func (s Server) JSAPIPreVerify(ctx context.Context, request *JSAPIPreVerify_Request) (*JSAPIPreVerify_Response, error) {
	return s.Impl.JSAPIPreVerify(request.Url, request.Appid, request.JsapiList)
}

// OauthAuthorize OAuth 授权
func (s Server) OauthAuthorize(ctx context.Context, request *OauthAuthorize_Request) (*OauthAuthorize_Response, error) {
	return s.Impl.OauthAuthorize(request.Url, request.Appid)
}

// ReadArticle 阅读公众号文章
func (s Server) ReadArticle(ctx context.Context, request *ReadArticle_Request) (*ReadArticle_Response, error) {
	return s.Impl.ReadArticle(request.Url)
}

// LikeArticle 点赞公众号文章
func (s Server) LikeArticle(ctx context.Context, request *LikeArticle_Request) (*LikeArticle_Response, error) {
	return s.Impl.LikeArticle(request.Url)
}

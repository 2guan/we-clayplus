package official

// Ability 公众号能力接口（供插件嵌入使用）
type Ability interface {
	// Follow 关注公众号
	Follow(appid string) (*Follow_Response, error)
	// Quit 取关公众号
	Quit(appid string) (*Quit_Response, error)
	// MpGetA8Key 获取公众号 A8Key
	MpGetA8Key(url string) (*MpGetA8Key_Response, error)
	// JSAPIPreVerify JSAPI 预验证
	JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerify_Response, error)
	// OauthAuthorize OAuth 授权
	OauthAuthorize(url, appid string) (*OauthAuthorize_Response, error)
	// ReadArticle 阅读公众号文章
	ReadArticle(url string) (*ReadArticle_Response, error)
	// LikeArticle 点赞公众号文章
	LikeArticle(url string) (*LikeArticle_Response, error)
}

// Instance 公众号能力实例（由 gateway/internal/ability 层注入）
var Instance Ability

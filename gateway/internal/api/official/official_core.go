// Package officialapi 提供公众号服务的远程实现（通过 gRPC 调用 core）。
package officialapi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
)

// remote 公众号服务 远程实现（通过 gRPC 调用 core）。
type remote struct{}

// Get 获取 OfficialService 单例（gRPC 模式）。
var Get = sync.OnceValue(func() OfficialService {
	return &remote{}
})

// Follow 关注公众号
func (w remote) Follow(appid string) (*contactapi.VerifyUserResponse, error) {
	var resp contactapi.VerifyUserResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/official/%s/follow", appid)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Quit 取关公众号
func (w remote) Quit(appid string) (*baseapi.OperateResponse, error) {
	var resp baseapi.OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/official/%s", appid)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MpGetA8Key 获取公众号 A8Key
func (w remote) MpGetA8Key(url string) (*GetA8KeyResponse, error) {
	var resp GetA8KeyResponse
	if err := api.GetCore().Post("/api/official/a8key").Query("url", url).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// JSAPIPreVerify JSAPI 预验证
func (w remote) JSAPIPreVerify(url, appid string, jsapiList []string) (*JSAPIPreVerifyResponse, error) {
	var resp JSAPIPreVerifyResponse
	if err := api.GetCore().Post("/api/official/jsapi").
		Query("url", url, "appid", appid, "jsapi_list", strings.Join(jsapiList, ",")).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// OauthAuthorize OAuth 授权
func (w remote) OauthAuthorize(url, appid string) (*OauthAuthorizeResponse, error) {
	var resp OauthAuthorizeResponse
	if err := api.GetCore().Post("/api/official/oauth").
		Query("url", url, "appid", appid).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReadArticle 阅读公众号文章
func (w remote) ReadArticle(url string) (*ArticleResult, error) {
	var resp articleResultDTO
	if err := api.GetCore().Post("/api/official/article/read").Query("url", url).DoJson(&resp); err != nil {
		return nil, err
	}
	return mapArticleResult(resp), nil
}

// LikeArticle 点赞公众号文章
func (w remote) LikeArticle(url string) (*ArticleResult, error) {
	var resp articleResultDTO
	if err := api.GetCore().Post("/api/official/article/like").Query("url", url).DoJson(&resp); err != nil {
		return nil, err
	}
	return mapArticleResult(resp), nil
}

type articleResultDTO struct {
	ResponseBody string            `json:"ResponseBody"`
	Cookies      string            `json:"Cookies"`
	Headers      map[string]string `json:"Headers"`
}

func mapArticleResult(result articleResultDTO) *ArticleResult {
	headers := make(map[string]string, len(result.Headers))
	for key, value := range result.Headers {
		headers[key] = value
	}
	return &ArticleResult{
		ResponseBody: result.ResponseBody,
		Cookies:      result.Cookies,
		Headers:      headers,
	}
}

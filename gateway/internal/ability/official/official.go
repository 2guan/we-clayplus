// Package officialability 提供公众号能力的实现（直连型）。
package officialability

import (
	sdk "github.com/we-clay/we-clay/sdk/official"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	officialapi "github.com/we-clay/we-clay/gateway/internal/api/official"
	sdkroot "github.com/we-clay/we-clay/sdk"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ability 公众号能力实现（直连型）。
type ability struct {
	api officialapi.OfficialService
}

func init() {
	sdk.Instance = &ability{api: officialapi.Get()}
}

// Follow 关注公众号
func (a ability) Follow(appid string) (*sdk.Follow_Response, error) {
	return nil, sdkroot.NotImplemented("official.Follow")
}

// Quit 取关公众号
func (a ability) Quit(appid string) (*sdk.Quit_Response, error) {
	return nil, sdkroot.NotImplemented("official.Quit")
}

// MpGetA8Key 获取公众号 A8Key
func (a ability) MpGetA8Key(url string) (*sdk.MpGetA8Key_Response, error) {
	return nil, sdkroot.NotImplemented("official.MpGetA8Key")
}

// JSAPIPreVerify JSAPI 预验证
func (a ability) JSAPIPreVerify(url, appid string, jsapiList []string) (*sdk.JSAPIPreVerify_Response, error) {
	return nil, sdkroot.NotImplemented("official.JSAPIPreVerify")
}

// OauthAuthorize OAuth 授权
func (a ability) OauthAuthorize(url, appid string) (*sdk.OauthAuthorize_Response, error) {
	return nil, sdkroot.NotImplemented("official.OauthAuthorize")
}

// ReadArticle 阅读公众号文章
func (a ability) ReadArticle(url string) (*sdk.ReadArticle_Response, error) {
	return nil, sdkroot.NotImplemented("official.ReadArticle")
}

// LikeArticle 点赞公众号文章
func (a ability) LikeArticle(url string) (*sdk.LikeArticle_Response, error) {
	return nil, sdkroot.NotImplemented("official.LikeArticle")
}

func mapContactBaseResult(resp *contactapi.VerifyUserResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return mapBaseResult(resp.GetBaseResponse())
}

func mapBaseResult(resp *baseapi.BaseResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return &sdk.BaseResult{
		Code:    resp.GetCode(),
		Message: resp.GetMessage().GetValue(),
	}
}

func mapBuffer(buffer *baseapi.Buffer) *sdk.Buffer {
	if buffer == nil {
		return nil
	}
	return &sdk.Buffer{
		Size: buffer.GetSize(),
		Data: buffer.GetData(),
	}
}

func mapJSAPIPermission(permission *officialapi.GetA8KeyResponse_JSAPIPermissionBitSet) *sdk.JSAPIPermissionBitSet {
	if permission == nil {
		return nil
	}
	return &sdk.JSAPIPermissionBitSet{
		Value1: permission.GetValue1(),
		Value2: permission.GetValue2(),
		Value3: permission.GetValue3(),
		Value4: permission.GetValue4(),
	}
}

func mapScope(scope *officialapi.GetA8KeyResponse_BizScopeInfo) *sdk.BizScopeInfo {
	if scope == nil {
		return nil
	}
	return &sdk.BizScopeInfo{
		Scope:            scope.GetScope(),
		ScopeStatus:      scope.GetScopeStatus(),
		ScopeDescription: scope.GetScopeDescription(),
		ApiCount:         scope.GetApiCount(),
		ApiList:          mapStringValues(scope.GetApiList()),
	}
}

func mapScopeList(scopes []*officialapi.GetA8KeyResponse_BizScopeInfo) []*sdk.BizScopeInfo {
	result := make([]*sdk.BizScopeInfo, 0, len(scopes))
	for _, scope := range scopes {
		if scope == nil {
			continue
		}
		result = append(result, mapScope(scope))
	}
	return result
}

func mapStringValues(values []*wrapperspb.StringValue) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		result = append(result, value.GetValue())
	}
	return result
}

func mapHTTPHeader(header *officialapi.GetA8KeyResponse_HttpHeader) *sdk.HttpHeader {
	if header == nil {
		return nil
	}
	return &sdk.HttpHeader{
		Key:   header.GetKey(),
		Value: header.GetValue(),
	}
}

func mapHTTPHeaders(headers []*officialapi.GetA8KeyResponse_HttpHeader) []*sdk.HttpHeader {
	result := make([]*sdk.HttpHeader, 0, len(headers))
	for _, header := range headers {
		if header == nil {
			continue
		}
		result = append(result, mapHTTPHeader(header))
	}
	return result
}

func mapJSAPIResult(result *officialapi.JSAPIPreVerifyResponse_JSAPIResult) *sdk.JSAPIResult {
	if result == nil {
		return nil
	}
	return &sdk.JSAPIResult{
		Code:  result.GetCode(),
		Error: result.GetError(),
		Json:  result.GetJson(),
	}
}

func mapArticleResult(result *officialapi.ArticleResult) *sdk.ArticleResult {
	if result == nil {
		return nil
	}
	headers := make(map[string]string, len(result.GetHeaders()))
	for key, value := range result.GetHeaders() {
		headers[key] = value
	}
	return &sdk.ArticleResult{
		ResponseBody: result.GetResponseBody(),
		Cookies:      result.GetCookies(),
		Headers:      headers,
	}
}

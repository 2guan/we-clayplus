package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// 部分兼容网关即使请求 b64_json 也返回自己的媒体链接。只取回配置
// 服务的同源图片，禁止跨源、重定向及认证信息外发；不接受任意 URL 抓取。
func downloadGeneratedImage(ctx context.Context, c Config, rawURL string) ([]byte, error) {
	base, err := url.Parse(strings.TrimRight(c.Endpoint, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("模型图片服务地址无效")
	}
	u, err := url.Parse(rawURL)
	if err != nil || rawURL == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, fmt.Errorf("模型未返回有效图片链接")
	}
	u = base.ResolveReference(u)
	if u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("模型返回了跨源图片链接，已停止下载；请使用 b64_json 或服务同源图片链接")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("模型图片链接无效")
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	client := &http.Client{Transport: debugTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("模型图片下载失败：%w", safeConnectionError(err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型图片下载返回 HTTP %d", res.StatusCode)
	}
	if res.ContentLength > 32<<20 {
		return nil, fmt.Errorf("模型图片超过 32 MB")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (32<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("模型图片读取失败：%w", safeConnectionError(err))
	}
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("模型图片超过 32 MB")
	}
	return data, nil
}

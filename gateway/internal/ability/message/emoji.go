package messageability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxEmojiSize = 20 << 20

func allowedEmojiURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "qpic.cn" || strings.HasSuffix(h, ".qpic.cn") || h == "wxapp.tc.qq.com" || h == "vweixinf.tc.qq.com"
}

// 不接受前端指定 URL；仅从已接收消息中的微信 CDN 地址读取。
func downloadEmoji(ctx context.Context, raw string) (io.ReadCloser, error) {
	return downloadEmojiWithClient(ctx, raw, emojiHTTPClient())
}

func emojiHTTPClient() *http.Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, a := range ips {
			if !a.IP.IsGlobalUnicast() || a.IP.IsPrivate() || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast() {
				return nil, errors.New("无效 CDN 地址")
			}
		}
		for _, a := range ips {
			c, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if err == nil {
				return c, nil
			}
		}
		return nil, errors.New("CDN 无法连接")
	}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 40 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowedEmojiURL(req.URL.String()) {
			return errors.New("CDN 重定向不允许")
		}
		return nil
	}}
	return client
}

func downloadEmojiWithClient(ctx context.Context, raw string, client *http.Client) (io.ReadCloser, error) {
	if !allowedEmojiURL(raw) {
		return nil, errors.New("此表情未提供支持的 CDN 地址，请在手机微信查看")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("表情地址无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("表情下载失败，请稍后重试")
	}
	if resp.StatusCode != 200 || resp.ContentLength > maxEmojiSize {
		resp.Body.Close()
		return nil, errors.New("表情已过期或不可下载，请在手机微信查看")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxEmojiSize+1))
	if err != nil || len(data) > maxEmojiSize {
		return nil, errors.New("表情下载中断或超过 20 MB")
	}
	switch http.DetectContentType(data) {
	case "image/png", "image/gif", "image/jpeg", "image/webp":
	default:
		return nil, errors.New("表情格式暂不支持，可能是加密资源")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

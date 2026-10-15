// 文件名：service.go
// 主要功能：读取 RSS/Atom 和可配置的媒体 JSON 服务，限制响应大小、超时及内网访问。
// 作者：2guan
package toolbox

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"golang.org/x/net/html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Item struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Artist   string `json:"artist,omitempty"`
	MediaURL string `json:"media_url,omitempty"`
}

// PublicURL 校验链接语法；连接时仍验证每一个 DNS 结果，阻止重绑定和内网跳转。
func PublicURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("请填写不带凭据的 HTTP(S) 地址")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) {
		return fmt.Errorf("外部服务不能访问本机、内网或保留地址")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return fmt.Errorf("外部服务不能访问本机")
	}
	return nil
}
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
func HTTPClient() *http.Client {
	tr := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, fmt.Errorf("服务域名解析失败")
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("服务域名没有地址")
		}
		for _, v := range ips {
			if !publicIP(v.IP) {
				return nil, fmt.Errorf("外部服务解析到了非公网地址")
			}
		}
		d := net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	return &http.Client{Transport: tr, Timeout: 25 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("重定向过多")
		}
		if via[0].URL.Scheme == "https" && r.URL.Scheme != "https" {
			return fmt.Errorf("不允许 HTTPS 降级跳转")
		}
		if r.URL.Host != via[0].URL.Host {
			r.Header.Del("Authorization")
		}
		return PublicURL(r.URL.String())
	}}
}
func ReadURL(ctx context.Context, client *http.Client, raw, key string, limit int64) ([]byte, string, error) {
	if e := PublicURL(raw); e != nil {
		return nil, "", e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, "", fmt.Errorf("服务地址无效")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, "", fmt.Errorf("外部服务请求失败，请检查地址、网络或超时")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("外部服务返回 HTTP %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil || int64(len(data)) > limit {
		return nil, "", fmt.Errorf("外部响应读取失败或超过大小上限")
	}
	return data, res.Header.Get("Content-Type"), nil
}
func plain(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	for {
		t := z.Next()
		if t == html.ErrorToken {
			break
		}
		if t == html.TextToken {
			b.Write(z.Text())
		}
	}
	return strings.TrimSpace(b.String())
}
func Feed(data []byte, query string, limit int) ([]Item, error) {
	var f struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
				Link  string `xml:"link"`
			} `xml:"item"`
		} `xml:"channel"`
		Entries []struct {
			Title string `xml:"title"`
			Links []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if xml.Unmarshal(data, &f) != nil {
		return nil, fmt.Errorf("新闻源不是有效 RSS / Atom")
	}
	out := []Item{}
	add := func(title, link string) {
		title = plain(title)
		if title != "" && len(title) <= 1000 && len(link) <= 2048 && PublicURL(link) == nil && (query == "" || strings.Contains(strings.ToLower(title), strings.ToLower(query))) {
			out = append(out, Item{Title: title, URL: link})
		}
	}
	for _, i := range f.Channel.Items {
		add(i.Title, i.Link)
	}
	for _, i := range f.Entries {
		for _, l := range i.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				add(i.Title, l.Href)
				break
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// JSON 服务契约：GET endpoint?q=关键词（视频额外提供 url），返回 {"items":[...]}。
func Search(ctx context.Context, client *http.Client, endpoint, key, query string, limit int) ([]Item, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("尚未配置服务地址，请在插件配置中填写")
	}
	u, e := url.Parse(endpoint)
	if e != nil {
		return nil, fmt.Errorf("服务地址无效")
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("url", query)
	q.Set("limit", fmt.Sprint(limit))
	u.RawQuery = q.Encode()
	raw, _, e := ReadURL(ctx, client, u.String(), key, 2<<20)
	if e != nil {
		return nil, e
	}
	var body struct {
		Items []Item `json:"items"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, fmt.Errorf("服务返回格式无效，应包含 items 数组")
	}
	if len(body.Items) > limit {
		body.Items = body.Items[:limit]
	}
	for i := range body.Items {
		v := &body.Items[i]
		v.Title = plain(v.Title)
		v.Artist = plain(v.Artist)
		if len(v.Title) > 1000 || len(v.Artist) > 500 || v.Title == "" {
			return nil, fmt.Errorf("服务结果标题无效")
		}
		if v.URL != "" && PublicURL(v.URL) != nil {
			return nil, fmt.Errorf("服务返回了不允许的链接")
		}
		if v.MediaURL != "" && PublicURL(v.MediaURL) != nil {
			return nil, fmt.Errorf("服务返回了不允许的媒体地址")
		}
	}
	return body.Items, nil
}

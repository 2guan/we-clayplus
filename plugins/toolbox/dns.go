package toolbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func onlyFakeIPs(ips []net.IPAddr) bool {
	if len(ips) == 0 {
		return false
	}
	for _, addr := range ips {
		ip := addr.IP.To4()
		if ip == nil || ip[0] != 198 || ip[1] != 18 && ip[1] != 19 {
			return false
		}
	}
	return true
}

// 固定解析器 IP 避免再次进入系统的虚拟 DNS；TLS 校验保持开启，禁止重定向。
func resolvePublicDNS(ctx context.Context, host string) ([]net.IPAddr, error) {
	tr := &http.Transport{TLSHandshakeTimeout: 5 * time.Second}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return queryPublicDNS(ctx, client, host)
}

func queryPublicDNS(ctx context.Context, client *http.Client, host string) ([]net.IPAddr, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://223.5.5.5/resolve?"+url.Values{"name": {host}, "type": {"A"}}.Encode(), nil)
	if e != nil {
		return nil, fmt.Errorf("公网 DNS 请求无效")
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("代理虚拟 DNS 的公网备用解析失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("公网备用解析服务不可用")
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	var body struct {
		Status int
		Answer []struct {
			Type int
			Data string
		}
	}
	if e != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &body) != nil || body.Status != 0 {
		return nil, fmt.Errorf("公网备用解析响应无效")
	}
	var ips []net.IPAddr
	for _, a := range body.Answer {
		if a.Type != 1 {
			continue
		}
		ip := net.ParseIP(a.Data)
		if ip == nil || ip.To4() == nil || !publicIP(ip) {
			return nil, fmt.Errorf("备用 DNS 返回了非公网地址")
		}
		ips = append(ips, net.IPAddr{IP: ip})
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("公网备用解析没有可用地址")
	}
	return ips, nil
}

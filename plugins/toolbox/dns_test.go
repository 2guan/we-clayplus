package toolbox

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestOnlyFakeIPs(t *testing.T) {
	for _, tc := range []struct {
		ips  []string
		want bool
	}{
		{[]string{"198.18.1.218"}, true}, {[]string{"198.18.0.1", "198.19.255.254"}, true},
		{nil, false}, {[]string{"198.18.0.1", "10.0.0.1"}, false}, {[]string{"198.18.0.1", "1.1.1.1"}, false}, {[]string{"127.0.0.1"}, false}, {[]string{"198.20.0.1"}, false}, {[]string{"::1"}, false},
	} {
		var ips []net.IPAddr
		for _, ip := range tc.ips {
			ips = append(ips, net.IPAddr{IP: net.ParseIP(ip)})
		}
		if onlyFakeIPs(ips) != tc.want {
			t.Fatal("虚拟 DNS 范围识别错误", tc.ips)
		}
	}
}

func TestPublicDNSFallbackContract(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"public", `{"Status":0,"Answer":[{"type":5,"data":"alias.example.com"},{"type":1,"data":"1.1.1.1"}]}`, 200, true},
		{"loopback", `{"Status":0,"Answer":[{"type":1,"data":"127.0.0.1"}]}`, 200, false},
		{"private", `{"Status":0,"Answer":[{"type":1,"data":"10.0.0.1"}]}`, 200, false},
		{"mixed", `{"Status":0,"Answer":[{"type":1,"data":"1.1.1.1"},{"type":1,"data":"10.0.0.1"}]}`, 200, false},
		{"fake", `{"Status":0,"Answer":[{"type":1,"data":"198.18.0.1"}]}`, 200, false},
		{"reserved", `{"Status":0,"Answer":[{"type":1,"data":"203.0.113.1"}]}`, 200, false},
		{"empty", `{"Status":0,"Answer":[]}`, 200, false}, {"nxdomain", `{"Status":3}`, 200, false}, {"malformed", `not json`, 200, false}, {"oversize", strings.Repeat("x", 65537), 200, false}, {"redirect", ``, 302, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "223.5.5.5" || r.URL.Scheme != "https" || r.URL.Path != "/resolve" || r.URL.Query().Get("name") != "example.com&injected=1" || r.URL.Query().Get("type") != "A" || r.Header.Get("Authorization") != "" {
					t.Error("备用解析地址、参数编码或凭据隔离错误")
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			ips, e := queryPublicDNS(context.Background(), client, "example.com&injected=1")
			if (e == nil) != tc.ok {
				t.Fatal("备用解析校验结果错误", e)
			}
			if tc.ok && (len(ips) != 1 || !ips[0].IP.Equal(net.ParseIP("1.1.1.1"))) {
				t.Fatal("真实地址丢失")
			}
		})
	}
}

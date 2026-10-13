package messageability

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type emojiTransport func(*http.Request) (*http.Response, error)

func (f emojiTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEmojiURLAndRedirectPolicy(t *testing.T) {
	for _, raw := range []string{"https://wxapp.tc.qq.com/a", "http://vweixinf.tc.qq.com/a", "https://mmbiz.qpic.cn/a"} {
		if !allowedEmojiURL(raw) {
			t.Fatal("valid CDN denied", raw)
		}
	}
	for _, raw := range []string{"file:///tmp/a", "http://127.0.0.1/a", "https://mmbiz.qpic.cn.evil.test/a", "https://evil.test@qpic.cn/a", "https://qpic.cn:8443/a", "https://qpic.cn/a#fragment"} {
		if allowedEmojiURL(raw) {
			t.Fatal("unsafe URL allowed", raw)
		}
		r, _ := http.NewRequest(http.MethodGet, raw, nil)
		if emojiHTTPClient().CheckRedirect(r, nil) == nil {
			t.Fatal("unsafe redirect allowed")
		}
	}
}
func TestEmojiDownloadValidatesPayloadAndBound(t *testing.T) {
	gif := []byte("GIF89a\x01\x00\x01\x00")
	for _, tt := range []struct {
		name   string
		body   []byte
		status int
		ok     bool
	}{
		{"gif", gif, 200, true}, {"html", []byte("<html>expired</html>"), 200, false}, {"expired", gif, 403, false}, {"chunked oversize", bytes.Repeat([]byte{'x'}, maxEmojiSize+1), 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &http.Client{Transport: emojiTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(bytes.NewReader(tt.body)), ContentLength: -1, Header: http.Header{}}, nil
			})}
			stream, err := downloadEmojiWithClient(context.Background(), "https://mmbiz.qpic.cn/synthetic", c)
			if !tt.ok {
				if err == nil {
					stream.Close()
					t.Fatal("invalid payload accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			got, _ := io.ReadAll(stream)
			if !bytes.Equal(got, tt.body) {
				t.Fatal("GIF was modified")
			}
		})
	}
}
func TestEmojiInvalidURLDoesNotRequest(t *testing.T) {
	c := &http.Client{Transport: emojiTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unsafe request sent"); return nil, nil })}
	_, err := downloadEmojiWithClient(context.Background(), "http://127.0.0.1/private?credential=synthetic", c)
	if err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal("error must not expose URL")
	}
}

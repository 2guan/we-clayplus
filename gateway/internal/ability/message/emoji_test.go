// 文件名：emoji_test.go
// 主要功能：验证 MD5 表情发送与引用、微信表情包 CDN 下载与重定向安全策略。
package messageability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
)

type emojiAPI struct {
	api.MessageService
	response *api.UploadEmojiResponse
	err      error
	calls    int
	digest   string
	data     []byte
}

func (e *emojiAPI) SendEmoji(receiver, digest string, data []byte) (*api.UploadEmojiResponse, error) {
	e.calls++
	e.digest = digest
	e.data = data
	return e.response, e.err
}

func TestEmojiReferenceAndUnknownReceipt(t *testing.T) {
	SetOutboundReady(true)
	const digest = "0123456789abcdef0123456789abcdef"
	m := &sdk.Message{Type: sdk.TypeEmoji, Receiver: &contact.Contact{Username: "synthetic"}, Data: &sdk.Message_Emoji{Emoji: &sdk.EmojiData{Media: &sdk.Media{Md5: digest}}}}
	good := &api.UploadEmojiResponse{Result: []*api.UploadEmojiResponse_Result{{Code: proto.Int32(0), NewId: proto.Uint64(9007199254740993), Md5: proto.String(digest)}}}
	for _, tc := range []struct {
		resp *api.UploadEmojiResponse
		err  error
		ok   bool
	}{{good, nil, true}, {nil, nil, false}, {&api.UploadEmojiResponse{}, nil, false}, {nil, errors.New("unknown"), false}, {&api.UploadEmojiResponse{Result: []*api.UploadEmojiResponse_Result{{NewId: proto.Uint64(123), Md5: proto.String("wrong")}}}, nil, false}} {
		client := &emojiAPI{response: tc.resp, err: tc.err}
		a := &ability{api: client}
		r, e := a.Send(m)
		if (e == nil) != tc.ok || client.calls != 1 || client.digest != digest || len(client.data) != 0 {
			t.Fatal("引用路径或失败处理错误", e)
		}
		if tc.ok && r.NewId != 9007199254740993 {
			t.Fatal("大整数回执被截断")
		}
	}
	for _, media := range []*sdk.Media{nil, {}, {Md5: "invalid"}, {Md5: digest, Data: []byte("wrong bytes")}} {
		m.Data = &sdk.Message_Emoji{Emoji: &sdk.EmojiData{Media: media}}
		client := &emojiAPI{}
		a := &ability{api: client}
		if _, e := a.Send(m); e == nil || client.calls != 0 {
			t.Fatal("无效资源进入 core")
		}
	}
}

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

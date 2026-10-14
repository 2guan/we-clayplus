// 文件名：emoji_test.go
// 主要功能：验证 MD5 表情发送、原始上传校验与异常回执不重试。
// 作者：2guan
package messageability

import (
	"errors"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"testing"
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

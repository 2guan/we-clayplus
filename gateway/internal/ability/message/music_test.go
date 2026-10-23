package messageability

import (
	"errors"
	base "github.com/we-clay/we-clay/gateway/internal/api/base"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

type musicAPI struct {
	api.MessageService
	response *api.SendAppMessageResponse
	err      error
	calls    int
	raw      string
}

func (a *musicAPI) SendApp(receiver, raw, extend string, typ int32) (*api.SendAppMessageResponse, error) {
	a.calls++
	a.raw = raw
	if receiver != "synthetic-peer" || typ != 76 || extend != "" {
		panic("音乐发送契约错误")
	}
	return a.response, a.err
}

func TestMusicSendFailuresAndReceipt(t *testing.T) {
	SetOutboundReady(true)
	raw, err := (sdk.Music{Title: "歌名", Artist: "歌手", Cover: "https://example.com/cover.jpg", AudioURL: "https://example.com/song.mp3"}).XML()
	if err != nil {
		t.Fatal(err)
	}
	response := func(code int32) *api.SendAppMessageResponse {
		return &api.SendAppMessageResponse{NewId: proto.Uint64(9007199254740993), CreateTime: proto.Int32(1789890000), ClientId: proto.String("synthetic-peer_1789890000"), BaseResponse: &base.BaseResponse{Code: proto.Int32(code)}}
	}
	for _, tc := range []struct {
		name     string
		response *api.SendAppMessageResponse
		err      error
		ok       bool
	}{
		{"success", response(0), nil, true}, {"refused", response(-1), nil, false}, {"missing", nil, nil, false}, {"empty", &api.SendAppMessageResponse{}, nil, false}, {"missing-id", &api.SendAppMessageResponse{BaseResponse: &base.BaseResponse{Code: proto.Int32(0)}}, nil, false}, {"transport", nil, errors.New("发送结果未知"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &musicAPI{response: tc.response, err: tc.err}
			ability := &ability{api: a}
			m := &sdk.Message{Type: sdk.TypeAppMusic, Receiver: &contact.Contact{Username: "synthetic-peer"}, Data: &sdk.Message_App{App: &sdk.AppData{SubType: 76, Xml: raw}}}
			r, e := ability.Send(m)
			if (e == nil) != tc.ok || a.calls != 1 {
				t.Fatal("失败误报成功或发生自动重发", e, a.calls)
			}
			if !strings.Contains(a.raw, "<thumburl>https://example.com/cover.jpg</thumburl>") {
				t.Fatal("受限音乐重建时丢失手机缩略图")
			}
			if tc.ok && (r.GetNewId() != 9007199254740993 || r.GetCreateTime() != 1789890000) {
				t.Fatal("丢失原始回执")
			}
			m.GetApp().Xml = `<appmsg><type>57</type></appmsg>`
			if _, e = ability.Send(m); e == nil || a.calls != 1 {
				t.Fatal("无效音乐进入 core")
			}
		})
	}
}

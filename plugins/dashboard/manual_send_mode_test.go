// 文件名：manual_send_mode_test.go
// 主要功能：验证手动发送成功或失败均不修改会话模式，兼容旧自动接管配置。
// 作者：2guan
package dashboard

import (
	"context"
	"errors"
	"github.com/we-clay/we-clay/sdk/control"
	"testing"
)

type modePreservingHost struct{ fail bool }

func (h modePreservingHost) Execute(context.Context, control.Command) (any, error) {
	if h.fail {
		return nil, errors.New("synthetic send failure")
	}
	return map[string]string{"id": "synthetic-message"}, nil
}
func TestManualSendPreservesReplyMode(t *testing.T) {
	for _, kind := range []string{"friend", "group"} {
		for _, mode := range []string{"auto", "manual", "paused"} {
			for _, fail := range []bool{false, true} {
				t.Run(kind+"/"+mode+map[bool]string{false: "/sent", true: "/failed"}[fail], func(t *testing.T) {
					s := newServer(t, true)
					s.cfg.Mock = false
					s.host = modePreservingHost{fail: fail}
					target := "synthetic-peer"
					if kind == "group" {
						target = "synthetic@chatroom"
					}
					if err := s.store.change(func(st *State) error {
						st.Contacts = []Contact{{ID: target, Kind: kind, Mode: mode}}
						st.Settings["reply.pause_on_send"] = true // 升级前的旧设置不能继续改变模式。
						st.Settings["reply.default_friend"] = "自动"
						st.Settings["reply.default_group"] = "自动"
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					before := s.store.snapshot().Contacts[0]
					err := s.send(context.Background(), actionRequest{Target: target, Content: "synthetic text"}, "manual")
					if (err != nil) != fail {
						t.Fatal("发送结果不符", err)
					}
					after := s.store.snapshot().Contacts[0]
					if after.Mode != before.Mode || after.ModeInherited != before.ModeInherited {
						t.Fatal("发送改变了会话模式或继承关系")
					}
				})
			}
		}
	}
}

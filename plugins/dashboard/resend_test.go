// 文件名：resend_test.go
// 主要功能：验证重发状态限制、未知回执确认和成功后删除原记录。
// 作者：2guan
package dashboard

import (
	"context"
	"testing"
)

func TestResendLifecycle(t *testing.T) {
	for _, scenario := range []string{"normal", "error"} {
		t.Run(scenario, func(t *testing.T) {
			s := newServer(t, true)
			target := s.store.snapshot().Contacts[0].ID
			s.store.change(func(v *State) error {
				v.Scenario = scenario
				v.Messages = []Message{{ID: "retry", Target: target, Kind: "text", Content: "原文", Self: true, Status: "unknown"}}
				return nil
			})
			if s.resend(context.Background(), actionRequest{ID: "retry", Target: target}) == nil {
				t.Fatal("未知回执未要求确认")
			}
			err := s.resend(context.Background(), actionRequest{ID: "retry", Target: target, Enabled: true, Content: "伪造内容"})
			ms := s.store.snapshot().Messages
			if scenario == "normal" {
				if err != nil || len(ms) != 1 || ms[0].Content != "原文" || ms[0].Status != "sent" || ms[0].ID == "retry" {
					t.Fatalf("重发结果错误: %v", err)
				}
				if s.resend(context.Background(), actionRequest{ID: "retry", Target: target, Enabled: true}) == nil {
					t.Fatal("重复重发已删除记录")
				}
			} else {
				if err == nil || len(ms) != 2 || ms[0].ID != "retry" {
					t.Fatal("失败重发删除了原消息")
				}
			}
		})
	}
}
func TestResendRejectsInvalidMessage(t *testing.T) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0].ID
	for _, m := range []Message{{Kind: "text", Self: false, Status: "failed"}, {Kind: "text", Self: true, Status: "sent"}, {Kind: "text", Self: true, Status: "sending"}, {Kind: "image", Self: true, Status: "failed"}} {
		m.ID = "retry"
		m.Target = target
		m.Content = "内容"
		s.store.change(func(v *State) error { v.Messages = []Message{m}; return nil })
		if s.resend(context.Background(), actionRequest{ID: m.ID, Target: target}) == nil {
			t.Fatal("接受了无效重发")
		}
		if len(s.store.snapshot().Messages) != 1 {
			t.Fatal("无效重发改变归档")
		}
	}
}

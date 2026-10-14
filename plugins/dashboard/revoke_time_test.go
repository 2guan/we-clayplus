// 文件名：revoke_time_test.go
// 主要功能：验证撤回接口的两分钟时限以及异常时间保护。
// 作者：2guan
package dashboard

import (
	"testing"
	"time"
)

func TestRevokeTimeLimit(t *testing.T) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0].ID
	for _, tc := range []struct {
		stamp   string
		allowed bool
	}{{time.Now().Add(-time.Minute).Format(time.RFC3339), true}, {time.Now().Add(-121 * time.Second).Format(time.RFC3339), false}, {"", false}, {time.Now().Add(time.Minute).Format(time.RFC3339), false}} {
		s.store.change(func(v *State) error {
			v.Messages = []Message{{ID: "fixture", Target: target, Self: true, Status: "sent", Time: tc.stamp}}
			return nil
		})
		got := call(t, s, "/api/action", actionRequest{Kind: "revoke", Target: target, ID: "fixture"}, nil, "")
		if (got.Code == 200) != tc.allowed {
			t.Fatalf("时间 %q: %d", tc.stamp, got.Code)
		}
	}
}
